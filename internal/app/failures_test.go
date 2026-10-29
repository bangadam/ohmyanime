package app

import (
	"context"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"myanime/internal/logging"
	"myanime/internal/media"
	"myanime/internal/provider"
)

// Timeout and title absence are distinct, stable categories: a timeout is
// retryable against the same source, title absence never is and must point
// the viewer at another provider.
func TestPlaybackFailureCategories(t *testing.T) {
	table := []struct {
		name          string
		err           error
		wantStatus    int
		wantCode      string
		wantStage     string
		wantRetryable bool
	}{
		{"provider timeout", &provider.Error{Code: "timeout", Provider: "hianime"}, 504, "PROVIDER_TIMEOUT", stageResolve, true},
		{"deadline exceeded", context.DeadlineExceeded, 504, "PROVIDER_TIMEOUT", stageResolve, true},
		{"title absent", &provider.Error{Code: "no_source", Provider: "hianime"}, 404, "TITLE_ABSENT", stageResolve, false},
		{"invalid episode id", &provider.Error{Code: "invalid_mapping", Provider: "catalog"}, 400, "INVALID_INPUT", stageResolve, false},
		{"rate limited", &provider.Error{Code: "rate_limited", Provider: "hianime"}, 429, "PROVIDER_RATE_LIMITED", stageResolve, true},
		{"unavailable", &provider.Error{Code: "unavailable", Provider: "hianime"}, 502, "PROVIDER_UNAVAILABLE", stageResolve, true},
		{"malformed source", &provider.Error{Code: "malformed_source", Provider: "hianime"}, 502, "MALFORMED_SOURCE", stageResolve, false},
		{"session expired", media.ErrExpired, 410, "SESSION_EXPIRED", stageSession, false},
		{"media limit", media.ErrLimit, 429, "RATE_LIMITED", stageSession, true},
	}
	for _, tc := range table {
		f := classifyPlayback(tc.err)
		if f.Status != tc.wantStatus || f.Code != tc.wantCode || f.Stage != tc.wantStage || f.Retryable != tc.wantRetryable {
			t.Errorf("%s: got status=%d code=%s stage=%s retryable=%v, want status=%d code=%s stage=%s retryable=%v",
				tc.name, f.Status, f.Code, f.Stage, f.Retryable, tc.wantStatus, tc.wantCode, tc.wantStage, tc.wantRetryable)
		}
	}
	timeout := classifyPlayback(&provider.Error{Code: "timeout"})
	absent := classifyPlayback(&provider.Error{Code: "no_source"})
	if timeout.Code == absent.Code || timeout.Status == absent.Status || timeout.Retryable == absent.Retryable {
		t.Fatalf("timeout and title absence collapsed into one category: %+v vs %+v", timeout, absent)
	}
}

// A poisoned upstream URL in the provider or cause field must never reach the
// response or the log: the payload carries only redacted values and the log
// redacts at write time.
func TestWritePlaybackFailureRedactsSecrets(t *testing.T) {
	dir := t.TempDir()
	logDir := filepath.Join(dir, "logs")
	a := &App{logs: logging.New(logging.Options{Dir: logDir, MaxSize: 1 << 20, MaxFiles: 1})}
	poison := "https://cdn.example/master.m3u8?token=hunter2&sig=deadbeef"
	w := httptest.NewRecorder()
	a.writePlaybackFailure(w, &provider.Error{Code: "unavailable", Provider: poison, Cause: errors.New("GET " + poison + " failed")})
	a.logs.Close()
	body := w.Body.String()
	for _, secret := range []string{"hunter2", "deadbeef"} {
		if strings.Contains(body, secret) {
			t.Errorf("response leaks %q: %s", secret, body)
		}
	}
	if !strings.Contains(body, "[REDACTED]") {
		t.Errorf("expected redacted provider value: %s", body)
	}
	if !strings.Contains(body, `"stage":"resolve"`) || !strings.Contains(body, `"requestId"`) {
		t.Errorf("expected stage and request ID in payload: %s", body)
	}
	b, err := os.ReadFile(filepath.Join(logDir, "myanime.log"))
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	out := string(b)
	for _, secret := range []string{"hunter2", "deadbeef"} {
		if strings.Contains(out, secret) {
			t.Errorf("log leaks %q: %s", secret, out)
		}
	}
	if !strings.Contains(out, "[REDACTED]") {
		t.Errorf("expected redaction in log: %s", out)
	}
}

// The generic handleErr mapping exposes the same title-absence category so
// non-playback callers cannot regress it into a generic provider failure.
func TestHandleErrTitleAbsent(t *testing.T) {
	a := &App{logs: logging.New(logging.Options{Dir: t.TempDir(), MaxSize: 1 << 20, MaxFiles: 1})}
	defer a.logs.Close()
	w := httptest.NewRecorder()
	a.handleErr(w, &provider.Error{Code: "no_source", Provider: "hianime"})
	if w.Code != 404 || !strings.Contains(w.Body.String(), "TITLE_ABSENT") {
		t.Fatalf("got %d %s", w.Code, w.Body.String())
	}
}
