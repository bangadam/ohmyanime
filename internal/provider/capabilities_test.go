package provider

import (
	"context"
	"testing"
	"time"

	"myanime/internal/domain"
)

// capabilityStub declares capabilities only through the derived Modes() list.
type capabilityStub struct {
	stubAnimeProvider
}

// richStub declares capabilities explicitly through the optional interface.
type richStub struct {
	stubAnimeProvider
}

func (p *richStub) Capabilities() Capabilities {
	return Capabilities{
		Kinds:         []Kind{KindAnime},
		AudioModes:    []AudioMode{AudioSub},
		ReaderFormats: []ReaderFormat{"long-strip"},
	}
}

func registerCapabilityStub(t *testing.T, p AnimeProvider) *Service {
	t.Helper()
	s := offlineService()
	for _, existing := range s.Providers() {
		if e := s.SetEnabled(existing.ID, false); e != nil {
			t.Fatal(e)
		}
	}
	if e := s.RegisterAnime(p); e != nil {
		t.Fatal(e)
	}
	return s
}

func capabilityAnime() domain.Anime {
	a := domain.Anime{ID: "anilist:91", Title: "Capability Title"}
	return a
}

// The Providers() payload must expose provider-level capabilities: the kinds
// the source fulfils and the audio modes it can serve, derived from the
// adapter's own declaration and nothing else.
func TestProvidersExposeDeclaredCapabilities(t *testing.T) {
	s := registerCapabilityStub(t, &capabilityStub{stubAnimeProvider{
		id:       "capstub",
		modes:    []string{"sub", "dub"},
		episodes: []providerEpisode{{label: "1", key: "k1", title: "Episode 1", ordinal: 1}},
	}})
	a := capabilityAnime()
	s.putCache("anime:"+a.ID, a, nil, time.Hour)
	var info *domain.ProviderInfo
	for i, p := range s.Providers() {
		if p.ID == "capstub" {
			info = &s.Providers()[i]
		}
	}
	if info == nil {
		t.Fatalf("registered adapter not listed: %v", s.Providers())
	}
	caps := info.Capabilities
	if len(caps.Kinds) != 1 || caps.Kinds[0] != "anime" {
		t.Fatalf("kinds %+v", caps)
	}
	if len(caps.AudioModes) != 2 || caps.AudioModes[0] != "sub" || caps.AudioModes[1] != "dub" {
		t.Fatalf("audio modes %+v", caps)
	}
	if len(caps.ReaderFormats) != 0 {
		t.Fatalf("reader formats leaked into anime adapter: %+v", caps)
	}
}

// An explicit Capabilities() declaration wins over derivation, so richer
// provider-level feature sets (reader formats for manga-capable sources)
// survive the projection onto the wire shape.
func TestExplicitCapabilitiesDeclarationWins(t *testing.T) {
	s := registerCapabilityStub(t, &richStub{stubAnimeProvider{
		id:    "richstub",
		modes: []string{"sub", "dub"},
	}})
	for _, p := range s.Providers() {
		if p.ID != "richstub" {
			continue
		}
		caps := p.Capabilities
		if len(caps.AudioModes) != 1 || caps.AudioModes[0] != "sub" {
			t.Fatalf("explicit declaration not honoured: %+v", caps)
		}
		if len(caps.ReaderFormats) != 1 || caps.ReaderFormats[0] != "long-strip" {
			t.Fatalf("reader formats lost: %+v", caps)
		}
		return
	}
	t.Fatalf("registered adapter not listed: %v", s.Providers())
}

// Missing capability must fail conservatively: a provider that does not
// declare dub support is never asked to serve it, even when explicitly
// selected.
func TestUndeclaredDubModeNeverOffered(t *testing.T) {
	s := registerCapabilityStub(t, &capabilityStub{stubAnimeProvider{
		id:       "subonly",
		modes:    []string{"sub"},
		episodes: []providerEpisode{{label: "1", key: "k1", title: "Episode 1", ordinal: 1}},
		source:   domain.Source{MasterURL: "https://cdn.example/master.m3u8"},
	}})
	a := capabilityAnime()
	s.putCache("anime:"+a.ID, a, nil, time.Hour)
	req := domain.ResolveRequest{AnimeID: a.ID, EpisodeID: episodeID(a.ID, "1"), Mode: "dub", Provider: "subonly"}
	if _, e := s.Resolve(context.Background(), req); e == nil {
		t.Fatal("undeclared dub mode resolved anyway")
	}
}

// Declaring a provider-level dub capability never implies a title has dub:
// the same declaration must still serve sub, and the resolved source reports
// the requested mode rather than a capability-invented one.
func TestDubCapabilityNeverImpliesTitleAvailability(t *testing.T) {
	s := registerCapabilityStub(t, &capabilityStub{stubAnimeProvider{
		id:       "dubcapable",
		modes:    []string{"sub", "dub"},
		episodes: []providerEpisode{{label: "1", key: "k1", title: "Episode 1", ordinal: 1}},
		source:   domain.Source{MasterURL: "https://cdn.example/master.m3u8"},
	}})
	a := capabilityAnime()
	s.putCache("anime:"+a.ID, a, nil, time.Hour)
	src, e := s.Resolve(context.Background(), domain.ResolveRequest{
		AnimeID: a.ID, EpisodeID: episodeID(a.ID, "1"), Mode: "sub", Provider: "dubcapable",
	})
	if e != nil || src.Mode != "sub" {
		t.Fatalf("sub resolve %+v %v", src, e)
	}
}

// supportsAudioMode fails closed on partial or absent declarations.
func TestSupportsAudioModeFailsClosed(t *testing.T) {
	if (Capabilities{}).supportsAudioMode("sub") {
		t.Fatal("empty capabilities supported a mode")
	}
	caps := Capabilities{AudioModes: []AudioMode{AudioSub}}
	if !caps.supportsAudioMode("sub") {
		t.Fatal("declared sub not supported")
	}
	if caps.supportsAudioMode("dub") {
		t.Fatal("undeclared dub supported")
	}
	if caps.supportsAudioMode("") {
		t.Fatal("empty mode supported")
	}
}
