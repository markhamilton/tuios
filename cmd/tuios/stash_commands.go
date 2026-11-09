package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"
	"github.com/Gaurav-Gosain/tuios/internal/plural"
	"github.com/spf13/cobra"
)

// This file holds the CLI half of the session stash: put a file in, list what is
// in, and get a file out when it has to cross a link. The point of the command
// is the path it prints, so the plain output puts that path on a line of its
// own and nothing else on that line, which is what a caller pipes into --attach.
//
// With a session on another machine, `stash put` reads the file here and
// sends its bytes, because the path means nothing there, and `stash get`
// brings a stashed file's bytes back here. Both are bounded at 8 MB. On this
// machine neither copies anything through the socket.

// stashEntryRow is one entry of a stash listing or the result of a put.
type stashEntryRow struct {
	Path       string `json:"path"`
	Name       string `json:"name"`
	Hash       string `json:"hash"`
	Bytes      int64  `json:"bytes"`
	MediaType  string `json:"media_type"`
	Kind       string `json:"kind"`
	Source     string `json:"source"`
	StoredAt   int64  `json:"stored_at"`
	Referenced bool   `json:"referenced"`
	Missing    bool   `json:"missing"`
}

// newStashCommand builds `tuios stash` and its two subcommands.
func newStashCommand() *cobra.Command {
	stashCmd := &cobra.Command{
		Use:   "stash",
		Short: "Store files for the session, so another agent can read them later",
		Long: `Put a file in the session's own store and get back a path anyone in the
session can open.

An attachment on a message is a path the sender owns. That is fast, because
nothing is copied, but the sender can delete the file and the reader then finds
nothing there. A stashed file is the daemon's instead. It is there until the
session is killed or the daemon stops, and then it is gone. Nothing survives a
restart.

Use it when you hand a file to another agent and will not keep it yourself. Use
a plain path when you will.`,
	}

	var putSession string
	var putJSON bool
	putCmd := &cobra.Command{
		Use:   "put <file>",
		Short: "Copy a file into the session store and print the stored path",
		Long: `Copy a file into the session's store and print where it now lives.

The daemon opens the file itself, on its own host and as the user that started
it, so the path must be absolute and readable by that user.

The store is content-addressed. Put the same bytes twice and you get the same
path back, and the second put stores nothing.

One file is capped at 16 MB and one session at 256 MB. A put that would pass the
session cap deletes stored files to make room, oldest first, and never one that
a message in the ring still points at. The count of deleted files is printed, so
you can see when something you stashed earlier has gone.`,
		Example: `  # Store a screenshot and hand the path to another agent
  path=$(tuios stash put /tmp/flame.png)
  tuios send-agent-message -w review --attach "$path" 'the hot path is in decode'

  # Store a log and see what the session now holds
  tuios stash put /var/log/build.log
  tuios stash list`,
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return runStashPut(putSession, args[0], putJSON)
		},
	}
	putCmd.Flags().StringVarP(&putSession, "session", "s", "", "Target session (default: this pane's session, else the most recently active)")
	putCmd.Flags().BoolVar(&putJSON, "json", false, "Output result as JSON")
	_ = putCmd.RegisterFlagCompletionFunc("session", completeSessionNames)

	var listSession string
	var listJSON bool
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List the files in the session store",
		Long: `List what the session's store holds, oldest first.

USED says a message still in the agent ring points at the file. Those are never
deleted to make room, so the first row without it is the next one to go.`,
		Example: `  # What is in the store, and how full is it
  tuios stash list

  # Every stored path, for a script
  tuios stash list --json | jq -r '.entries[].path'`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runStashList(listSession, listJSON)
		},
	}
	listCmd.Flags().StringVarP(&listSession, "session", "s", "", "Target session (default: this pane's session, else the most recently active)")
	listCmd.Flags().BoolVar(&listJSON, "json", false, "Output result as JSON")
	_ = listCmd.RegisterFlagCompletionFunc("session", completeSessionNames)

	var getSession string
	var getJSON bool
	getCmd := &cobra.Command{
		Use:   "get <stored-path> [file]",
		Short: "Copy a stashed file out of the session store, across a link",
		Long: `Copy one stashed file to a path here. The stored path is what 'stash put' or
'stash list' printed.

It exists for a session on another machine: a path in that machine's stash
cannot be opened here, so the bytes cross the link. A file over 8 MB is refused.
On this machine, open the stored path directly instead.

The copy is written to the file you name, or to the stored file's name in the
current directory. The path written is printed on a line of its own.`,
		Example: `  # Bring an attachment from a session on build here
  tuios stash get -s build:api /run/user/1000/tuios/stash/<id>/<hash>.png flame.png`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(_ *cobra.Command, args []string) error {
			out := ""
			if len(args) > 1 {
				out = args[1]
			}
			return runStashGet(getSession, args[0], out, getJSON)
		},
	}
	getCmd.Flags().StringVarP(&getSession, "session", "s", "", "Target session (default: this pane's session, else the most recently active)")
	getCmd.Flags().BoolVar(&getJSON, "json", false, "Output result as JSON")
	_ = getCmd.RegisterFlagCompletionFunc("session", completeSessionNames)

	stashCmd.AddCommand(putCmd, listCmd, getCmd)
	return stashCmd
}

// runStashPut copies a file into the session store.
func runStashPut(sessionName, path string, jsonOutput bool) error {
	t, err := dialSessionTarget(sessionName)
	if err != nil {
		return err
	}
	defer t.Close()

	// The path is sent as given. Making it absolute here would resolve it against this process's directory, and the daemon may
	// be somewhere else; the daemon refuses a relative path and says so,
	// which is the honest answer.
	params := t.params(map[string]any{"path": path})

	raw, err := t.client.Call("stash-put", params)
	if err != nil {
		return reportVerbError(t.explain("stash-put", err), jsonOutput)
	}
	if jsonOutput {
		return printVerbResultOn(t, raw, jsonOutput)
	}
	return printStashPut(os.Stdout, os.Stderr, raw)
}

// runStashGet copies a stashed file out of a session store to a path here.
func runStashGet(sessionName, stored, out string, jsonOutput bool) error {
	t, err := dialSessionTarget(sessionName)
	if err != nil {
		return err
	}
	defer t.Close()

	raw, err := t.client.Call("stash-get", t.params(map[string]any{"path": stored}))
	if err != nil {
		return reportVerbError(t.explain("stash-get", err), jsonOutput)
	}
	var res struct {
		Name    string `json:"name"`
		Bytes   int64  `json:"bytes"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}
	data, err := base64.StdEncoding.DecodeString(res.Content)
	if err != nil {
		return fmt.Errorf("the daemon sent content this build cannot read: %w", err)
	}
	if out == "" {
		out = res.Name
	}
	if err := os.WriteFile(out, data, 0o600); err != nil {
		return fmt.Errorf("cannot write %s: %w", out, err)
	}
	if jsonOutput {
		outputJSON(stashGetJSON(out, len(data), stored))
		return nil
	}
	printStashGet(os.Stdout, os.Stderr, out, int64(len(data)))
	return nil
}

// stashGetJSON is the --json result of stash get.
func stashGetJSON(path string, size int, stored string) map[string]any {
	return map[string]any{"success": true, "message": "file copied", "path": path, "bytes": size, "stored": stored}
}

// printStashGet prints the written path to out and the size note to notes, so
// stdout carries only the path.
func printStashGet(out, notes io.Writer, path string, size int64) {
	fmt.Fprintln(out, path)
	fmt.Fprintf(notes, "copied %s\n", stashBytes(size))
}

// printStashPut prints the stored path to out and the note about what the put
// did to notes. Stdout holds only the path, so `path=$(tuios stash put f)`
// captures a path that --attach can use.
func printStashPut(out, notes io.Writer, raw json.RawMessage) error {
	var res struct {
		Path      string `json:"path"`
		Bytes     int64  `json:"bytes"`
		Deduped   bool   `json:"deduped"`
		Evicted   int    `json:"evicted"`
		Evictions uint64 `json:"evictions"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}
	fmt.Fprintln(out, res.Path)
	note := fmt.Sprintf("stored %s", stashBytes(res.Bytes))
	if res.Deduped {
		note = fmt.Sprintf("already stored, %s", stashBytes(res.Bytes))
	}
	if res.Evicted > 0 {
		note += fmt.Sprintf(", dropped %s to make room", plural.Count(res.Evicted, "older file"))
	} else if res.Evictions > 0 {
		note += fmt.Sprintf(", %s dropped so far in this session", plural.Count(res.Evictions, "file"))
	}
	fmt.Fprintln(notes, note)
	return nil
}

// runStashList prints what the session store holds.
func runStashList(sessionName string, jsonOutput bool) error {
	t, err := dialSessionTarget(sessionName)
	if err != nil {
		return err
	}
	defer t.Close()

	raw, err := t.client.Call("stash-list", t.params(map[string]any{"session": sessionName}))
	if err != nil {
		return reportVerbError(t.explain("stash-list", err), jsonOutput)
	}
	if jsonOutput {
		return printVerbResultOn(t, raw, jsonOutput)
	}
	return printStashList(os.Stdout, raw)
}

func printStashList(w io.Writer, raw json.RawMessage) error {
	var res struct {
		Dir      string          `json:"dir"`
		Entries  []stashEntryRow `json:"entries"`
		Total    int             `json:"total"`
		Bytes    int64           `json:"bytes"`
		Evicted  uint64          `json:"evicted"`
		MaxBytes int64           `json:"max_bytes"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}
	if len(res.Entries) == 0 {
		fmt.Fprintln(w, "The session store is empty. 'tuios stash put <file>' puts a file in it.")
		return nil
	}

	rows := make([][]string, 0, len(res.Entries))
	for _, e := range res.Entries {
		used := ""
		if e.Referenced {
			used = "yes"
		}
		state := ""
		if e.Missing {
			state = "MISSING"
		}
		rows = append(rows, []string{
			e.Name[:min(12, len(e.Name))],
			stashBytes(e.Bytes),
			e.MediaType,
			used,
			e.Source,
			state,
		})
	}

	t := table.New().
		Border(lipgloss.RoundedBorder()).
		BorderStyle(lipgloss.NewStyle().Foreground(lipgloss.Color("8"))).
		Headers("FILE", "SIZE", "TYPE", "USED", "FROM", "").
		Rows(rows...).
		StyleFunc(func(row, col int) lipgloss.Style {
			base := lipgloss.NewStyle().Padding(0, 1)
			if row == table.HeaderRow {
				return base.Bold(true).Foreground(lipgloss.Color("12"))
			}
			switch col {
			case 0, 2, 4:
				return base.Foreground(lipgloss.Color("8"))
			default:
				return base
			}
		})

	lipgloss.Fprintln(w, t.Render())
	fmt.Fprintf(w, "\n%s, %s of %s, in %s\n",
		plural.Count(res.Total, "file"), stashBytes(res.Bytes), stashBytes(res.MaxBytes), res.Dir)
	if res.Evicted > 0 {
		fmt.Fprintf(w, "%s %s dropped to make room. USED marks the ones a message still points at.\n", plural.Count(res.Evicted, "file"), plural.Word(res.Evicted, "was", "were"))
	}
	fmt.Fprintln(w, "Every file here is deleted when the session is killed or the daemon stops.")
	return nil
}

// stashBytes renders a size the way a person reads one.
func stashBytes(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/float64(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/float64(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}
