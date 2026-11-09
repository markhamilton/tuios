package main

import (
	"strings"
	"testing"
)

// A look comes only from a local file. A URL is refused before anything is
// read, so tuios never downloads a look.
func TestReadLookSourceRefusesURLs(t *testing.T) {
	for _, in := range []string{"https://example.com/a.toml", "http://example.com/a.toml", "file:///etc/passwd"} {
		_, _, err := readLookSource(in)
		if err == nil || !strings.Contains(err.Error(), "local file") {
			t.Errorf("%q: error %v, want one about a local file", in, err)
		}
	}
	if _, _, err := readLookSource(""); err == nil {
		t.Error("an empty source is read, want an error")
	}
}
