package provider

import (
	"context"
	"errors"
	"io"
	"myanime/internal/domain"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// homeStub is a registered adapter whose homepage probe the test controls.
type homeStub struct {
	id   string
	home string
}

func (p *homeStub) Identity() Identity {
	return Identity{ID: p.id, Name: "Stub " + p.id, Kind: KindAnime}
}
func (p *homeStub) HomePage() string { return p.home }
func (p *homeStub) Modes() []string  { return []string{"sub"} }
func (p *homeStub) Map(context.Context, domain.Anime) (string, error) {
	return "stub-slug", nil
}
func (p *homeStub) Episodes(context.Context, string) ([]providerEpisode, error) {
	return nil, nil
}
func (p *homeStub) Resolve(context.Context, string, string, string) (domain.Source, error) {
	return domain.Source{}, nil
}

// switchableTransport serves queued outcomes in order; it also counts every
// request so tests can prove rendering issues no probes.
type switchableTransport struct {
	mu       sync.Mutex
	outcomes []func() (*http.Response, error)
	calls    int
}

func (t *switchableTransport) next() func() (*http.Response, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.calls++
	if len(t.outcomes) == 0 {
		return func() (*http.Response, error) { return nil, errors.New("connection refused") }
	}
	out := t.outcomes[0]
	t.outcomes = t.outcomes[1:]
	return out
}

func (t *switchableTransport) RoundTrip(r *http.Request) (*http.Response, error) { return t.next()() }
func (t *switchableTransport) total() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.calls
}

func okResponse(code int) *http.Response {
	return &http.Response{StatusCode: code, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("ok"))}
}

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

func healthService(t *testing.T) (*Service, *switchableTransport) {
	t.Helper()
	s := New()
	tr := &switchableTransport{}
	s.client.Transport = tr
	stub := &homeStub{id: "homestub", home: "https://example.test/"}
	if e := s.RegisterAnime(stub); e != nil {
		t.Fatal(e)
	}
	return s, tr
}

func TestHealthProbeReachableCarriesCheckedAt(t *testing.T) {
	s, tr := healthService(t)
	tr.outcomes = []func() (*http.Response, error){
		func() (*http.Response, error) { return okResponse(200), nil },
	}
	before := time.Now()
	h, e := s.CheckHealth(context.Background(), "homestub")
	if e != nil {
		t.Fatal(e)
	}
	if h.State != HealthReachable {
		t.Fatalf("state = %q, want reachable", h.State)
	}
	if h.CheckedAt.Before(before) || h.CheckedAt.After(time.Now()) {
		t.Fatalf("checkedAt %v outside probe window", h.CheckedAt)
	}
	var recorded domain.ProviderHealth
	for _, p := range s.Providers() {
		if p.ID == "homestub" {
			recorded = p.Health
		}
	}
	if recorded.State != "reachable" || recorded.CheckedAt.IsZero() {
		t.Fatalf("Providers() does not surface recorded health: %+v", recorded)
	}
}

func TestHealthTimeoutAndOfflineCarryCheckedAt(t *testing.T) {
	s, tr := healthService(t)
	tr.outcomes = []func() (*http.Response, error){
		func() (*http.Response, error) { return nil, timeoutErr{} },
		func() (*http.Response, error) { return nil, errors.New("connection refused") },
	}
	h, e := s.CheckHealth(context.Background(), "homestub")
	if e != nil {
		t.Fatal(e)
	}
	if h.State != HealthUnavailable || h.Reason != "timeout" {
		t.Fatalf("timeout probe: %+v", h)
	}
	if h.CheckedAt.IsZero() {
		t.Fatal("timeout result must carry checkedAt")
	}
	h, e = s.CheckHealth(context.Background(), "homestub")
	if e != nil {
		t.Fatal(e)
	}
	if h.State != HealthUnavailable || h.Reason != "offline" {
		t.Fatalf("offline probe: %+v", h)
	}
	if h.CheckedAt.IsZero() {
		t.Fatal("offline result must carry checkedAt")
	}
}

func TestHealthDegradedOnServerError(t *testing.T) {
	s, tr := healthService(t)
	tr.outcomes = []func() (*http.Response, error){
		func() (*http.Response, error) { return okResponse(503), nil },
	}
	h, e := s.CheckHealth(context.Background(), "homestub")
	if e != nil {
		t.Fatal(e)
	}
	if h.State != HealthDegraded || h.CheckedAt.IsZero() {
		t.Fatalf("degraded probe: %+v", h)
	}
}

func TestHealthRecoveryRestoresReachabilityWithoutRestart(t *testing.T) {
	s, tr := healthService(t)
	tr.outcomes = []func() (*http.Response, error){
		func() (*http.Response, error) { return nil, errors.New("connection refused") },
		func() (*http.Response, error) { return okResponse(200), nil },
	}
	h, _ := s.CheckHealth(context.Background(), "homestub")
	if h.State != HealthUnavailable {
		t.Fatalf("initial probe: %+v", h)
	}
	// Recovery is a later probe away; no restart, no re-registration.
	h, e := s.CheckHealth(context.Background(), "homestub")
	if e != nil {
		t.Fatal(e)
	}
	if h.State != HealthReachable {
		t.Fatalf("recovered probe: %+v", h)
	}
	// Health never touches enablement: the provider stays eligible and
	// explicitly selected providers stay selected.
	if !s.isEnabled("homestub") {
		t.Fatal("health probing must not disable a provider")
	}
	if s.Health("homestub").State != HealthReachable {
		t.Fatalf("recorded health: %+v", s.Health("homestub"))
	}
}

func TestRenderingDoesNotProbeProviders(t *testing.T) {
	s, tr := healthService(t)
	// The payload used when rendering pages and lists must not probe.
	for range 5 {
		_ = s.Providers()
	}
	if n := tr.total(); n != 0 {
		t.Fatalf("Providers() issued %d requests; rendering must not probe", n)
	}
	if h := s.Health("homestub"); h.State != HealthUnknown || !h.CheckedAt.IsZero() {
		t.Fatalf("unprobed health must stay unknown: %+v", h)
	}
}

func TestCheckHealthRejectsUnknownProvider(t *testing.T) {
	s, _ := healthService(t)
	if _, e := s.CheckHealth(context.Background(), "nobody"); e == nil {
		t.Fatal("expected invalid_provider for unknown ID")
	} else if pe, ok := e.(*Error); !ok || pe.Code != "invalid_provider" {
		t.Fatalf("unexpected error: %v", e)
	}
}
