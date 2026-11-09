package tuie2e

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Gaurav-Gosain/tuios/internal/testutil"
)

// gitState is what the person's git use depends on: where HEAD points and
// at what, the branches, the index entries and the stash.
func gitState(t *testing.T, repo string) string {
	t.Helper()
	return strings.Join([]string{
		"HEAD -> " + testutil.Git(t, repo, "symbolic-ref", "HEAD"),
		"HEAD = " + testutil.Git(t, repo, "rev-parse", "HEAD"),
		"branches:\n" + testutil.Git(t, repo, "for-each-ref", "--format=%(refname) %(objectname)", "refs/heads/"),
		"index:\n" + testutil.Git(t, repo, "ls-files", "--stage"),
		"stash:\n" + testutil.Git(t, repo, "stash", "list"),
	}, "\n")
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

// cliTranscript runs a test's tuios calls in a repository and keeps them,
// with their output, for transcript.txt in the artifact directory.
type cliTranscript struct {
	t    *testing.T
	base string
	repo string
	b    strings.Builder
}

func newCLITranscript(t *testing.T, base, repo string) *cliTranscript {
	tr := &cliTranscript{t: t, base: base, repo: repo}
	t.Cleanup(func() {
		path := filepath.Join(artifactDir(t), "transcript.txt")
		if err := os.WriteFile(path, []byte(tr.b.String()), 0o644); err != nil {
			t.Logf("save the transcript: %v", err)
		}
	})
	return tr
}

func (tr *cliTranscript) log(format string, args ...any) {
	fmt.Fprintf(&tr.b, "# "+format+"\n", args...)
}

func (tr *cliTranscript) run(args ...string) (string, error) {
	tr.t.Helper()
	out, err := tuiosCLIIn(tr.t, tr.base, tr.repo, args...)
	rc := 0
	if err != nil {
		rc = 1
	}
	fmt.Fprintf(&tr.b, "$ tuios %s\n%s[rc=%d]\n", strings.Join(args, " "), out, rc)
	return out, err
}

func (tr *cliTranscript) ok(args ...string) string {
	tr.t.Helper()
	out, err := tr.run(args...)
	if err != nil {
		tr.t.Fatalf("tuios %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return out
}
