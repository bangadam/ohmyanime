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

// stubAnimeProvider is an in-process adapter used to prove registration alone
// makes a source reachable: no resolver branch knows about it.
type stubAnimeProvider struct {
	id       string
	modes    []string
	episodes []providerEpisode
	source   domain.Source
	mapped   []domain.Anime
}

func (p *stubAnimeProvider) Identity() Identity {
	return Identity{ID: p.id, Name: "Stub " + p.id, Kind: KindAnime}
}
func (p *stubAnimeProvider) Modes() []string { return p.modes }
func (p *stubAnimeProvider) Map(ctx context.Context, a domain.Anime) (string, error) {
	p.mapped = append(p.mapped, a)
	return "stub-slug", nil
}
func (p *stubAnimeProvider) Episodes(ctx context.Context, slug string) ([]providerEpisode, error) {
	return p.episodes, nil
}
func (p *stubAnimeProvider) Resolve(ctx context.Context, slug, label, mode string) (domain.Source, error) {
	return p.source, nil
}

func offlineService() *Service {
	s := New()
	s.client.Transport = transportFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 503, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{}`)), Request: nil}, nil
	})
	return s
}

func TestRegisterAnimeAdapterNeedsNoResolverEdits(t *testing.T) {
	s := offlineService()
	for _, p := range s.Providers() {
		if e := s.SetEnabled(p.ID, false); e != nil {
			t.Fatal(e)
		}
	}
	a := domain.Anime{ID: "anilist:77", Title: "Stub Title"}
	s.putCache("anime:"+a.ID, a, nil, time.Hour)
	stub := &stubAnimeProvider{
		id:       "stubsource",
		modes:    []string{"sub", "dub"},
		episodes: []providerEpisode{{label: "1", key: "k1", title: "Episode 1", ordinal: 1}},
		source:   domain.Source{MasterURL: "https://cdn.example/master.m3u8"},
	}
	if e := s.RegisterAnime(stub); e != nil {
		t.Fatal(e)
	}
	// The registered adapter is listed and enabled without further wiring.
	listed := false
	for _, p := range s.Providers() {
		if p.ID == "stubsource" && p.Enabled && len(p.Modes) == 2 {
			listed = true
		}
	}
	if !listed {
		t.Fatalf("registered adapter not listed: %v", s.Providers())
	}
	eps, e := s.Episodes(context.Background(), a.ID)
	if e != nil || len(eps) != 1 || eps[0].Availability[0] != "stubsource" {
		t.Fatalf("inventory %v %v", eps, e)
	}
	if len(stub.mapped) != 1 || stub.mapped[0].ID != a.ID {
		t.Fatalf("adapter not mapped through: %v", stub.mapped)
	}
	// Explicit playback selection reaches the registered adapter.
	src, e := s.Resolve(context.Background(), domain.ResolveRequest{AnimeID: a.ID, EpisodeID: episodeID(a.ID, "1"), Mode: "sub", Provider: "stubsource"})
	if e != nil {
		t.Fatal(e)
	}
	if src.Provider != "stubsource" || src.Mode != "sub" || src.MasterURL != "https://cdn.example/master.m3u8" {
		t.Fatalf("source %+v", src)
	}
}

func TestRegisterDuplicateAnimeAdapterRejected(t *testing.T) {
	s := offlineService()
	if e := s.RegisterAnime(&stubAnimeProvider{id: "hianime", modes: []string{"sub"}}); e == nil {
		t.Fatal("duplicate registration accepted")
	}
}

func TestRegisterInvalidIdentityRejected(t *testing.T) {
	s := offlineService()
	if e := s.RegisterAnime(&stubAnimeProvider{id: "", modes: []string{"sub"}}); e == nil {
		t.Fatal("empty ID accepted")
	}
}

func TestSetEnabledUnknownProviderRejected(t *testing.T) {
	s := offlineService()
	if e := s.SetEnabled("nosuchprovider", false); e == nil {
		t.Fatal("unknown provider accepted")
	}
}

// An explicit provider whose upstream fails must return the failure, never a
// result or request from another provider.
func TestExplicitProviderFailureNeverFallsBack(t *testing.T) {
	s := offlineService()
	var mu sync.Mutex
	hosts := map[string]int{}
	s.client.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		hosts[r.URL.Host]++
		mu.Unlock()
		return &http.Response{StatusCode: 500, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{}`)), Request: r}, nil
	})
	a := domain.Anime{ID: "anilist:9", Title: "Test"}
	s.putCache("anime:"+a.ID, a, nil, time.Hour)
	_, e := s.Resolve(context.Background(), domain.ResolveRequest{AnimeID: a.ID, EpisodeID: episodeID(a.ID, "1"), Mode: "sub", Provider: "hianime"})
	if e == nil {
		t.Fatal("expected failure")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(hosts) != 1 {
		t.Fatalf("other providers contacted: %v", hosts)
	}
	if hosts["hianime.at"] == 0 {
		t.Fatalf("expected provider not contacted: %v", hosts)
	}
}

// A cancelled context must close in-flight upstream HTTP work.
func TestCancelledContextClosesUpstreamRequest(t *testing.T) {
	s := offlineService()
	started := make(chan struct{}, 1)
	s.client.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		select {
		case started <- struct{}{}:
		default:
		}
		<-r.Context().Done()
		return nil, r.Context().Err()
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, e := s.get(ctx, hiBase+"/search?keyword=x", "")
		done <- e
	}()
	<-started // the request is in flight
	cancel()
	select {
	case e := <-done:
		if !errors.Is(e, context.Canceled) {
			t.Fatalf("want context.Canceled, got %v", e)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled context did not close the upstream request")
	}
}

// Cancellation also aborts the resolve path before any source is returned.
func TestResolveCancelledContextAbortsUpstream(t *testing.T) {
	s := offlineService()
	a := domain.Anime{ID: "anilist:3", Title: "Test"}
	s.putCache("anime:"+a.ID, a, nil, time.Hour)
	s.client.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		<-r.Context().Done()
		return nil, r.Context().Err()
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, e := s.Resolve(ctx, domain.ResolveRequest{AnimeID: a.ID, EpisodeID: episodeID(a.ID, "1"), Mode: "sub", Provider: "hianime"})
	if !errors.Is(e, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", e)
	}
}
