package tuie2e

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Gaurav-Gosain/tuitest"
)

// The cockpit of DESIGN.md frame (a), built for real: twelve agents on three
// machines, one approval held, three branches ready for review with their
// pull requests, a fan of three attempts, and an agent running two
// subagents. The local machine runs the worktrees and the fan, build and gpu
// are second daemons reached through the ssh stand-in.

// frameAPR is the pull request the fake gh answers for a branch: number,
// state and checks, as gh pr view --json prints them.
var frameAPR = map[string]string{
	"docs/cli-flags": `{"number":884,"url":"https://github.example/o/r/pull/884","state":"OPEN","statusCheckRollup":[` +
		`{"__typename":"CheckRun","name":"test","status":"COMPLETED","conclusion":"SUCCESS"}]}`,
	"feat/sixel-scroll": `{"number":881,"url":"https://github.example/o/r/pull/881","state":"OPEN","statusCheckRollup":[` +
		`{"__typename":"CheckRun","name":"test","status":"COMPLETED","conclusion":"FAILURE"},` +
		`{"__typename":"CheckRun","name":"lint","status":"COMPLETED","conclusion":"SUCCESS"}]}`,
}

// frameAWorktree is one local agent in a worktree session of its own.
type frameAWorktree struct {
	session, branch, name, harness, state, message string
	meta                                           []string
}

var frameAWorktrees = []frameAWorktree{
	{"auth", "fix/token-refresh", "api-auth", "claude-code", "", "", []string{"model=opus 4.5", "context=62%", "cost=$1.84", "diff=+214/-37"}},
	{"docs", "docs/cli-flags", "docs-cli", "codex", "done", "", []string{"diff=+88/-12"}},
	{"sixel", "feat/sixel-scroll", "vt-sixel", "claude-code", "done", "", []string{"diff=+412/-160"}},
	{"rail", "feat/rail-tokens", "rail-tokens", "opencode", "done", "", []string{"diff=+36/-4"}},
	{"migrate", "chore/config-v3", "migrate-cfg", "claude-code", "working", "", []string{"diff=+230/-198"}},
}

// frameARemote is one agent on another machine.
type frameARemote struct {
	host, name, harness, state, message string
	meta                                []string
}

var frameAFar = []frameARemote{
	{"build", "ws-reconnect", "codex", "working", "Edit internal/federation/link.go", []string{"diff=+54/-11"}},
	{"build", "release-notes", "goose", "idle", "", nil},
	{"gpu", "bench-idle", "claude-code", "working", "Bash go test -bench Idle -count 5", []string{"context=31%", "diff=+18/-2"}},
}

// frameAFan is the fan: its attempts, what each runs, and what each reports.
var frameAFan = []struct{ session, name, harness, message, diff string }{
	{"repo-cache", "cache-1", "gemini", "Bash go test ./internal/cache/...", "+142/-20"},
	{"repo-cache-2", "cache-2", "claude-code", "Edit internal/cache/lru.go", "+61/-9"},
	{"repo-cache-3", "cache-3", "codex", "Read internal/cache/evict.go", "+97/-31"},
}

// frameAJudge is the judge's part of the fake agents: ready at once, and on
// its task, the verdict of frame (g) through the real CLI.
const frameAJudge = `  "$TUIOS_E2E_TUIOS" set-agent-state idle -w "$TUIOS_PANE_ID"
  while IFS= read -r line; do
    case "$line" in *"You judge"*)
      "$TUIOS_E2E_TUIOS" set-agent-state working -w "$TUIOS_PANE_ID"
      "$TUIOS_E2E_TUIOS" fan verdict repo-cache --pick repo-cache-2 \
        --summary "Keep B. It is the smallest change, and the only attempt that passes the check." \
        --reason "A takes a read lock on a plain Mutex, so the check fails." \
        --reason "C adds a shard type the task did not ask for, and its cache keeps the Mutex." \
        --reason "Take TestEvictUnderLoad from A. B has no test for eviction while Get runs." \
        --note "repo-cache=Takes RLock on a Mutex." --note "repo-cache=Adds TestEvictUnderLoad." \
        --note "repo-cache-2=Smallest change. Public API unchanged." --note "repo-cache-2=No test for eviction during Get." \
        --note "repo-cache-3=Adds a shard type." --note "repo-cache-3=Leaves the cache on a Mutex." >> "$TUIOS_E2E_JUDGE_LOG" 2>&1
      "$TUIOS_E2E_TUIOS" set-agent-state done -w "$TUIOS_PANE_ID"
      ;;
    esac
  done`

// frameALRU is the cache the fan's attempts change.
const frameALRU = `package cache

import "sync"

// Cache is a least recently used cache.
type Cache struct {
	mu    sync.Mutex
	items map[string]string
}

// Get returns the value of key.
func (c *Cache) Get(key string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.items[key]
}
`

// claudeAuthPane is what api-auth's pane prints: Claude Code at its
// permission prompt, as the live box should show it.
const claudeAuthPane = "\033[32m●\033[0m \033[1mUpdate(internal/auth/token.go)\033[0m\n  \033[2m└  Updated with 12 additions and 3\n     removals\033[0m\n\n\033[33m●\033[0m \033[1mBash(go test ./internal/auth/...\n       -run Token -count=1)\033[0m\n\n \033[33;1mBash command\033[0m\n Do you want to proceed?\n \033[36m❯ 1. Yes\033[0m\n   2. Yes, and do not ask again for\n      go test commands\n   3. No, and tell Claude what to do\n"

// frameARig is the scenario's machine here: its isolation root and its
// repository.
type frameARig struct{ base, repo string }

// frameGDiffs are what each attempt of the fan wrote: A changes the most,
// B the least, C adds a file of its own.
var frameGDiffs = map[string]map[string]string{
	"repo-cache": {
		"internal/cache/lru.go":      strings.Replace(frameALRU, "\tc.mu.Lock()\n\tdefer c.mu.Unlock()", "\tc.mu.RLock()\n\tdefer c.mu.RUnlock()\n\tc.hits.Add(1)", 1),
		"internal/cache/lru_test.go": "package cache\n\nimport \"testing\"\n\nfunc TestEvictUnderLoad(t *testing.T) {}\n",
		"internal/cache/stats.go":    "package cache\n\n// Stats reads the hits.\nfunc (c *Cache) Stats() int { return 0 }\n",
	},
	"repo-cache-2": {
		"internal/cache/lru.go": strings.Replace(frameALRU, "\tmu    sync.Mutex", "\tmu    sync.RWMutex", 1),
	},
	"repo-cache-3": {
		".env":                    "SECRET=1\n",
		"internal/cache/shard.go": "package cache\n\nimport \"sync\"\n\ntype shard struct {\n\tmu sync.RWMutex\n\titems map[string]string\n}\n",
		"internal/cache/lru.go":   strings.Replace(frameALRU, "// Get returns the value of key.", "// Get returns the value of key, from its shard.", 1),
	},
}

// waitClientOn waits until the daemon lists term's client as attached to
// session.
func waitClientOn(t *testing.T, base string, term *tuitest.Terminal, session string) {
	t.Helper()
	deadline := time.Now().Add(uiTimeout)
	for {
		out, _ := tuiosCLI(t, base, "list-clients", "--json")
		var rows []struct {
			PID     int    `json:"pid"`
			Session string `json:"session"`
		}
		if json.Unmarshal([]byte(out), &rows) == nil {
			for _, r := range rows {
				if r.PID == term.Pid() && r.Session == session {
					return
				}
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("ASSERTION: the client never went to %s: %s\n%s", session, out, term.Snapshot())
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// fleetHasRow reports whether a row of the fleet column is named name: the
// name is the first word after the row's marks.
func fleetHasRow(s tuitest.Screen, name string) bool {
	_, rows := s.Size()
	for r := range rows {
		line := []rune(s.Line(r))
		fleet := strings.Fields(string(line[:min(len(line), 56)]))
		for i, f := range fleet {
			if i < 3 && f == name {
				return true
			}
		}
	}
	return false
}

// fanIgnoredHashes reads, through compare-fan, the ignored_hash of every
// attempt of session's fan but session itself.
func fanIgnoredHashes(t *testing.T, base, session string) map[string]string {
	t.Helper()
	params, _ := json.Marshal(map[string]any{"session": session, "detail": true})
	out, err := tuiosCLI(t, base, "call", "compare-fan", string(params))
	if err != nil {
		t.Fatalf("compare-fan: %v: %s", err, out)
	}
	var res struct {
		Rows []struct {
			Session     string `json:"session"`
			IgnoredHash string `json:"ignored_hash"`
		} `json:"rows"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("compare-fan output: %v: %s", err, out)
	}
	hashes := map[string]string{}
	for _, r := range res.Rows {
		if r.Session != session {
			if r.IgnoredHash == "" {
				t.Fatalf("compare-fan gave %s no ignored_hash: %s", r.Session, out)
			}
			hashes[r.Session] = r.IgnoredHash
		}
	}
	return hashes
}
