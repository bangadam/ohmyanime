// Package logging provides bounded, redacted local structured logs for
// packaged desktop failures. Events are JSON lines written to size-capped
// rotating files with capped retention. Every message and value passes
// through a central redaction policy at write time. An unwritable log
// directory degrades to a no-op logger that never blocks or crashes
// playback; Close flushes buffered events.
package logging

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"
)

const (
	// DefaultMaxSize caps a single log file at 5 MiB.
	DefaultMaxSize int64 = 5 << 20
	// DefaultMaxFiles caps total retained log files, active file included.
	DefaultMaxFiles = 3
	// DefaultBuffer bounds queued events before dropping.
	DefaultBuffer = 256
	// recentMax bounds the in-memory ring of recent error categories.
	recentMax = 64
)

// Options configures a Logger. Zero values adopt safe defaults.
type Options struct {
	Dir      string // directory for log files; unwritable disables logging
	MaxSize  int64  // max bytes per file before rotation
	MaxFiles int    // max retained files including the active one
	Buffer   int    // queued events before dropping
}

// Logger writes structured JSON events to rotating files. A nil *Logger or
// a Logger opened against an unwritable directory is a safe no-op.
type Logger struct {
	opts    Options
	mu      sync.Mutex
	closed  bool
	events  chan event
	done    chan struct{}
	w       *rotator
	failed  bool
	logPath string
	// recent is a bounded ring of recent error-level event categories,
	// recorded even when file logging is disabled so diagnostics can still
	// report sanitized failure summaries.
	recent []string
}

type event struct {
	Level string         `json:"level"`
	Msg   string         `json:"msg"`
	At    time.Time      `json:"time"`
	KV    map[string]any `json:"kv,omitempty"`
}

// New opens a rotating logger in Dir. If the directory cannot be created or
// the log file cannot be opened, a disabled no-op Logger is returned.
func New(o Options) *Logger {
	if o.MaxSize <= 0 {
		o.MaxSize = DefaultMaxSize
	}
	if o.MaxFiles <= 0 {
		o.MaxFiles = DefaultMaxFiles
	}
	if o.Buffer <= 0 {
		o.Buffer = DefaultBuffer
	}
	l := &Logger{opts: o, events: make(chan event, o.Buffer), done: make(chan struct{})}
	if o.Dir == "" {
		return l
	}
	r, err := openRotator(o)
	if err != nil {
		return l
	}
	l.w = r
	l.logPath = r.path
	go l.loop()
	return l
}

// Enabled reports whether events are written to disk.
func (l *Logger) Enabled() bool { return l != nil && l.w != nil }

// Log queues a structured event. It never blocks: when the buffer is full
// or the logger is closed, the event is dropped. Key/value pairs alternate
// ("k1", v1, "k2", v2); a trailing dangling key is dropped.
func (l *Logger) Log(level, msg string, kv ...any) {
	if l == nil {
		return
	}
	if level == "error" {
		// The ring is independent of file writing so diagnostics keep
		// working when the log directory is unwritable.
		l.mu.Lock()
		if !l.closed {
			l.recent = append(l.recent, Redact(msg))
			if len(l.recent) > recentMax {
				l.recent = l.recent[len(l.recent)-recentMax:]
			}
		}
		l.mu.Unlock()
	}
	if l.w == nil {
		return
	}
	e := event{Level: level, Msg: msg, At: time.Now().UTC()}
	if len(kv) >= 2 {
		e.KV = make(map[string]any, len(kv)/2)
		for i := 0; i+1 < len(kv); i += 2 {
			k, ok := kv[i].(string)
			if !ok || k == "" {
				continue
			}
			e.KV[k] = stringify(kv[i+1])
		}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return
	}
	select {
	case l.events <- e:
	default: // buffer full: drop rather than block playback
	}
}

// ErrorCategory summarizes recent error-level events for diagnostics. Only
// the redacted event category is retained — never values, URLs or paths.
type ErrorCategory struct {
	Category string `json:"category"`
	Count    int    `json:"count"`
}

// RecentErrors returns error-level event categories from the in-memory
// ring, most recent first, at most limit entries (limit <= 0 means all).
// The ring is bounded and redacted; it survives disabled file logging and
// provider failures.
func (l *Logger) RecentErrors(limit int) []ErrorCategory {
	if l == nil {
		return []ErrorCategory{}
	}
	l.mu.Lock()
	ring := make([]string, len(l.recent))
	copy(ring, l.recent)
	l.mu.Unlock()
	counts := map[string]int{}
	order := []string{}
	for i := len(ring) - 1; i >= 0; i-- { // newest entries first
		if _, seen := counts[ring[i]]; !seen {
			order = append(order, ring[i])
		}
		counts[ring[i]]++
	}
	if limit > 0 && len(order) > limit {
		order = order[:limit]
	}
	out := make([]ErrorCategory, 0, len(order))
	for _, category := range order {
		out = append(out, ErrorCategory{Category: category, Count: counts[category]})
	}
	return out
}

// Dir returns the configured log directory ("" when unset).
func (l *Logger) Dir() string {
	if l == nil {
		return ""
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.opts.Dir
}

func (l *Logger) Info(msg string, kv ...any)  { l.Log("info", msg, kv...) }
func (l *Logger) Warn(msg string, kv ...any)  { l.Log("warn", msg, kv...) }
func (l *Logger) Error(msg string, kv ...any) { l.Log("error", msg, kv...) }

// Close stops accepting events, flushes everything queued, and waits for
// disk writes to finish. Close is idempotent and safe on disabled loggers.
func (l *Logger) Close() {
	if l == nil {
		return
	}
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		<-l.done
		return
	}
	l.closed = true
	close(l.events)
	l.mu.Unlock()
	if l.w != nil {
		<-l.done
		l.w.close()
	} else {
		close(l.done)
	}
}

func (l *Logger) loop() {
	for e := range l.events {
		if !l.write(e) {
			l.w = nil // underlying file broken: degrade to no-op
			continue
		}
	}
	close(l.done)
}

func (l *Logger) write(e event) bool {
	b, err := json.Marshal(e)
	if err != nil {
		return true
	}
	return l.w.write(append(redactBytes(b), '\n'))
}

func stringify(v any) any {
	switch t := v.(type) {
	case string:
		return t
	case error:
		return t.Error()
	default:
		return t
	}
}

// rotator appends to the active file and rotates when it exceeds MaxSize.
type rotator struct {
	mu       sync.Mutex
	dir      string
	path     string
	maxSize  int64
	maxFiles int
	f        *os.File
	size     int64
}

func openRotator(o Options) (*rotator, error) {
	if err := os.MkdirAll(o.Dir, 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(o.Dir, "myanime.log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	return &rotator{dir: o.Dir, path: path, maxSize: o.MaxSize, maxFiles: o.MaxFiles, f: f, size: st.Size()}, nil
}

func (r *rotator) write(b []byte) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.f == nil {
		return false
	}
	if r.size > 0 && r.size+int64(len(b)) > r.maxSize && !r.rotate() {
		return false
	}
	n, err := r.f.Write(b)
	r.size += int64(n)
	return err == nil
}

// rotate shifts numbered files up, dropping the oldest, and reopens a fresh
// active file. It returns false when the new file cannot be opened.
func (r *rotator) rotate() bool {
	r.f.Close()
	r.f = nil
	oldest := numberedPath(r.path, r.maxFiles-1)
	_ = os.Remove(oldest)
	for i := r.maxFiles - 2; i >= 1; i-- {
		_ = os.Rename(numberedPath(r.path, i), numberedPath(r.path, i+1))
	}
	_ = os.Rename(r.path, numberedPath(r.path, 1))
	f, err := os.OpenFile(r.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return false
	}
	r.f = f
	r.size = 0
	return true
}

func (r *rotator) close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.f != nil {
		_ = r.f.Close()
		r.f = nil
	}
}

func numberedPath(path string, i int) string {
	return path + "." + itoa(i)
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var d []byte
	for ; i > 0; i /= 10 {
		d = append([]byte{byte('0' + i%10)}, d...)
	}
	return string(d)
}

// Redact applies the central redaction policy to a log line. It removes:
//   - authorization and cookie header values
//   - signed URL / query token parameters and /segment/<token> media paths
//   - local filesystem home paths
//
// It is applied to the serialized event at write time so no call site can
// bypass it.
var redactRegex = regexp.MustCompile(`(?i)(` +
	`\b(?:authorization|proxy-authorization|cookie)\s*:\s*[^\n",}]+` + // header values
	`|[?&](?:access_token|id_token|refresh_token|token|sig|signature|sign|hmac|key|expires?d?|expiry|policy|x-amz-signature|x-amz-credential|x-amz-security-token|x-amz-token|x-goog-signature|bearer)=[^&\s"']*` + // signed URL / query tokens
	`|/segment/[A-Za-z0-9_\-=+/]+` + // signed media segment paths
	`|/(?:users|home)/[^/\s"',]+(?:/[^\s"',}]*)*` + // POSIX home paths
	`|[A-Za-z]:[\\/]+(?:users|home)[\\/][^\s"',}]*` + // Windows home paths
	`)`)

const redacted = "[REDACTED]"

var queryPrefix = regexp.MustCompile(`^[?&][^=&\s"']*=`)

// Redact rewrites sensitive substrings in s to [REDACTED], preserving the
// URL delimiter and parameter name where useful for debugging.
func Redact(s string) string {
	return string(redactBytes([]byte(s)))
}

func redactBytes(b []byte) []byte {
	return redactRegex.ReplaceAllFunc(b, func(m []byte) []byte {
		if p := queryPrefix.Find(m); len(p) > 0 {
			return append(append([]byte{}, p...), redacted...)
		}
		return []byte(redacted)
	})
}
