package main

import (
	"io"
	"os"
	"testing"
)

// `tuios attach` used to tear down its interface, print why it was stopping,
// and then sit there for four seconds before the process ended, which reads as
// a client that has hung. The stall was fang's error renderer querying the
// terminal for its background color, so the tests here pin the mechanism that
// keeps a command failure away from that renderer, and pin the exit status each
// of the three ways an attach can end must produce.

// captureStdout runs fn with os.Stdout redirected to a pipe and returns what was
// written.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	fn()
	_ = w.Close()
	os.Stdout = orig
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read captured stdout: %v", err)
	}
	return string(data)
}

// TestAttachDetachesOthersByDefault: tuios attach takes the session's other
// clients off it unless -k is given, -d is still accepted (a no-op now), -d
// and -k together are refused, and the attach a restart makes keeps the
// others, since every client of the session comes back that way.
//
// Negative control: make attachDetachesOthers return attachDetachOthers, as
// before, and the bare-attach row fails.
func TestAttachDetachesOthersByDefault(t *testing.T) {
	t.Setenv(keepOthersEnv, "")
	saved := []bool{attachKeepOthers, attachDetachOthers, attachAfterRestart}
	t.Cleanup(func() {
		attachKeepOthers, attachDetachOthers, attachAfterRestart = saved[0], saved[1], saved[2]
	})

	cases := []struct {
		args    []string
		want    bool
		wantErr bool
	}{
		{nil, true, false},
		{[]string{"-d"}, true, false},
		{[]string{"--detach-others"}, true, false},
		{[]string{"-k"}, false, false},
		{[]string{"--keep-others"}, false, false},
		{[]string{"--after-restart"}, false, false},
		{[]string{"-d", "-k"}, false, true},
	}
	for _, c := range cases {
		cmd, _, err := newRootCommand().Find([]string{"attach"})
		if err != nil {
			t.Fatal(err)
		}
		if err := cmd.ParseFlags(c.args); err != nil {
			t.Fatalf("attach %v: %v", c.args, err)
		}
		err = cmd.ValidateFlagGroups()
		if c.wantErr {
			if err == nil {
				t.Errorf("attach %v was accepted", c.args)
			}
			continue
		}
		if err != nil {
			t.Errorf("attach %v: %v", c.args, err)
			continue
		}
		if got := attachDetachesOthers(); got != c.want {
			t.Errorf("attach %v detaches the others = %v, want %v", c.args, got, c.want)
		}
	}

	// TUIOS_ATTACH_KEEP_OTHERS=1 is -k for every attach, and an explicit -d
	// wins over it.
	t.Setenv(keepOthersEnv, "1")
	for _, c := range []struct {
		args []string
		want bool
	}{{nil, false}, {[]string{"-d"}, true}} {
		cmd, _, _ := newRootCommand().Find([]string{"attach"})
		if err := cmd.ParseFlags(c.args); err != nil {
			t.Fatal(err)
		}
		if got := attachDetachesOthers(); got != c.want {
			t.Errorf("%s=1, attach %v detaches the others = %v, want %v", keepOthersEnv, c.args, got, c.want)
		}
	}

	cmd, _, _ := newRootCommand().Find([]string{"attach"})
	if f := cmd.Flags().Lookup("keep-others"); f == nil || f.Shorthand != "k" || f.Hidden {
		t.Errorf("keep-others is not a shown flag with -k: %+v", f)
	}
	if f := cmd.Flags().Lookup("detach-others"); f == nil || !f.Hidden {
		t.Errorf("detach-others is missing or still shown in help: %+v", f)
	}
	if !attachOwnFlags["keep-others"] {
		t.Error("a restart would carry -k to the attach it makes")
	}
}
