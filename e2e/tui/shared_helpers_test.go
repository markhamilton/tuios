package tuie2e

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Gaurav-Gosain/tuitest"
	"tuios.dev/shot"
)

// Helpers several tests share.

// tuiosCLIEnv is tuiosCLI with extra environment.
func tuiosCLIEnv(t *testing.T, base string, env []string, args ...string) (string, error) {
	t.Helper()
	pinPreV080Looks(t, base)
	cmd := exec.Command(tuiosBin, args...)
	cmd.Env = append(os.Environ(), "SHELL=/bin/sh")
	for _, key := range xdgKeys {
		cmd.Env = append(cmd.Env, key+"="+xdgDir(base, key))
	}
	cmd.Env = append(cmd.Env, env...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// containsAll reports whether s holds every part.
func containsAll(s string, parts ...string) bool {
	for _, p := range parts {
		if !contains(s, p) {
			return false
		}
	}
	return true
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// cliWithin is tuiosCLIEnv with a deadline, for the reads a wait loop polls.
// A daemon that stops answering then shows as a failed read, and the loop's
// own deadline still holds.
func cliWithin(t *testing.T, base string, env []string, args ...string) (string, error) {
	t.Helper()
	pinPreV080Looks(t, base)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, tuiosBin, args...)
	cmd.Env = append(os.Environ(), "SHELL=/bin/sh")
	for _, key := range xdgKeys {
		cmd.Env = append(cmd.Env, key+"="+xdgDir(base, key))
	}
	cmd.Env = append(cmd.Env, env...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// tuiosCLIInDir is tuiosCLIEnv run from dir, which is how a command finds the
// repository the person is in.
func tuiosCLIInDir(t *testing.T, base, dir string, env []string, args ...string) (string, error) {
	t.Helper()
	pinPreV080Looks(t, base)
	cmd := exec.Command(tuiosBin, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "SHELL=/bin/sh")
	for _, key := range xdgKeys {
		cmd.Env = append(cmd.Env, key+"="+xdgDir(base, key))
	}
	cmd.Env = append(cmd.Env, env...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func tailString(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

// railLists waits until the screen holds every name in want and none in not.
func railLists(t *testing.T, term *tuitest.Terminal, why string, want, not []string) {
	t.Helper()
	if err := term.WaitFor(func(s tuitest.Screen) bool {
		text := s.Text()
		for _, w := range want {
			if !strings.Contains(text, w) {
				return false
			}
		}
		for _, n := range not {
			if strings.Contains(text, n) {
				return false
			}
		}
		return true
	}, uiTimeout); err != nil {
		t.Fatalf("ASSERTION: %s: want %q on screen and %q off it: %v\n%s", why, want, not, err, term.Snapshot())
	}
}

// waitRailCurrent blocks until the rail marks want as the attached session.
func waitRailCurrent(t *testing.T, term *tuitest.Terminal, want string) {
	t.Helper()
	if err := term.WaitFor(func(s tuitest.Screen) bool { return railCurrentIs(s, want) }, uiTimeout); err != nil {
		t.Fatalf("ASSERTION: the rail never marked %q as the attached session: %v\n%s", want, err, term.Snapshot())
	}
}

// railCurrentIs reports whether the rail marks the session named want as the
// one this client is on: its row wears the focus mark in the rail's gutter,
// which is the screen's first column for a left rail.
func railCurrentIs(s tuitest.Screen, want string) bool {
	r := railRowOf(s, want)
	if r < 0 {
		return false
	}
	return strings.HasPrefix(s.Line(r), "▎")
}

// railShows blocks until the rail shows text.
func railShows(t *testing.T, term *tuitest.Terminal, want string) {
	t.Helper()
	if err := term.WaitFor(func(s tuitest.Screen) bool {
		return strings.Contains(s.Text(), want)
	}, uiTimeout); err != nil {
		t.Fatalf("the rail never showed %q: %v\n%s", want, err, term.Snapshot())
	}
}

// waitForCapture polls capture-pane with the given target flags until the
// pane shows want.
func waitForCapture(t *testing.T, base string, env, target []string, want string) {
	t.Helper()
	deadline := time.Now().Add(shellTimeout)
	var out string
	for time.Now().Before(deadline) {
		args := append([]string{"capture-pane"}, target...)
		out, _ = tuiosCLIEnv(t, base, env, args...)
		if strings.Contains(out, want) {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("ASSERTION: the pane never showed %q:\n%s", want, out)
}

// waitForAttention polls this machine's list-attention until it holds want.
func waitForAttention(t *testing.T, base string, env []string, want ...string) string {
	t.Helper()
	deadline := time.Now().Add(uiTimeout * 3)
	var out string
	for time.Now().Before(deadline) {
		out, _ = tuiosCLIEnv(t, base, env, "list-attention")
		if containsAll(out, want...) {
			return out
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("list-attention never held %q:\n%s", want, out)
	return out
}

// tcpListenPorts is the set of TCP ports, sorted, that the process pid holds
// a listening socket on. It matches the socket inodes under /proc/pid/fd to
// the LISTEN rows of /proc/pid/net/tcp and tcp6.
func tcpListenPorts(t *testing.T, pid int) []int {
	t.Helper()
	fdDir := fmt.Sprintf("/proc/%d/fd", pid)
	fds, err := os.ReadDir(fdDir)
	if err != nil {
		t.Fatalf("read %s: %v", fdDir, err)
	}
	inodes := map[string]bool{}
	for _, fd := range fds {
		link, err := os.Readlink(filepath.Join(fdDir, fd.Name()))
		if err != nil {
			continue
		}
		if inode, ok := strings.CutPrefix(link, "socket:["); ok {
			inodes[strings.TrimSuffix(inode, "]")] = true
		}
	}
	var ports []int
	for _, table := range []string{"tcp", "tcp6"} {
		path := fmt.Sprintf("/proc/%d/net/%s", pid, table)
		f, err := os.Open(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		sc := bufio.NewScanner(f)
		sc.Scan() // the header
		for sc.Scan() {
			// sl local_address rem_address st tx:rx tr:when retrnsmt uid timeout inode
			fields := strings.Fields(sc.Text())
			if len(fields) < 10 || fields[3] != "0A" || !inodes[fields[9]] {
				continue
			}
			_, hexPort, ok := strings.Cut(fields[1], ":")
			if !ok {
				continue
			}
			p, err := strconv.ParseUint(hexPort, 16, 16)
			if err != nil {
				continue
			}
			if !slices.Contains(ports, int(p)) {
				ports = append(ports, int(p))
			}
		}
		err = sc.Err()
		_ = f.Close()
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
	}
	slices.Sort(ports)
	return ports
}

// cliEnv is the environment of a tuios command for the hub under base. The
// pane variables of a tuios the suite runs in are left out, so the command
// is the person's, outside every pane.
func cliEnv(base string) []string {
	var env []string
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "TUIOS_") && !strings.HasPrefix(kv, "TUIOS_E2E") {
			continue
		}
		env = append(env, kv)
	}
	env = append(env, "SHELL=/bin/sh")
	for _, key := range xdgKeys {
		env = append(env, key+"="+xdgDir(base, key))
	}
	return env
}

// openPaletteRow opens the palette with the prefix key, which works in both
// modes, filters it to row and runs the first match.
func openPaletteRow(t *testing.T, term *tuitest.Terminal, query, row string) {
	t.Helper()
	if err := term.SendKeys(tuitest.Ctrl('b'), "P"); err != nil {
		t.Fatal(err)
	}
	waitPaletteOpen(t, term, "for "+row)
	if err := term.SendKeys(query); err != nil {
		t.Fatal(err)
	}
	if err := term.WaitForText(row, uiTimeout); err != nil {
		t.Fatalf("palette never filtered to %q: %v\n%s", row, err, term.Snapshot())
	}
	if err := term.SendKeys(tuitest.Enter); err != nil {
		t.Fatal(err)
	}
}

// randomFile writes size random bytes to path and returns their sha256.
func randomFile(t *testing.T, path string, size int) string {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	h := sha256.New()
	if _, err := io.CopyN(io.MultiWriter(f, h), rand.Reader, int64(size)); err != nil {
		t.Fatalf("fill %s: %v", path, err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close %s: %v", path, err)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// saveSyncArtifact keeps one run's output for a person to read.
func saveSyncArtifact(t *testing.T, name, out string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(artifactDir(t), name), []byte(out), 0o644); err != nil { //nolint:gosec // a test artifact
		t.Logf("save %s: %v", name, err)
	}
}

// saveArtifact writes a test's record where TUIOS_E2E_FRAMES points.
func saveTransferArtifact(t *testing.T, name string, v any) {
	t.Helper()
	dir := os.Getenv("TUIOS_E2E_FRAMES")
	if dir == "" {
		return
	}
	b, _ := json.MarshalIndent(v, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, name+".json"), b, 0o644); err != nil {
		t.Logf("could not save %s: %v", name, err)
	}
}

// copiesFrame saves the screen as a PNG and as text under TUIOS_E2E_FRAMES.
func copiesFrame(t *testing.T, term *tuitest.Terminal, name string) {
	t.Helper()
	saveFrame(t, term, name)
	if dir := os.Getenv("TUIOS_E2E_FRAMES"); dir != "" {
		savePNG(t, term.Screen(), shot.XTermPalette(), dir, name)
	}
}

func dialFileVerbs(t *testing.T, base string) *fileVerbConn {
	t.Helper()
	conn, err := net.Dial("unix", hubSocket(base))
	if err != nil {
		t.Fatalf("dial the daemon: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return &fileVerbConn{t: t, conn: conn, br: bufio.NewReaderSize(conn, 1<<20)}
}

// fileVerbConn is one JSON verb connection to a daemon.
type fileVerbConn struct {
	t    *testing.T
	conn net.Conn
	br   *bufio.Reader
	id   int
}

type verbFailure struct{ Code, Message string }

// typeLine types a line into the pane and presses Enter.
func typeInPane(t *testing.T, base, session, pane, line string) {
	t.Helper()
	if out, err := tuiosCLI(t, base, "send-text", "-s", session, "-w", pane, line+"\n"); err != nil {
		t.Fatalf("send-text: %v\n%s", err, out)
	}
}

// runTagged types cmd and waits for it to exit 0, by a mark that names tag,
// so a run after another one does not match the first one's mark.
func runTagged(t *testing.T, term *tuitest.Terminal, tag, cmd string) {
	t.Helper()
	typeLine(t, term, cmd+`; echo "DONE""-`+tag+`-RC=$?"`)
	if err := term.WaitForText("DONE-"+tag+"-RC=0", shellTimeout); err != nil {
		t.Fatalf("%s did not finish cleanly: %v\n%s", cmd, err, term.Snapshot())
	}
}

// paneHarness is the harness the daemon holds for each pane, by window id.
func paneHarness(t *testing.T, base, session string) map[string]string {
	t.Helper()
	out, err := tuiosCLI(t, base, "list-agents", "-s", session, "--json")
	if err != nil {
		t.Fatalf("list-agents: %v\n%s", err, out)
	}
	var listing struct {
		Agents []struct {
			ID      string `json:"window_id"`
			Harness string `json:"harness_id"`
		} `json:"agents"`
	}
	if err := json.Unmarshal([]byte(out), &listing); err != nil {
		t.Fatalf("list-agents JSON: %v\n%s", err, out)
	}
	got := map[string]string{}
	for _, w := range listing.Agents {
		got[w.ID] = w.Harness
	}
	return got
}

// syncNextBinary builds the "new" version once per run.
func syncNextBinary(t *testing.T) string {
	t.Helper()
	syncNextOnce.Do(func() {
		dir, err := os.MkdirTemp("", "tuios-e2e-sync-next")
		if err != nil {
			syncNextErr = err
			return
		}
		syncNextBin = filepath.Join(dir, "tuios")
		build := exec.Command("go", "build", "-trimpath", "-ldflags", "-X main.version="+syncNextVersion, "-o", syncNextBin, "./cmd/tuios")
		build.Dir = "../.."
		out, err := build.CombinedOutput()
		if err != nil {
			syncNextErr = err
			t.Logf("build: %s", out)
		}
	})
	if syncNextErr != nil {
		t.Fatalf("build the new version: %v", syncNextErr)
	}
	return syncNextBin
}

const syncNextVersion = "e2e-sync-next"

func forbidden(err error) bool {
	var vf *verbFailure
	return errors.As(err, &vf) && vf.Code == "forbidden"
}

func hubSocket(base string) string {
	return filepath.Join(xdgDir(base, "XDG_RUNTIME_DIR"), "tuios", "tuios.sock")
}

func (c *fileVerbConn) call(verb string, params any) (json.RawMessage, error) {
	c.id++
	line, _ := json.Marshal(map[string]any{"id": c.id, "verb": verb, "params": params})
	_ = c.conn.SetDeadline(time.Now().Add(60 * time.Second))
	if _, err := c.conn.Write(append(line, '\n')); err != nil {
		return nil, err
	}
	reply, err := c.br.ReadBytes('\n')
	if err != nil {
		return nil, err
	}
	var resp struct {
		Result json.RawMessage `json:"result"`
		Error  *verbFailure    `json:"error"`
	}
	if err := json.Unmarshal(reply, &resp); err != nil {
		return nil, fmt.Errorf("decode %q: %w", reply, err)
	}
	if resp.Error != nil {
		return nil, resp.Error
	}
	return resp.Result, nil
}

// mustRaw is must that returns the result as it came.
func (c *fileVerbConn) mustRaw(verb string, params any) json.RawMessage {
	c.t.Helper()
	raw, err := c.call(verb, params)
	if err != nil {
		c.t.Fatalf("%s: %v", verb, err)
	}
	return raw
}

func (c *fileVerbConn) must(verb string, params any, out any) {
	c.t.Helper()
	raw, err := c.call(verb, params)
	if err != nil {
		c.t.Fatalf("%s: %v", verb, err)
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			c.t.Fatalf("decode %s: %v\n%s", verb, err, raw)
		}
	}
}

var (
	syncNextOnce sync.Once
	syncNextBin  string
	syncNextErr  error
)

// daemonPID is the pid of the daemon of base.
func daemonPID(t *testing.T, base string) int {
	t.Helper()
	var hello struct {
		PID int `json:"pid"`
	}
	dialFileVerbs(t, base).must("hello", nil, &hello)
	if hello.PID <= 0 {
		t.Fatalf("the daemon gave no pid")
	}
	return hello.PID
}
func (e *verbFailure) Error() string { return e.Code + ": " + e.Message }
