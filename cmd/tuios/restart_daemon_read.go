package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/Gaurav-Gosain/tuios/internal/restartplan"
	"github.com/Gaurav-Gosain/tuios/internal/session"
	"golang.org/x/term"
)

// What restart-server reads about this machine's daemon before it asks: its
// state, its version, and the sessions with the panes a restart ends and the
// agents that can resume.

// Daemon states.
const (
	daemonRunning = "running"
	daemonStopped = "stopped"
	daemonUnknown = "unknown"
)

// daemonReadTimeout bounds the whole read of the daemon.
const daemonReadTimeout = 90 * time.Second

// syncAgent is an agent pane whose conversation can resume after a restart.
type syncAgent = restartplan.Agent

// localDaemon is what restart-server read about the daemon.
type localDaemon struct {
	State        string
	Version      string
	PID          int
	SessionCount int
	Sessions     []restartplan.Session
	// Unread says the sessions could not be listed, so a restart's cost is
	// not known.
	Unread bool
}

// readLocalDaemon reads this machine's daemon over its socket.
func readLocalDaemon(ctx context.Context) localDaemon {
	var d localDaemon
	if !session.IsDaemonRunning() {
		d.State = daemonStopped
		return d
	}
	d.State = daemonRunning
	client, err := session.DialVerbClientAs(version)
	if err != nil {
		if mismatch, ok := errors.AsType[*session.ProtocolMismatchError](err); ok {
			d.Version = mismatch.DaemonVersion
			d.PID = mismatch.DaemonPID
			d.SessionCount = mismatch.Sessions
			d.Unread = true
			return d
		}
		d.State = daemonUnknown
		return d
	}
	defer func() { _ = client.Close() }()
	raw, err := client.Call("hello", map[string]any{"client": "tuios-restart-server", "version": version, "protocol": session.VerbProtocolVersion})
	if err == nil {
		var hs struct {
			DaemonVersion string `json:"daemon_version"`
			PID           int    `json:"pid"`
			Sessions      int    `json:"sessions"`
		}
		if json.Unmarshal(raw, &hs) == nil {
			d.Version = hs.DaemonVersion
			d.PID = hs.PID
			d.SessionCount = hs.Sessions
		}
	}
	readDaemonSessions(ctx, &d, client)
	return d
}

// readDaemonSessions lists the daemon's sessions and the panes in each that
// run a program. A pane counts as running one when its terminal's foreground
// process is not the pane's own shell, or when the pane's own process is not
// a shell at all.
func readDaemonSessions(ctx context.Context, d *localDaemon, client *session.VerbClient) {
	raw, err := client.Call("list-sessions", nil)
	if err != nil {
		d.Unread = true
		return
	}
	var list struct {
		Sessions []struct {
			Name string `json:"name"`
		} `json:"sessions"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		d.Unread = true
		return
	}
	type paneRef struct {
		session int
		window  string
		facts   paneFacts
	}
	var refs []paneRef
	var pids []string
	sessions := make([]restartplan.Session, 0, len(list.Sessions))
	for _, s := range list.Sessions {
		ss := restartplan.Session{Name: plainLine(s.Name)}
		raw, err := client.Call("list-windows", map[string]any{"session": s.Name})
		if err == nil {
			var wl struct {
				Windows []struct {
					DisplayName string `json:"display_name"`
					WindowID    string `json:"window_id"`
					PID         int    `json:"pid"`
					AtPrompt    *bool  `json:"at_prompt"`
					Running     string `json:"running_cmdline"`
					AgentState  string `json:"agent_state"`
					Resumable   string `json:"agent_resumable"`
				} `json:"windows"`
			}
			if json.Unmarshal(raw, &wl) == nil {
				ss.Panes = len(wl.Windows)
				live := map[string]string{}
				for _, w := range wl.Windows {
					name := plainLine(w.DisplayName)
					if name == "" {
						name = shortWindowID(w.WindowID)
					}
					if w.Resumable != "" {
						ss.Resume = append(ss.Resume, syncAgent{Window: name, Harness: plainLine(w.Resumable)})
					} else if w.AgentState != "" && w.AgentState != "none" {
						live[w.WindowID] = name
					}
					f := paneFacts{pid: w.PID, running: plainLine(w.Running)}
					if w.AtPrompt != nil {
						f.atPrompt, f.promptKnown = *w.AtPrompt, true
					}
					refs = append(refs, paneRef{session: len(sessions), window: name, facts: f})
					if w.PID > 0 {
						pids = append(pids, strconv.Itoa(w.PID))
					}
				}
				// A daemon of another build may not mark the panes, so the
				// saved files answer for it.
				if d.Version != version {
					ss.Resume = append(ss.Resume, resumableAgents(s.Name, live)...)
				}
			}
		}
		sessions = append(sessions, ss)
	}
	procs := map[int]paneProc{}
	if len(pids) > 0 {
		cmd := exec.CommandContext(ctx, "/bin/sh", append([]string{"-c", paneProcScript, "sh"}, pids...)...)
		cmd.WaitDelay = 5 * time.Second
		if out, err := cmd.Output(); err == nil {
			procs = parsePaneProcs(string(out))
		}
	}
	for _, r := range refs {
		if program, busy := paneBusy(r.facts, procs); busy {
			sessions[r.session].Busy = append(sessions[r.session].Busy, restartplan.Pane{Window: r.window, Program: program})
		}
	}
	d.Sessions = sessions
	d.SessionCount = len(sessions)
}

// paneProcScript prints, for each pid it is given, the pid, its terminal's
// foreground process group, its command and the foreground command.
const paneProcScript = `for p in "$@"; do g=$(ps -o tpgid= -p "$p" 2>/dev/null | tr -d " "); c=$(ps -o comm= -p "$p" 2>/dev/null); if [ -z "$c" ]; then continue; fi; f=; if [ -n "$g" ] && [ "$g" -gt 0 ] 2>/dev/null; then f=$(ps -o comm= -p "$g" 2>/dev/null); fi; echo "pane=$p|$g|$c|$f"; done`

// paneFacts is what the daemon said about one pane.
type paneFacts struct {
	pid         int
	running     string
	atPrompt    bool
	promptKnown bool
}

// paneProc is what ps said about one pane's process.
type paneProc struct {
	tpgid      int
	comm       string
	foreground string
}

// parsePaneProcs reads the pane= lines paneProcScript prints.
func parsePaneProcs(out string) map[int]paneProc {
	procs := map[int]paneProc{}
	sc := bufio.NewScanner(strings.NewReader(out))
	sc.Buffer(make([]byte, 0, 4096), 64<<10)
	for sc.Scan() {
		line, ok := strings.CutPrefix(sc.Text(), "pane=")
		if !ok {
			continue
		}
		parts := strings.SplitN(line, "|", 4)
		if len(parts) != 4 {
			continue
		}
		pid, err := strconv.Atoi(parts[0])
		if err != nil {
			continue
		}
		tpgid, _ := strconv.Atoi(strings.TrimSpace(parts[1]))
		procs[pid] = paneProc{tpgid: tpgid, comm: strings.TrimSpace(parts[2]), foreground: strings.TrimSpace(parts[3])}
	}
	return procs
}

// shellNames are the programs a pane runs at its prompt.
var shellNames = map[string]bool{
	"sh": true, "bash": true, "zsh": true, "fish": true, "dash": true, "ksh": true,
	"mksh": true, "tcsh": true, "csh": true, "nu": true, "elvish": true, "xonsh": true,
	"pwsh": true, "ash": true, "yash": true, "oksh": true,
}

func programName(comm string) string {
	return strings.TrimPrefix(path.Base(strings.TrimSpace(comm)), "-")
}

// paneBusy decides whether a pane runs a program, and which.
func paneBusy(f paneFacts, procs map[int]paneProc) (string, bool) {
	if p, ok := procs[f.pid]; ok && f.pid > 0 {
		own := programName(p.comm)
		if p.tpgid > 0 && p.tpgid != f.pid {
			if fg := programName(p.foreground); fg != "" {
				return fg, true
			}
			return "a program", true
		}
		if !shellNames[own] {
			return own, true
		}
		return "", false
	}
	// No process to read: what the shell integration said, when it said it.
	if f.running != "" {
		return f.running, true
	}
	if f.promptKnown && !f.atPrompt {
		return "a program", true
	}
	return "", false
}

// lastLine is the last line of s, with the space around it trimmed.
func lastLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndex(s, "\n"); i >= 0 {
		return s[i+1:]
	}
	return s
}

// isTTY reports whether the person can answer a question. tty overrides the
// check when it is set; nil means read it from stdin.
func isTTY(tty *bool) bool {
	if tty != nil {
		return *tty
	}
	return term.IsTerminal(int(os.Stdin.Fd()))
}
