package provider

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// HealthState is the observed reachability of a provider's homepage. It is
// bounded evidence from a single manual or low-frequency probe, never a
// guarantee that any particular episode or chapter plays.
type HealthState string

const (
	// HealthUnknown means no probe has run since startup.
	HealthUnknown HealthState = "unknown"
	// HealthReachable means the homepage answered successfully.
	HealthReachable HealthState = "reachable"
	// HealthDegraded means the homepage answered but errored (e.g. HTTP 4xx/5xx).
	HealthDegraded HealthState = "degraded"
	// HealthUnavailable means the probe timed out or could not connect.
	HealthUnavailable HealthState = "unavailable"
)

// Health is the last observed reachability of one provider.
type Health struct {
	State HealthState `json:"state"`
	// CheckedAt is when the state was observed; zero while unknown.
	CheckedAt time.Time `json:"checkedAt,omitempty"`
	// Reason is a short machine-readable cause, e.g. timeout, offline.
	Reason string `json:"reason,omitempty"`
}

// probeTimeout bounds a single homepage probe.
const probeTimeout = 5 * time.Second

// homePage is optionally implemented by adapters that can name a homepage to
// probe. Adapters without one stay HealthUnknown until they implement it.
type homePage interface {
	HomePage() string
}

// CheckHealth probes one provider's homepage once, records the result, and
// returns it. Every terminal path stamps CheckedAt, including timeouts and
// offline failures. Probing never changes enablement or selection.
func (s *Service) CheckHealth(ctx context.Context, id string) (Health, error) {
	home, ok := s.homeOf(id)
	if !ok {
		return Health{}, failure("invalid_provider", id)
	}
	h := s.probe(ctx, id, home)
	s.mu.Lock()
	s.health[id] = h
	s.mu.Unlock()
	return h, nil
}

// CheckAllHealth probes every registered provider once, concurrently, with a
// per-provider timeout. It is the manual refresh path; nothing in normal
// rendering calls it.
func (s *Service) CheckAllHealth(ctx context.Context) {
	ids := s.providerIDs()
	var wg sync.WaitGroup
	for _, id := range ids {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			_, _ = s.CheckHealth(ctx, id)
		}(id)
	}
	wg.Wait()
}

// Health returns the recorded state for one provider without probing.
func (s *Service) Health(id string) Health {
	s.mu.Lock()
	defer s.mu.Unlock()
	h, ok := s.health[id]
	if !ok {
		return Health{State: HealthUnknown}
	}
	return h
}

// providerIDs lists every registered provider ID in registration order.
func (s *Service) providerIDs() []string {
	ids := []string{}
	for _, p := range s.reg.animeProviders() {
		ids = append(ids, p.Identity().ID)
	}
	for _, p := range s.reg.mangaProviders() {
		ids = append(ids, p.Identity().ID)
	}
	return ids
}

// homeOf resolves a provider's homepage URL from whichever contract
// registered it.
func (s *Service) homeOf(id string) (string, bool) {
	if p, ok := s.reg.lookupAnime(id); ok {
		if hp, ok := p.(homePage); ok {
			return hp.HomePage(), true
		}
	}
	if p, ok := s.reg.lookupManga(id); ok {
		if hp, ok := p.(homePage); ok {
			return hp.HomePage(), true
		}
	}
	return "", s.reg.known(id)
}

// probe performs one bounded GET against the homepage and classifies the
// outcome. Every terminal path stamps CheckedAt, including timeouts and
// offline failures.
func (s *Service) probe(ctx context.Context, id, home string) Health {
	if home == "" {
		return Health{State: HealthUnknown, CheckedAt: time.Now(), Reason: "no_homepage"}
	}
	u, e := url.Parse(home)
	if e != nil || validateURL(u) != nil {
		return Health{State: HealthUnknown, CheckedAt: time.Now(), Reason: "invalid_homepage"}
	}
	pctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	r, e := http.NewRequestWithContext(pctx, http.MethodGet, home, nil)
	if e != nil {
		return Health{State: HealthUnavailable, CheckedAt: time.Now(), Reason: "offline"}
	}
	r.Header.Set("User-Agent", "Mozilla/5.0 MyAnime/1.0")
	resp, e := s.client.Do(r)
	if e != nil {
		state, reason := HealthUnavailable, "offline"
		if errors.Is(e, context.DeadlineExceeded) || pctx.Err() != nil {
			state, reason = HealthUnavailable, "timeout"
		} else if ctx.Err() != nil {
			state, reason = HealthUnavailable, "cancelled"
		} else {
			var ne net.Error
			if errors.As(e, &ne) && ne.Timeout() {
				// Some transports report deadline exhaustion as a net.Error
				// rather than a wrapped context error.
				state, reason = HealthUnavailable, "timeout"
			}
		}
		return Health{State: state, CheckedAt: time.Now(), Reason: reason}
	}
	io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
	resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 400 {
		return Health{State: HealthReachable, CheckedAt: time.Now()}
	}
	return Health{State: HealthDegraded, CheckedAt: time.Now(), Reason: http.StatusText(resp.StatusCode)}
}
