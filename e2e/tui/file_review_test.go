package tuie2e

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// What the review of the file verbs found, each as the attack or the load
// that shows it.

// fakeFar is a daemon socket on the far machine that answers verbs from a
// table. Each handler returns the result, or an error code, and may take the
// connection over after the reply (open-file-stream).
type fakeFar struct {
	t    *testing.T
	ln   net.Listener
	mu   sync.Mutex
	seen []string
}

type fakeReply struct {
	result   any
	code     string
	takeover func(c net.Conn)
}

func startFakeFar(t *testing.T, remote string, handle func(verb string, params map[string]any) fakeReply) *fakeFar {
	t.Helper()
	dir := filepath.Join(xdgDir(remote, "XDG_RUNTIME_DIR"), "tuios")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("unix", filepath.Join(dir, "tuios.sock"))
	if err != nil {
		t.Fatalf("listen as build's daemon: %v", err)
	}
	f := &fakeFar{t: t, ln: ln}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go f.serve(c, handle)
		}
	}()
	return f
}

func (f *fakeFar) serve(c net.Conn, handle func(string, map[string]any) fakeReply) {
	defer func() { _ = c.Close() }()
	br := bufio.NewReader(c)
	for {
		line, err := br.ReadBytes('\n')
		if err != nil {
			return
		}
		var req struct {
			ID     json.RawMessage `json:"id"`
			Verb   string          `json:"verb"`
			Params map[string]any  `json:"params"`
		}
		if json.Unmarshal(line, &req) != nil {
			return
		}
		f.mu.Lock()
		f.seen = append(f.seen, req.Verb)
		f.mu.Unlock()
		var r fakeReply
		if req.Verb == "hello" {
			r.result = map[string]any{"type": "hello", "protocol": 1, "min_protocol": 1, "daemon_version": "dev", "sessions": 0}
		} else {
			r = handle(req.Verb, req.Params)
		}
		resp := map[string]any{"id": req.ID}
		if r.code != "" {
			resp["error"] = map[string]any{"code": r.code, "message": "the fake far daemon does not do " + req.Verb}
		} else {
			resp["result"] = r.result
		}
		out, _ := json.Marshal(resp)
		if _, err := c.Write(append(out, '\n')); err != nil {
			return
		}
		if r.takeover != nil {
			r.takeover(c)
			return
		}
	}
}

func shaHex(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// callWithin is one verb call that must answer within d.
func (c *fileVerbConn) callWithin(d time.Duration, verb string, params any) (json.RawMessage, error) {
	c.id++
	line, _ := json.Marshal(map[string]any{"id": c.id, "verb": verb, "params": params})
	_ = c.conn.SetDeadline(time.Now().Add(d))
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
		return nil, err
	}
	if resp.Error != nil {
		return resp.Result, resp.Error
	}
	return resp.Result, nil
}

// TestFileVerbsOnAPipeAndADevice asks the file verbs about two things a
// folder can hold that are not an ordinary file: a named pipe, which blocks
// whoever opens it to read, and /dev/zero, which never ends. Every verb must
// answer at once.
func TestFileVerbsOnAPipeAndADevice(t *testing.T) {
	base := t.TempDir()
	killDaemon(t, base)
	if out, err := tuiosCLI(t, base, "new", "home", "--detach"); err != nil {
		t.Fatalf("start the daemon: %v\n%s", err, out)
	}
	pipe := filepath.Join(base, "pipe")
	if err := syscall.Mkfifo(pipe, 0o600); err != nil {
		t.Skipf("no named pipes here: %v", err)
	}
	type probe struct {
		Verb   string         `json:"verb"`
		Params map[string]any `json:"params"`
		Ms     int64          `json:"ms"`
		Answer string         `json:"answer"`
	}
	var probes []probe
	for _, p := range []probe{
		{Verb: "file-read", Params: map[string]any{"path": pipe}},
		{Verb: "file-hash", Params: map[string]any{"path": pipe}},
		{Verb: "open-file-stream", Params: map[string]any{"path": pipe}},
		{Verb: "file-hash", Params: map[string]any{"path": "/dev/zero"}},
		{Verb: "file-read", Params: map[string]any{"path": "/dev/zero"}},
	} {
		c := dialFileVerbs(t, base)
		start := time.Now()
		raw, err := c.callWithin(5*time.Second, p.Verb, p.Params)
		p.Ms = time.Since(start).Milliseconds()
		p.Answer = string(raw)
		if err != nil {
			p.Answer = err.Error()
		}
		probes = append(probes, p)
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() {
			t.Errorf("ASSERTION: %s on %v did not answer in 5 s", p.Verb, p.Params["path"])
		}
	}

	saveTransferArtifact(t, "file-verbs-not-a-file", map[string]any{"probes": probes})
}

// daemonReadBytes is how many bytes the daemon of base has read, from /proc.
func daemonReadBytes(t *testing.T, base string) int64 {
	t.Helper()
	var hello struct {
		PID int `json:"pid"`
	}
	dialFileVerbs(t, base).must("hello", nil, &hello)
	b, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(hello.PID), "io"))
	if err != nil {
		t.Skipf("no /proc io here: %v", err)
	}
	for line := range strings.SplitSeq(string(b), "\n") {
		if rest, ok := strings.CutPrefix(line, "rchar:"); ok {
			n, _ := strconv.ParseInt(strings.TrimSpace(rest), 10, 64)
			return n
		}
	}
	t.Fatalf("no rchar in the daemon's io")
	return 0
}
