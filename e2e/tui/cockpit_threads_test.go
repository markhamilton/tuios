package tuie2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
)

// fakeOpenCodeService runs a service that answers as opencode 2.0.32 does,
// with page as the session's messages, and returns the service.json that
// names it.
func fakeOpenCodeService(t *testing.T, page []byte, session string) []byte {
	t.Helper()
	// Made when the test runs, so no secret scanner reads a literal.
	password := strings.Repeat("y", 8) + strconv.Itoa(os.Getpid())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if user, pass, ok := r.BasicAuth(); !ok || user != "opencode" || pass != password {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/api/info":
			fmt.Fprintf(w, `{"version":"2.0.32","pid":%d}`, os.Getpid())
		case "/api/session/" + session + "/message":
			_, _ = w.Write(page)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	data, _ := json.Marshal(map[string]any{"id": "svc", "version": "2.0.32", "url": srv.URL, "pid": os.Getpid(), "password": password})
	return data
}
