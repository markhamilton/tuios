package tuie2e

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// procTicks is a process's user and system CPU time, in clock ticks.
func procTicks(t *testing.T, pid int) int64 {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		t.Skipf("no /proc here: %v", err)
	}
	// The command name is in parentheses and may hold spaces.
	f := strings.Fields(string(b[strings.LastIndexByte(string(b), ')')+2:]))
	utime, _ := strconv.ParseInt(f[11], 10, 64)
	stime, _ := strconv.ParseInt(f[12], 10, 64)
	return utime + stime
}

// procReads is how many read calls a process has made, from /proc.
func procReads(t *testing.T, pid int) int64 {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "io"))
	if err != nil {
		t.Skipf("no /proc io here: %v", err)
	}
	for line := range strings.SplitSeq(string(b), "\n") {
		if rest, ok := strings.CutPrefix(line, "syscr:"); ok {
			n, _ := strconv.ParseInt(strings.TrimSpace(rest), 10, 64)
			return n
		}
	}
	t.Fatalf("no syscr in /proc/%d/io", pid)
	return 0
}
