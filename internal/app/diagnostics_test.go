package app

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"myanime/internal/logging"
	"myanime/internal/provider"
)

func newDiagnosticsClient(t *testing.T) *contractClient {
	t.Helper()
	return newContractClient(t)
}

func getDiagnostics(t *testing.T, c *contractClient) string {
	t.Helper()
	env := c.request("GET", "/diagnostics", "", 200)
	return string(env.Data)
}

func TestDiagnosticsReportIsSafeAndStructured(t *testing.T) {
	c := newDiagnosticsClient(t)
	body := getDiagnostics(t, c)
	for _, want := range []string{`"version"`, `"os"`, `"mode"`, `"providers"`, `"recentErrors"`} {
		if !strings.Contains(body, want) {
			t.Errorf("report missing %s: %s", want, body)
		}
	}
	// Provider identifiers only — no upstream URLs of any kind.
	if strings.Contains(body, "https://") || strings.Contains(body, "http://") {
		t.Errorf("report contains a URL: %s", body)
	}
}

func TestDiagnosticsNeverLeaksPoisonedValues(t *testing.T) {
	c := newDiagnosticsClient(t)
	// Poisoned error message with tokens, a signed media path, a cookie
	// header and a home path, exactly the classes logging redacts.
	c.app.logs.Error(
		"provider_failed https://api.example/v1/failed?token=sekrit-token-123",
		"cookie", "session=poison-cookie",
		"authorization", "Bearer poison-bearer",
		"url", "https://cdn.example/pages/segment/signed-token-987/001.jpg?token=page-token-456",
		"path", "/Users/leaky/home/manga.db",
	)
	body := getDiagnostics(t, c)
	for _, poison := range []string{
		"sekrit-token-123", "poison-cookie", "poison-bearer",
		"signed-token-987", "page-token-456", "/Users/leaky", "manga.db",
	} {
		if strings.Contains(body, poison) {
			t.Errorf("report leaks %q: %s", poison, body)
		}
	}
	if !strings.Contains(body, "[REDACTED]") {
		t.Errorf("report should show redaction instead of the category's secret: %s", body)
	}
}

func TestDiagnosticsProviderFailureKeepsReportAlive(t *testing.T) {
	c := newDiagnosticsClient(t)
	// A provider failure flows through the same handler every route uses.
	rec := httptest.NewRecorder()
	c.app.handleErr(rec, &provider.Error{Code: "timeout", Provider: "hianime.example"})
	if rec.Code != 504 {
		t.Fatalf("provider failure status %d", rec.Code)
	}
	body := getDiagnostics(t, c)
	if !strings.Contains(body, `"providers"`) {
		t.Errorf("report blanked after provider failure: %s", body)
	}
	if !strings.Contains(body, "request_failed") {
		t.Errorf("recent error category missing after failure: %s", body)
	}
}

func TestDiagnosticsCountsRecentErrorCategories(t *testing.T) {
	c := newDiagnosticsClient(t)
	c.app.logs.Error("request_failed")
	c.app.logs.Error("request_failed")
	c.app.logs.Warn("not_an_error")
	body := getDiagnostics(t, c)
	if !strings.Contains(body, `"category":"request_failed","count":2`) {
		t.Errorf("expected counted request_failed category: %s", body)
	}
}

func TestOpenLogFolderVariants(t *testing.T) {
	c := newDiagnosticsClient(t)
	dir := c.app.logs.Dir()
	if dir == "" {
		t.Fatal("log dir not configured")
	}

	// Success path: opener invoked with the log directory.
	var opened string
	orig := openLogFolder
	openLogFolder = func(d string) error {
		opened = d
		return nil
	}
	t.Cleanup(func() { openLogFolder = orig })
	c.request("POST", "/diagnostics/open-log-folder", "", 200)
	if opened != dir {
		t.Errorf("opened %q want %q", opened, dir)
	}

	// Unsupported host degrades honestly with a distinct code.
	openLogFolder = func(string) error { return &logFolderError{unsupported: true} }
	env := c.request("POST", "/diagnostics/open-log-folder", "", 501)
	if env.Error == nil || env.Error.Code != "NOT_SUPPORTED" {
		t.Errorf("unsupported host should report NOT_SUPPORTED: %+v", env.Error)
	}

	// Opener failure is honest, never a fake success.
	openLogFolder = func(string) error { return &logFolderError{} }
	env = c.request("POST", "/diagnostics/open-log-folder", "", 502)
	if env.Error == nil || env.Error.Code != "LOG_FOLDER_FAILED" {
		t.Errorf("opener failure should report LOG_FOLDER_FAILED: %+v", env.Error)
	}
}

func TestOpenLogFolderWithoutLogDirectory(t *testing.T) {
	// An app whose logging directory could not be created has no folder to
	// open; the route must say so instead of inventing a path.
	c := newDiagnosticsClient(t)
	c.app.logs.Close()
	c.app.logs = logging.New(logging.Options{}) // no dir, no file logging
	env := c.request("POST", "/diagnostics/open-log-folder", "", 404)
	if env.Error == nil || env.Error.Code != "NOT_FOUND" {
		t.Errorf("missing log folder should report NOT_FOUND: %+v", env.Error)
	}
	// Diagnostics still works without a log folder.
	env = c.request("GET", "/diagnostics", "", 200)
	if !strings.Contains(string(env.Data), `"recentErrors"`) {
		t.Errorf("diagnostics should stay available: %s", string(env.Data))
	}
}

func TestOpenLogFolderMissingOnDisk(t *testing.T) {
	c := newDiagnosticsClient(t)
	dir := t.TempDir()
	c.app.logs = logging.New(logging.Options{Dir: filepath.Join(dir, "gone")})
	if err := os.RemoveAll(filepath.Join(dir, "gone")); err != nil {
		t.Fatal(err)
	}
	env := c.request("POST", "/diagnostics/open-log-folder", "", 404)
	if env.Error == nil || env.Error.Code != "NOT_FOUND" {
		t.Errorf("missing on-disk folder should report NOT_FOUND: %+v", env.Error)
	}
}
