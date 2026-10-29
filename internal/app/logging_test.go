package app

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"myanime/internal/logging"
)

func TestHandleErrLogsRedacted(t *testing.T) {
	dir := t.TempDir()
	logDir := filepath.Join(dir, "logs")
	a := &App{logs: logging.New(logging.Options{Dir: logDir, MaxSize: 1 << 20, MaxFiles: 1})}
	w := &nilWriter{}
	a.handleErr(w, errors.New("fetch https://cdn.example/v.m3u8?token=hunter2&expires=99 failed"))
	a.logs.Close()
	b, err := os.ReadFile(filepath.Join(logDir, "myanime.log"))
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	out := string(b)
	if strings.Contains(out, "hunter2") {
		t.Errorf("log leaks token: %s", out)
	}
	if !strings.Contains(out, "[REDACTED]") {
		t.Errorf("expected redaction: %s", out)
	}
}

// nilWriter is a no-op ResponseWriter sufficient for handleErr.
type nilWriter struct{}

func (nilWriter) Header() http.Header       { return http.Header{} }
func (nilWriter) Write([]byte) (int, error) { return 0, nil }
func (nilWriter) WriteHeader(int)           {}
