package provider

import (
	"context"
	"myanime/internal/domain"
	"sync"
)

// Kind distinguishes the content contracts a provider can fulfil.
type Kind string

const (
	KindAnime Kind = "anime"
	KindManga Kind = "manga"
)

// Identity is the provider identity shared by every content contract.
type Identity struct {
	// ID is the stable slug used in requests, settings and storage.
	ID string
	// Name is the human-readable label shown in the interface.
	Name string
	// Kind selects the contract the adapter fulfils.
	Kind Kind
}

// AnimeProvider is the contract for anime sources. Orchestration — caching,
// cancellation, enablement and explicit selection — stays in Service, so an
// adapter only talks to its own upstream.
type AnimeProvider interface {
	Identity() Identity
	// Modes lists the audio modes the upstream can serve.
	Modes() []string
	// Map resolves an AniList ID to the provider's own work identifier.
	Map(ctx context.Context, a domain.Anime) (slug string, err error)
	// Episodes lists the provider's playable inventory for one slug.
	Episodes(ctx context.Context, slug string) ([]providerEpisode, error)
	// Resolve returns a playable source for one episode label and mode.
	Resolve(ctx context.Context, slug, label, mode string) (domain.Source, error)
}

// MangaProvider is the contract for manga sources. Orchestration — caching
// and enablement — stays in Service, so an adapter only talks to its own
// upstream. IDs are provider-prefixed (mangadex:<uuid>) end to end.
type MangaProvider interface {
	Identity() Identity
	Search(ctx context.Context, query, cursor string, limit int) ([]domain.Manga, string, error)
	Detail(ctx context.Context, id string) (domain.Manga, error)
	Chapters(ctx context.Context, mangaID string) ([]domain.Chapter, error)
	Chapter(ctx context.Context, chapterID string) (domain.Chapter, error)
	Pages(ctx context.Context, chapterID string) ([]domain.MangaPage, error)
}

// registry holds the in-process adapters by kind. Adding an adapter is a
// registration call, never an edit to a resolver branch.
type registry struct {
	mu         sync.RWMutex
	anime      map[string]AnimeProvider
	animeOrder []string
	manga      map[string]MangaProvider
	mangaOrder []string
}

func newRegistry() *registry {
	return &registry{anime: map[string]AnimeProvider{}, manga: map[string]MangaProvider{}}
}

func (r *registry) registerAnime(p AnimeProvider) error {
	id := p.Identity()
	if id.ID == "" || id.Kind != KindAnime {
		return failure("invalid_provider", id.ID)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, dup := r.anime[id.ID]; dup {
		return failure("invalid_provider", id.ID)
	}
	r.anime[id.ID] = p
	r.animeOrder = append(r.animeOrder, id.ID)
	return nil
}

func (r *registry) registerManga(p MangaProvider) error {
	id := p.Identity()
	if id.ID == "" || id.Kind != KindManga {
		return failure("invalid_provider", id.ID)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, dup := r.manga[id.ID]; dup {
		return failure("invalid_provider", id.ID)
	}
	r.manga[id.ID] = p
	r.mangaOrder = append(r.mangaOrder, id.ID)
	return nil
}

// animeProviders lists the registered anime adapters in registration order.
func (r *registry) animeProviders() []AnimeProvider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]AnimeProvider, 0, len(r.animeOrder))
	for _, id := range r.animeOrder {
		out = append(out, r.anime[id])
	}
	return out
}

// mangaProviders lists the registered manga adapters in registration order.
func (r *registry) mangaProviders() []MangaProvider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]MangaProvider, 0, len(r.mangaOrder))
	for _, id := range r.mangaOrder {
		out = append(out, r.manga[id])
	}
	return out
}

// lookupAnime returns the adapter registered under an anime provider ID.
func (r *registry) lookupAnime(id string) (AnimeProvider, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.anime[id]
	return p, ok
}

// lookupManga returns the adapter registered under a manga provider ID.
func (r *registry) lookupManga(id string) (MangaProvider, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.manga[id]
	return p, ok
}

// known reports whether any contract registered this provider ID.
func (r *registry) known(id string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if _, ok := r.anime[id]; ok {
		return true
	}
	_, ok := r.manga[id]
	return ok
}

// AudioMode is a provider-level audio capability. It says the upstream can
// serve that mode somewhere, never that any particular title offers it:
// per-title availability and per-session tracks are discovered separately.
type AudioMode string

const (
	AudioSub AudioMode = "sub"
	AudioDub AudioMode = "dub"
)

// ReaderFormat is a provider-level manga reading capability, reserved for the
// manga contract. Anime adapters leave it empty.
type ReaderFormat string

// Capabilities is the typed, provider-level feature set an adapter declares.
// It is strictly separate from episode/chapter availability and from the
// subtitle/audio tracks a resolve session discovers at playback time.
type Capabilities struct {
	// Kinds lists the content contracts the adapter fulfils.
	Kinds []Kind
	// AudioModes lists audio modes the upstream can serve.
	AudioModes []AudioMode
	// ReaderFormats lists manga reading formats the upstream can serve.
	ReaderFormats []ReaderFormat
}

// capabilitySource is optionally implemented by adapters that declare richer
// provider-level capabilities than their Modes() list. Adapters that do not
// implement it get capabilities derived conservatively: only the audio modes
// they already advertise, and no reader formats.
type capabilitySource interface {
	Capabilities() Capabilities
}

// animeCapabilities resolves an anime adapter's provider-level capabilities.
// An undeclared capability is never inferred: a mode absent from the adapter's
// own declaration stays absent here.
func animeCapabilities(p AnimeProvider) Capabilities {
	if c, ok := p.(capabilitySource); ok {
		return c.Capabilities()
	}
	caps := Capabilities{Kinds: []Kind{p.Identity().Kind}}
	for _, m := range p.Modes() {
		if AudioMode(m) == AudioSub || AudioMode(m) == AudioDub {
			caps.AudioModes = append(caps.AudioModes, AudioMode(m))
		}
	}
	return caps
}

// mangaCapabilities resolves a manga adapter's provider-level capabilities.
// Without an explicit declaration only the fulfilled kind is reported.
func mangaCapabilities(p MangaProvider) Capabilities {
	if c, ok := p.(capabilitySource); ok {
		return c.Capabilities()
	}
	return Capabilities{Kinds: []Kind{p.Identity().Kind}}
}

// supportsAudioMode reports whether a provider-level capability declaration
// covers one audio mode. Missing declarations fail closed.
func (c Capabilities) supportsAudioMode(mode string) bool {
	for _, m := range c.AudioModes {
		if string(m) == mode {
			return true
		}
	}
	return false
}
