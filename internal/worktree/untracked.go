package worktree

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Gaurav-Gosain/tuios/internal/childproc"
)

// largeUntracked lists the untracked files of the work tree at root that are
// not ignored and are larger than limit bytes, sorted. Untracked is against
// the work tree's own index.
func largeUntracked(ctx context.Context, root string, limit int64) ([]string, error) {
	if limit <= 0 {
		return nil, nil
	}
	out, err := runCtx(ctx, root, nil, "ls-files", "-z", "--others", "--exclude-standard")
	if err != nil {
		return nil, err
	}
	var large []string
	for p := range strings.SplitSeq(out, "\x00") {
		if p == "" {
			continue
		}
		info, err := os.Lstat(filepath.Join(root, filepath.FromSlash(p)))
		if err == nil && info.Mode().IsRegular() && info.Size() > limit {
			large = append(large, p)
		}
	}
	slices.Sort(large)
	return large, nil
}

// runStdin is runCtx with stdin.
func runStdin(ctx context.Context, dir string, env []string, stdin string, args ...string) (string, error) {
	cmd := childproc.Git(ctx, dir, args...)
	cmd.Env = append(cmd.Env, env...)
	cmd.Stdin = strings.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("git %s: %w", args[0], ctx.Err())
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", args[0], msg)
	}
	return stdout.String(), nil
}
