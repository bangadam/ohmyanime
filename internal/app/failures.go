package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"myanime/internal/logging"
	"myanime/internal/media"
	"myanime/internal/provider"
)

// Playback failure stages. The stage names the pipeline step that failed so
// the interface can retry that step alone instead of rebuilding the session:
// "resolve" retries the resolve request, "manifest" reloads the master
// playlist, "segment" resumes segment loading, and "session" requires a new
// session.
const (
	stageResolve  = "resolve"
	stageManifest = "manifest"
	stageSegment  = "segment"
	stageSession  = "session"
)

// playbackFailure is the stable, safe wire shape for a playback failure.
// Codes and stages are contract: they never embed upstream response bodies,
// credentials, or signed URLs.
type playbackFailure struct {
	Status     int
	Code       string
	Message    string
	Stage      string
	Retryable  bool
	RetryAfter time.Duration
	Provider   string
}

// classifyPlayback maps a playback-path error onto the stable failure
// categories. A provider timeout and title absence are deliberately distinct:
// the first is retryable against the same source, the second is not and
// points the viewer at another provider.
func classifyPlayback(err error) playbackFailure {
	var pe *provider.Error
	if errors.As(err, &pe) {
		f := playbackFailure{Status: 502, Code: "PROVIDER_UNAVAILABLE", Message: "Source unavailable", Stage: stageResolve, Retryable: true, Provider: pe.Provider}
		switch pe.Code {
		case "timeout":
			f.Status, f.Code, f.Message = 504, "PROVIDER_TIMEOUT", "Source did not respond in time"
		case "rate_limited":
			f.Status, f.Code, f.Message, f.RetryAfter = 429, "PROVIDER_RATE_LIMITED", "Source is rate limiting requests", pe.RetryAfter
		case "auth":
			f.Code, f.Message, f.Retryable = "PROVIDER_AUTH", "Source authentication failed", false
		case "forbidden":
			f.Code, f.Message, f.Retryable = "PROVIDER_FORBIDDEN", "Source access denied", false
		case "malformed_source":
			f.Code, f.Message, f.Retryable = "MALFORMED_SOURCE", "Source returned an invalid response", false
		case "no_source":
			f.Status, f.Code, f.Message, f.Retryable = 404, "TITLE_ABSENT", "This episode is not available from the selected source", false
		case "invalid_mapping", "invalid_provider", "invalid_mode":
			f.Status, f.Code, f.Message, f.Retryable = 400, "INVALID_INPUT", "Invalid source mapping", false
		case "unsupported":
			f.Status, f.Code, f.Message, f.Retryable = 422, "UNSUPPORTED", "Unsupported option", false
		}
		return f
	}
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return playbackFailure{Status: 504, Code: "PROVIDER_TIMEOUT", Message: "Source did not respond in time", Stage: stageResolve, Retryable: true}
	case errors.Is(err, context.Canceled):
		return playbackFailure{Status: 408, Code: "REQUEST_CANCELED", Message: "Request canceled"}
	case errors.Is(err, media.ErrExpired):
		return playbackFailure{Status: 410, Code: "SESSION_EXPIRED", Message: "Playback session expired", Stage: stageSession}
	case errors.Is(err, media.ErrInvalid):
		return playbackFailure{Status: 400, Code: "INVALID_MEDIA", Message: "Invalid media", Stage: stageManifest}
	case errors.Is(err, media.ErrLimit):
		return playbackFailure{Status: 429, Code: "RATE_LIMITED", Message: "Too many active media sessions", Stage: stageSession, Retryable: true}
	}
	return playbackFailure{Status: 502, Code: "PROVIDER_UNAVAILABLE", Message: "Source or storage unreachable", Stage: stageResolve, Retryable: true}
}

// writePlaybackFailure renders the stable failure payload for the playback
// endpoints: the failing stage, a request ID, and only sanitized values.
// Every dynamic value passes through the central redaction policy from
// internal/logging, so an upstream URL that reaches a provider or cause field
// cannot leak credentials or signatures into a response or the local log.
func (a *App) writePlaybackFailure(w http.ResponseWriter, err error) {
	f := classifyPlayback(err)
	if f.RetryAfter > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(int((f.RetryAfter+time.Second-1)/time.Second)))
	}
	body := map[string]any{"code": f.Code, "message": f.Message, "retryable": f.Retryable}
	if f.Stage != "" {
		body["stage"] = f.Stage
	}
	if f.Provider != "" {
		body["provider"] = logging.Redact(f.Provider)
	}
	a.logs.Error("playback_failed", "code", f.Code, "stage", f.Stage, "provider", f.Provider, "error", err)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(f.Status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": body, "requestId": randomToken()[:12]})
}
