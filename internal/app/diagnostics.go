package app

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"runtime"

	"myanime/internal/logging"
)

// Version is the application version reported in diagnostics. It is a var
// so release packaging can stamp it at link time.
var Version = "dev"

// diagnosticsReport is the safe, shareable troubleshooting summary. It
// deliberately contains only: app version, OS/arch, run mode, provider
// identifiers (never full URLs) and recent redacted error categories. It
// must never include cookies, authorization tokens, signed media URLs or
// personal watch/reading history.
type diagnosticsReport struct {
	Version      string                  `json:"version"`
	OS           string                  `json:"os"`
	Mode         string                  `json:"mode"`
	Providers    []string                `json:"providers"`
	RecentErrors []logging.ErrorCategory `json:"recentErrors"`
}

func (a *App) diagnostics(w http.ResponseWriter, r *http.Request) {
	ids := make([]string, 0, 4)
	for _, p := range a.catalog.Providers() {
		ids = append(ids, p.ID)
	}
	report := diagnosticsReport{
		Version:      Version,
		OS:           runtime.GOOS + " " + runtime.GOARCH,
		Mode:         a.cfg.Mode,
		Providers:    ids,
		RecentErrors: a.logs.RecentErrors(8),
	}
	b, err := json.Marshal(report)
	if err != nil {
		failure(w, 500, "DIAGNOSTICS_FAILED", "Diagnostics couldn't be assembled.")
		return
	}
	// Defense in depth: the same central redaction policy as file logging
	// is applied to the serialized report before it leaves the process.
	reply(w, 200, json.RawMessage(logging.Redact(string(b))))
}

// errLogFolderUnsupported reports that the current host has no known way
// to open the OS file manager. The route surfaces it honestly instead of
// pretending the folder opened.
var errLogFolderUnsupported = &logFolderError{}

type logFolderError struct{ unsupported bool }

func (e *logFolderError) Error() string {
	if e.unsupported {
		return "opening folders is not supported on this system"
	}
	return "log folder could not be opened"
}

// openLogFolder opens the host file manager at dir. It is a package var so
// tests can substitute a deterministic implementation; the real one is
// best-effort and never blocks on user interaction.
var openLogFolder = func(dir string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", dir).Start()
	case "windows":
		return exec.Command("explorer", dir).Start()
	case "linux", "freebsd", "openbsd", "netbsd":
		if _, err := exec.LookPath("xdg-open"); err != nil {
			return &logFolderError{unsupported: true}
		}
		return exec.Command("xdg-open", dir).Start()
	default:
		return &logFolderError{unsupported: true}
	}
}

func (a *App) openLogFolderHandler(w http.ResponseWriter, r *http.Request) {
	dir := a.logs.Dir()
	if dir == "" {
		failure(w, 404, "NOT_FOUND", "No log folder is configured on this system.")
		return
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		failure(w, 404, "NOT_FOUND", "No log folder exists yet. Logs appear after the first failure.")
		return
	}
	if err := openLogFolder(dir); err != nil {
		var lfe *logFolderError
		if errors.As(err, &lfe) && lfe.unsupported {
			failure(w, 501, "NOT_SUPPORTED", "Opening the log folder isn't supported on this system.")
			return
		}
		failure(w, 502, "LOG_FOLDER_FAILED", "The log folder couldn't be opened.")
		return
	}
	reply(w, 200, map[string]any{"opened": true})
}
