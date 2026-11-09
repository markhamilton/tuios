package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Gaurav-Gosain/tuios/internal/session"
)

// The Inbox on the command line: what is waiting for the person, in every
// session, in the order the TUI's Inbox shows it.

// attentionRow is one item of the list-attention result.
type attentionRow struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Session   string `json:"session"`
	Window    string `json:"window"`
	Workspace int    `json:"workspace"`
	Harness   string `json:"harness"`
	Name      string `json:"name"`
	Summary   string `json:"summary"`
	// RiskyFlags are, on a resume row, the flags that turn approvals off.
	RiskyFlags []string `json:"risky_flags"`
	Since      int64    `json:"since"`
	Thread     uint64   `json:"thread"`
	Count      int      `json:"count"`
	Seen       int      `json:"seen"`
	// RequestID is set while a harness hook holds the approval for an answer
	// from the Inbox.
	RequestID string `json:"request_id"`
	// Options are the answers a question ask-human put takes.
	Options []string `json:"options"`
	// SnoozedUntil is set on an item the person snoozed, listed with
	// --snoozed: when it opens again (unix nanoseconds), -1 for when it
	// changes.
	SnoozedUntil int64 `json:"snoozed_until"`
}

// attentionHeldNote ends the row of an approval a hook is holding, so the
// person knows the pane shows no prompt and where to answer it.
const attentionHeldNote = "held: answer in the Inbox"

// attentionGroupTitle is the heading a kind's rows sit under, the TUI Inbox's
// heading for it: a question put with ask-human sits under Questions with an
// agent's own, and a finished turn under Done.
func attentionGroupTitle(kind string) string {
	switch kind {
	case session.AttentionApproval:
		return "Approvals"
	case session.AttentionPlan:
		return "Plans"
	case session.AttentionQuestion, session.AttentionAsk:
		return "Questions"
	case session.AttentionMail:
		return "Mail"
	case session.AttentionErrored:
		return "Errored"
	case session.AttentionResume:
		return "Resume"
	case session.AttentionFinished:
		return "Done"
	case session.AttentionOutbox:
		return "Waiting to send"
	}
	return kind
}

// waitedFor is how long an item has waited, in words short enough for a
// column: 40s, 12m, 3h, 2d.
func waitedFor(since int64, now time.Time) string {
	if since <= 0 {
		return "?"
	}
	d := max(now.Sub(time.Unix(0, since)), 0)
	switch {
	case d < time.Minute:
		return strconv.Itoa(int(d.Seconds())) + "s"
	case d < time.Hour:
		return strconv.Itoa(int(d.Minutes())) + "m"
	case d < 24*time.Hour:
		return strconv.Itoa(int(d.Hours())) + "h"
	default:
		return strconv.Itoa(int(d.Hours())/24) + "d"
	}
}

func runListAttention(sessionName string, kinds []string, selector string, snoozed, jsonOutput bool) error {
	client, err := dialVerb()
	if err != nil {
		return reportVerbError(err, jsonOutput)
	}
	defer func() { _ = client.Close() }()
	params := map[string]any{}
	if sessionName != "" {
		params["session"] = sessionName
	}
	if len(kinds) > 0 {
		params["kinds"] = kinds
	}
	if selector != "" {
		params["select"] = selector
	}
	if snoozed {
		params["include_snoozed"] = true
	}
	raw, err := client.Call("list-attention", params)
	if err != nil {
		return reportVerbError(err, jsonOutput)
	}
	if jsonOutput {
		return printVerbResult(raw, true)
	}
	return printAttentionList(os.Stdout, raw, time.Now())
}

// printAttentionList prints the Inbox grouped by kind, oldest first, one row
// per item: how long it has waited, where it is, and what it says. Every name
// and summary was written by an agent, so each is kept to one plain line.
func printAttentionList(w io.Writer, raw json.RawMessage, now time.Time) error {
	var res struct {
		Items []attentionRow `json:"items"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}
	if len(res.Items) == 0 {
		fmt.Fprintln(w, "Nothing is waiting for you.")
		return nil
	}
	heading := ""
	waiting := 0
	for _, it := range res.Items {
		title := attentionGroupTitle(it.Kind)
		if it.SnoozedUntil != 0 {
			// Listed after everything open, under a heading of their own.
			title = "Snoozed"
		} else {
			waiting++
		}
		if title != heading {
			if heading != "" {
				fmt.Fprintln(w)
			}
			heading = title
			fmt.Fprintf(w, "%s\n", heading)
		}
		where := plainLine(it.Session)
		name := plainLine(it.Name)
		if name == "" && it.Window != "" {
			name = shortWindowID(it.Window)
		}
		if name != "" {
			where += "/" + name
		}
		summary := plainLine(it.Summary)
		if it.Count > 1 {
			summary = strings.TrimSpace(summary + fmt.Sprintf(" (%d)", it.Count))
		}
		if warn := session.ResumeWarning(it.RiskyFlags); warn != "" {
			summary = plainLine(warn) + ": " + summary
		}
		if it.Kind == session.AttentionMail && it.Seen > 0 {
			summary = strings.TrimSpace(summary + fmt.Sprintf(" [%d seen]", it.Seen))
		}
		fmt.Fprintf(w, "  %4s  %-6s %s", waitedFor(it.Since, now), "#"+it.ID, where)
		if summary != "" {
			fmt.Fprintf(w, "  %s", summary)
		}
		switch {
		case it.Kind == session.AttentionAsk:
			// The answers are what the question is; the keys are the Inbox's.
			opts := make([]string, len(it.Options))
			for i, o := range it.Options {
				opts[i] = strconv.Itoa(i+1) + " " + plainLine(o)
			}
			fmt.Fprintf(w, "  (%s; answer in the Inbox)", strings.Join(opts, ", "))
		case it.RequestID != "":
			fmt.Fprintf(w, "  (%s)", attentionHeldNote)
		}
		if it.SnoozedUntil != 0 {
			fmt.Fprintf(w, "  [%s]", snoozedUntilWords(it.SnoozedUntil, now))
		}
		fmt.Fprintln(w)
	}
	fmt.Fprintf(w, "\n%d waiting. Open the agents view with the prefix key then i, or jump to the oldest with the prefix key then o.\n", waiting)
	return nil
}

// snoozedUntilWords says when a snoozed item opens again.
func snoozedUntilWords(until int64, now time.Time) string {
	if until < 0 {
		return "snoozed until it changes"
	}
	at := time.Unix(0, until).In(now.Location())
	if y, m, d := now.Date(); at.Year() == y && at.Month() == m && at.Day() == d {
		return "snoozed until " + at.Format("15:04")
	}
	return "snoozed until " + at.Format("Mon Jan 2 15:04")
}

// newListAttentionCommand is `tuios list-attention`.
func newListAttentionCommand() *cobra.Command {
	var sessionName, selector string
	var kinds []string
	var jsonOutput, snoozed bool
	cmd := &cobra.Command{
		Use:   "list-attention",
		Short: "List what is waiting for you in every session: the Inbox",
		Long: `List the Inbox: every approval and question an agent is blocked on, mail to
you, errored agents, conversations a restart left to resume, and finished turns
nobody has looked at, in every session. Rows are grouped Approvals,
Questions, Mail, Errored, Resume, Finished, oldest first, with how long each has
waited.

An item closes by itself when what opened it stops being true: the agent
leaves needs_input or errored, the mail is read, a client focuses the pane
that finished, or the conversation is resumed ('tuios resume-agent').
Dismissing one is for the person at an attached client, from the Inbox
(prefix i), and so are snoozing, marking unread and undo: they have no command
here. --snoozed also lists the items the person snoozed, after the rest, with
when each opens again. 'tuios subscribe --types attention' streams every
change.`,
		Example: `  # What needs me?
  tuios list-attention

  # Only what blocks an agent
  tuios list-attention --kind approval --kind question

  # Only the codex agents of one fan-out
  tuios list-attention --select 'harness:codex group:fan/add-retry'

  # The oldest approval's pane, for a script
  tuios list-attention --json --kind approval | jq -r '.items[0].window'

  # Everything, the snoozed items too
  tuios list-attention --snoozed`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runListAttention(sessionName, kinds, selector, snoozed, jsonOutput)
		},
	}
	cmd.Flags().StringVar(&selector, "select", "", "Only the items a selector matches: space-separated key:value terms, such as 'harness:codex needs:you'")
	cmd.Flags().StringVarP(&sessionName, "session", "s", "", "Only this session (default: every session)")
	cmd.Flags().StringSliceVar(&kinds, "kind", nil, "Only these kinds: "+strings.Join(session.AttentionKindNames, ", "))
	cmd.Flags().BoolVar(&snoozed, "snoozed", false, "Also list the items snoozed in the Inbox, after the rest")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output result as JSON")
	_ = cmd.RegisterFlagCompletionFunc("session", completeSessionNames)
	_ = cmd.RegisterFlagCompletionFunc("kind", func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		return session.AttentionKindNames, cobra.ShellCompDirectiveNoFileComp
	})
	return cmd
}
