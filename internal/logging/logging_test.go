package logging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readAllLogs(t *testing.T, dir string) (string, int64) {
	t.Helper()
	var sb strings.Builder
	var total int64
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read log dir: %v", err)
	}
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), "myanime.log") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		sb.Write(b)
		total += int64(len(b))
	}
	return sb.String(), total
}

func TestRotationCapsDiskUsage(t *testing.T) {
	dir := t.TempDir()
	l := New(Options{Dir: dir, MaxSize: 200, MaxFiles: 2, Buffer: 4096})
	payload := strings.Repeat("x", 50)
	for i := range 200 {
		l.Info("filler", "i", i, "data", payload)
	}
	l.Close()
	_, total := readAllLogs(t, dir)
	// MaxFiles files of at most MaxSize bytes each.
	if total > 2*200 {
		t.Fatalf("disk usage %d exceeds cap %d", total, 2*200)
	}
	entries, _ := os.ReadDir(dir)
	var logFiles int
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "myanime.log") {
			logFiles++
		}
	}
	if logFiles > 2 {
		t.Fatalf("retained %d log files, want <= %d", logFiles, 2)
	}
}

func TestRedactionAtWriteTime(t *testing.T) {
	dir := t.TempDir()
	l := New(Options{Dir: dir, MaxSize: 1 << 20, MaxFiles: 1, Buffer: 256})
	l.Error("request_failed",
		"authorization", "Authorization: Bearer super-secret-jwt",
		"cookie", "Cookie: session=abc123; other=v",
		"url", "https://cdn.example/stream.m3u8?token=deadbeef&expires=1735689600&sig=hmacvalue",
		"segment", "http://127.0.0.1:8767/segment/QURNSU5fU0VDUkVU",
		"path", "/Users/bob/Library/Application Support/myanime.db",
		"winpath", `C:\Users\bob\secrets.txt`,
		"plain", "kept visible",
	)
	l.Close()
	out, _ := readAllLogs(t, dir)
	for _, secret := range []string{
		"super-secret-jwt", "session=abc123", "deadbeef", "hmacvalue",
		"QURNSU5fU0VDUkVU", "/Users/bob", `C:\Users\bob`, "secrets.txt", "Library/Application",
	} {
		if strings.Contains(out, secret) {
			t.Errorf("log leaks %q:\n%s", secret, out)
		}
	}
	if !strings.Contains(out, "[REDACTED]") {
		t.Error("expected redaction markers in output")
	}
	if !strings.Contains(out, "kept visible") {
		t.Error("redaction removed benign content")
	}
}

func TestClosedLoggerDoesNotPanic(t *testing.T) {
	dir := t.TempDir()
	l := New(Options{Dir: dir, Buffer: 4})
	l.Info("before close")
	l.Close()
	l.Info("after close") // must be a no-op, not a panic
	l.Error("after close", "k", "v")
	l.Close() // idempotent
	var nilLogger *Logger
	nilLogger.Info("nil logger") // nil receiver is a safe no-op
}

func TestFlushOnClose(t *testing.T) {
	dir := t.TempDir()
	l := New(Options{Dir: dir, MaxSize: 1 << 20, MaxFiles: 1, Buffer: 1024})
	l.Warn("buffered_event", "code", 42)
	l.Close()
	out, _ := readAllLogs(t, dir)
	if !strings.Contains(out, "buffered_event") {
		t.Error("Close did not flush buffered events")
	}
	if !strings.Contains(out, `"code":42`) {
		t.Error("structured kv missing from flushed event")
	}
}

func TestUnwritableDirIsNoOp(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	l := New(Options{Dir: filepath.Join(blocker, "logs")})
	if l.Enabled() {
		t.Fatal("logger should be disabled for unwritable dir")
	}
	l.Error("never written", "detail", "dropped")
	l.Close() // must not hang or panic
}

func TestRedact(t *testing.T) {
	cases := map[string]string{
		"Authorization: Bearer abc.def.ghi":                "[REDACTED]",
		"header cookie: sid=zzz":                           "header [REDACTED]",
		"https://x.io/v.m3u8?expires=999&token=TOKEN&ok=1": "https://x.io/v.m3u8?expires=[REDACTED]&token=[REDACTED]&ok=1",
		"/home/alice/secret.txt":                           "[REDACTED]",
		"/var/log/syslog":                                  "/var/log/syslog",
	}
	for in, want := range cases {
		if got := Redact(in); got != want {
			t.Errorf("Redact(%q) = %q, want %q", in, got, want)
		}
	}
}
