package tuie2e

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

// Web Push, end to end: a real daemon, a stub push service on loopback (the
// test's own HTTP server), and this test as the phone. The phone registers
// with register-push over the phone link, with the nonce of attach-presence.
// The real Claude Code hook holds an approval, and the stub receives the
// push. The test decrypts it with the phone's private key, by its own
// implementation of RFC 8291 and RFC 8188, checks the payload, the headers
// and the VAPID JWT, then answers the approval and decrypts the close.
//
// It also checks who may register: no nonce, a made-up nonce, a link without
// the respond capability, and a process inside a pane holding the person's
// live nonce are all refused. Each refusal has its positive half: the same
// call with the person's nonce, over the phone link, from outside every pane,
// is taken.
//
// And delivery: a push service that answers 410 has its phone removed, one
// that answers 503 once gets the push on the retry, and a phone that asked for
// another kind gets nothing.
//
// And the review fixes: a presence made for one session can not register,
// list or remove a phone. Each registration, a replace, a removal over a link
// and a 410 open an Inbox item. A link can not replace a phone. The CLI reads
// the subscription from stdin and takes none of it on the command line. The
// VAPID sub names no machine. Every body is padded to a bucket. An approval's
// open and close live 3600 seconds. An ask's nine options of "<" keep their
// place in the payload.
//
// The artifact is webpush-transcript.json in TUIOS_E2E_FRAMES: every request
// the stub received, decrypted.
//
// NEGATIVE CONTROLS (e2e/tui/NEGATIVE_CONTROLS.md):
//   - the n.web.note call cut from pushNotifier.note: no push arrives.
//   - the requirePushPerson call cut from verbRegisterPush: a call with no
//     nonce registers.
//   - the register-push row cut from the link policy table: the link
//     without respond registers.
//   - the ErrGone arm cut from webPusher.deliver: the 410 phone stays.
//   - the retry cut (no RetryableError check): the 503 phone never gets it.
//   - the humanNonceFor check cut from requirePushPerson: a presence for one
//     session registers.
//   - the hostname put back in the default subject: the JWT sub names it.
//   - each notePhone call cut: its Inbox item never says so.
//   - register always may replace: the link replaces pixel.
//   - the old --endpoint, --p256dh and --auth flags put back: the command
//     line takes them.
//   - PaddedSize returns n: the body is not a bucket.
//   - pushTTLWaiting 120: the approval's TTL is 120.
//   - the old fitPayload: the ask's push has no options.

// stubPush is the push service: it records each request and answers with the
// status its path asks for.
type stubPush struct {
	srv  *http.Server
	ln   net.Listener
	mu   sync.Mutex
	reqs []stubReq
	// flaky counts the requests to /s/flaky, which fail once.
	flaky int
	got   chan stubReq
}

type stubReq struct {
	Path    string            `json:"path"`
	Headers map[string]string `json:"headers"`
	Body    []byte            `json:"-"`
	Status  int               `json:"status"`
	// Payload is the decrypted JSON, filled in by the test.
	Payload map[string]any `json:"payload,omitempty"`
}

func startStubPush(t *testing.T) *stubPush {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &stubPush{ln: ln, got: make(chan stubReq, 64)}
	s.srv = &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 8192))
		h := map[string]string{}
		for _, k := range []string{"Content-Encoding", "Content-Type", "Ttl", "Urgency", "Topic", "Authorization"} {
			h[k] = r.Header.Get(k)
		}
		status := http.StatusCreated
		s.mu.Lock()
		switch r.URL.Path {
		case "/s/gone":
			status = http.StatusGone
		case "/s/redirect":
			status = http.StatusTemporaryRedirect
			w.Header().Set("Location", "/s/landed")
		case "/s/flaky":
			s.flaky++
			if s.flaky == 1 {
				status = http.StatusServiceUnavailable
			}
		}
		req := stubReq{Path: r.URL.Path, Headers: h, Body: body, Status: status}
		s.reqs = append(s.reqs, req)
		s.mu.Unlock()
		w.WriteHeader(status)
		select {
		case s.got <- req:
		default:
		}
	})}
	go func() { _ = s.srv.Serve(ln) }()
	t.Cleanup(func() { _ = s.srv.Close() })
	return s
}

func (s *stubPush) url(path string) string { return "http://" + s.ln.Addr().String() + path }

func (s *stubPush) requests(path string) []stubReq {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []stubReq
	for _, r := range s.reqs {
		if r.Path == path {
			out = append(out, r)
		}
	}
	return out
}

// phoneKeys is a subscription's private half, kept by the test as a phone
// keeps it.
type phoneKeys struct {
	priv *ecdh.PrivateKey
	auth []byte
}

func newPhoneKeys(t *testing.T) phoneKeys {
	t.Helper()
	priv, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	auth := make([]byte, 16)
	_, _ = rand.Read(auth)
	return phoneKeys{priv: priv, auth: auth}
}

func (k phoneKeys) p256dh() string {
	return base64.RawURLEncoding.EncodeToString(k.priv.PublicKey().Bytes())
}
func (k phoneKeys) authB64() string { return base64.RawURLEncoding.EncodeToString(k.auth) }

// hmacSHA256 is HMAC-SHA-256(key, data).
func hmacSHA256(key, data []byte) []byte {
	m := hmac.New(sha256.New, key)
	m.Write(data)
	return m.Sum(nil)
}

// decrypt is the phone's side of RFC 8291 over one aes128gcm record, written
// from the RFCs and not from the daemon's code.
func (k phoneKeys) decrypt(body []byte) ([]byte, error) {
	if len(body) < 21 {
		return nil, fmt.Errorf("body of %d bytes is shorter than the header", len(body))
	}
	salt := body[:16]
	rs := binary.BigEndian.Uint32(body[16:20])
	idlen := int(body[20])
	if idlen != 65 || len(body) < 21+idlen {
		return nil, fmt.Errorf("keyid length %d, want 65", idlen)
	}
	asPublic := body[21 : 21+idlen]
	ct := body[21+idlen:]
	if uint32(len(ct)) > rs {
		return nil, fmt.Errorf("the record of %d bytes is longer than rs %d", len(ct), rs)
	}
	as, err := ecdh.P256().NewPublicKey(asPublic)
	if err != nil {
		return nil, fmt.Errorf("keyid is not a P-256 point: %w", err)
	}
	ecdhSecret, err := k.priv.ECDH(as)
	if err != nil {
		return nil, err
	}
	uaPublic := k.priv.PublicKey().Bytes()
	prkKey := hmacSHA256(k.auth, ecdhSecret)
	keyInfo := append(append(append([]byte("WebPush: info\x00"), uaPublic...), asPublic...), 1)
	ikm := hmacSHA256(prkKey, keyInfo)
	prk := hmacSHA256(salt, ikm)
	cek := hmacSHA256(prk, []byte("Content-Encoding: aes128gcm\x00\x01"))[:16]
	nonce := hmacSHA256(prk, []byte("Content-Encoding: nonce\x00\x01"))[:12]
	block, err := aes.NewCipher(cek)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	plain, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return nil, fmt.Errorf("AES-GCM open: %w", err)
	}
	// The last record ends in 0x02, then any zero padding was before it.
	end := len(plain) - 1
	for end >= 0 && plain[end] == 0 {
		end--
	}
	if end < 0 || plain[end] != 2 {
		return nil, fmt.Errorf("no last-record delimiter")
	}
	return plain[:end], nil
}

// checkVAPID verifies the Authorization header against the key register-push
// returned, and returns the JWT's claims.
func checkVAPID(t *testing.T, header, wantKey string) map[string]any {
	t.Helper()
	rest, ok := strings.CutPrefix(header, "vapid ")
	if !ok {
		t.Fatalf("Authorization %q is not vapid", header)
	}
	var tok, key string
	for part := range strings.SplitSeq(rest, ",") {
		part = strings.TrimSpace(part)
		if v, ok := strings.CutPrefix(part, "t="); ok {
			tok = v
		}
		if v, ok := strings.CutPrefix(part, "k="); ok {
			key = v
		}
	}
	if key != wantKey {
		t.Fatalf("vapid k=%q, want the key register-push returned %q", key, wantKey)
	}
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		t.Fatalf("vapid t is not a JWT: %q", tok)
	}
	hdr, _ := base64.RawURLEncoding.DecodeString(parts[0])
	var h map[string]any
	if json.Unmarshal(hdr, &h) != nil || h["alg"] != "ES256" || h["typ"] != "JWT" {
		t.Fatalf("JWT header %s, want ES256 JWT", hdr)
	}
	pub, _ := base64.RawURLEncoding.DecodeString(key)
	if len(pub) != 65 || pub[0] != 4 {
		t.Fatalf("vapid key is not an uncompressed point")
	}
	pk := &ecdsa.PublicKey{Curve: elliptic.P256(), X: new(big.Int).SetBytes(pub[1:33]), Y: new(big.Int).SetBytes(pub[33:])}
	sig, _ := base64.RawURLEncoding.DecodeString(parts[2])
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if len(sig) != 64 || !ecdsa.Verify(pk, digest[:], new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])) {
		t.Fatalf("the VAPID JWT signature does not verify")
	}
	raw, _ := base64.RawURLEncoding.DecodeString(parts[1])
	var claims map[string]any
	if err := json.Unmarshal(raw, &claims); err != nil {
		t.Fatalf("JWT claims: %v", err)
	}
	return claims
}

// waitPush waits for a push to path that decrypts to a payload pred accepts.
func waitPush(t *testing.T, s *stubPush, k phoneKeys, path string, pred func(map[string]any) bool) stubReq {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	seen := 0
	for time.Now().Before(deadline) {
		reqs := s.requests(path)
		for ; seen < len(reqs); seen++ {
			r := reqs[seen]
			if r.Status >= 300 {
				continue
			}
			plain, err := k.decrypt(r.Body)
			if err != nil {
				t.Fatalf("decrypt the push to %s: %v", path, err)
			}
			if err := json.Unmarshal(plain, &r.Payload); err != nil {
				t.Fatalf("the push to %s is not JSON: %v\n%s", path, err, plain)
			}
			if len(plain) > 3<<10 {
				t.Fatalf("the payload is %d bytes, over 3 KiB", len(plain))
			}
			if padded := len(r.Body) - 86 - 17; !pushPadBuckets[padded] {
				t.Fatalf("ASSERTION: the push to %s pads a %d-byte payload to %d bytes, not to a bucket", path, len(plain), padded)
			}
			if pred(r.Payload) {
				return r
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("no push to %s matched; got %d requests", path, len(s.requests(path)))
	return stubReq{}
}

// subJSON is a subscription as PushSubscription.toJSON() gives it, the
// shape tuios notify push register reads from a file or stdin.
func (k phoneKeys) subJSON(endpoint string) string {
	data, _ := json.Marshal(map[string]any{
		"endpoint": endpoint,
		"keys":     map[string]string{"p256dh": k.p256dh(), "auth": k.authB64()},
	})
	return string(data)
}

// tuiosCLIStdin is tuiosCLI with stdin.
func tuiosCLIStdin(t *testing.T, base, stdin string, args ...string) (string, error) {
	t.Helper()
	pinPreV080Looks(t, base)
	cmd := exec.Command(tuiosBin, args...)
	cmd.Env = append(os.Environ(), "SHELL=/bin/sh")
	for _, key := range xdgKeys {
		cmd.Env = append(cmd.Env, key+"="+xdgDir(base, key))
	}
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// pushPadBuckets are the sizes the daemon pads a payload up to. A body is
// 86 bytes of header, then the padded payload, the delimiter and the 16-byte
// tag.
var pushPadBuckets = map[int]bool{256: true, 512: true, 1024: true, 2048: true, 3072: true}
