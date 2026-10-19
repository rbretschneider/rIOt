package server

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHARDEN002_C5_NoNilHandlerOrDefaultServeMux pins the invariant that makes
// the net/http/pprof import safe. That import registers /debug/pprof/* on the
// default mux in init(); those handlers are never reachable as long as nothing
// serves the default mux. An http.Server with a nil Handler, or a call to the
// package-level serve helpers with a nil handler, would serve it
// (HARDEN-002 C5(b)).
func TestHARDEN002_C5_NoNilHandlerOrDefaultServeMux(t *testing.T) {
	forbidden := []string{
		"http.DefaultServeMux",
		"http.ListenAndServe(",
		"http.ListenAndServeTLS(",
		"http.Serve(",
		"http.ServeTLS(",
	}

	var scanned int
	for _, root := range []string{"../../cmd", "../../internal"} {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			raw, readErr := os.ReadFile(path)
			require.NoError(t, readErr, path)
			scanned++
			src := stripLineComments(string(raw))

			for _, f := range forbidden {
				assert.NotContains(t, src, f, "%s must not use %s: it serves the default mux that net/http/pprof registers on", path, f)
			}

			const lit = "http.Server{"
			for from := 0; ; {
				i := strings.Index(src[from:], lit)
				if i < 0 {
					break
				}
				start := from + i + len(lit)
				body := literalBody(src[start:])
				assert.Contains(t, body, "Handler:", "%s: every http.Server literal must set Handler (a nil Handler serves the default mux)", path)
				from = start
			}
			return nil
		})
		require.NoError(t, err, "walking %s", root)
	}
	require.Greater(t, scanned, 20, "the scan must actually cover the source tree, scanned %d files", scanned)
}

// stripLineComments removes // comments so documentation that mentions the
// forbidden identifiers does not trip the scan.
func stripLineComments(src string) string {
	lines := strings.Split(src, "\n")
	for i, l := range lines {
		if idx := strings.Index(l, "//"); idx >= 0 && !strings.Contains(l[:idx], "\"") {
			lines[i] = l[:idx]
		}
	}
	return strings.Join(lines, "\n")
}

// literalBody returns the text up to the brace that closes a composite
// literal whose opening brace has just been consumed.
func literalBody(s string) string {
	depth := 1
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[:i]
			}
		}
	}
	return s
}

func TestHARDEN002_C5_ScanDetectsViolations(t *testing.T) {
	// Guards the scanner itself: a literal without Handler is found.
	body := literalBody("Addr: \":1\", ReadTimeout: 1}")
	assert.NotContains(t, body, "Handler:", "scanner must report a literal lacking Handler")
	body = literalBody("Addr: \":1\", Handler: x, TLSConfig: &tls.Config{}}")
	assert.Contains(t, body, "Handler:")
}
