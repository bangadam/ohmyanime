package domain

import (
	"context"
	"time"
)

type Anime struct {
	ID                string   `json:"id"`
	Title             string   `json:"title"`
	AlternativeTitles []string `json:"alternativeTitles,omitempty"`
	PosterURL         string   `json:"posterUrl,omitempty"`
	Synopsis          string   `json:"synopsis,omitempty"`
	Type              string   `json:"type,omitempty"`
	Season            string   `json:"season,omitempty"`
	Year              int      `json:"year,omitempty"`
	Status            string   `json:"status,omitempty"`
	Availability      []string `json:"availability"`
	Genres            []string `json:"genres,omitempty"`
	Tags              []string `json:"tags,omitempty"`
}
type Episode struct {
	ID           string   `json:"id"`
	AnimeID      string   `json:"animeId"`
	Label        string   `json:"label"`
	Title        string   `json:"title,omitempty"`
	Ordinal      float64  `json:"ordinal"`
	Availability []string `json:"availability"`
}
type CatalogQuery struct {
	Query, Cursor, Season, Status, Type, Sort string
	Genres, Tags                              []string
	Year, Limit                               int
}

// Sort tokens accepted by the catalogue. newest orders by start date while
// updated orders by the last metadata change upstream.
const (
	CatalogSortPopular   = "popular"
	CatalogSortTrending  = "trending"
	CatalogSortScore     = "score"
	CatalogSortNewest    = "newest"
	CatalogSortUpdated   = "updated"
	CatalogSortTitleAsc  = "title_asc"
	CatalogSortTitleDesc = "title_desc"
)

type CatalogPage struct {
	Items      []Anime `json:"items"`
	NextCursor *string `json:"nextCursor"`
}
type Manga struct {
	ID                string   `json:"id"`
	Title             string   `json:"title"`
	AlternativeTitles []string `json:"alternativeTitles,omitempty"`
	PosterURL         string   `json:"posterUrl,omitempty"`
	Synopsis          string   `json:"synopsis,omitempty"`
	Type              string   `json:"type,omitempty"`
	Year              int      `json:"year,omitempty"`
	Status            string   `json:"status,omitempty"`
	Tags              []string `json:"tags,omitempty"`
	Author            string   `json:"author,omitempty"`
	Availability      []string `json:"availability"`
}
type Chapter struct {
	ID      string  `json:"id"`
	MangaID string  `json:"mangaId"`
	Label   string  `json:"label"`
	Title   string  `json:"title,omitempty"`
	Ordinal float64 `json:"ordinal"`
	// Volume, Number and Special carry the chapter identity dimensions used
	// by domain.ChapterKey; they keep "12", "12.5" and specials distinct.
	Volume      string `json:"volume,omitempty"`
	Number      string `json:"number,omitempty"`
	Special     string `json:"special,omitempty"`
	Language    string `json:"language"`
	PageCount   int    `json:"pageCount"`
	PublishedAt string `json:"publishedAt,omitempty"`
	GroupName   string `json:"groupName,omitempty"`
}
type MangaPage struct {
	Index int    `json:"index"`
	URL   string `json:"url"`
	// Referer is the origin some image CDNs require for hotlink protection.
	// It is provider-supplied metadata for the proxy, never sent to clients.
	Referer string `json:"-"`
}
type ResolveRequest struct {
	AnimeID   string `json:"animeId"`
	EpisodeID string `json:"episodeId"`
	Mode      string `json:"mode"`
	Provider  string `json:"provider"`
	Quality   string `json:"quality"`
}
type Variant struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Height int    `json:"height,omitempty"`
	URL    string `json:"-"`
}
type Subtitle struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Language string `json:"language"`
	URL      string `json:"url"`
	Default  bool   `json:"default"`
}
type Interval struct {
	Start float64 `json:"start"`
	End   float64 `json:"end"`
}
type Source struct {
	MasterURL, Referer, Provider, Mode string
	Variants                           []Variant
	Subtitles                          []Subtitle
	Intro, Outro                       *Interval
	Hardsub                            bool
	HardsubLanguage                    string
}
type ProviderInfo struct {
	ID      string   `json:"id"`
	Enabled bool     `json:"enabled"`
	Modes   []string `json:"modes"`
	Status  string   `json:"status"`
	// Capabilities is the provider-level feature set: what the source can
	// serve in general, never a claim about any specific title.
	Capabilities ProviderCapabilities `json:"capabilities"`
	Health       ProviderHealth       `json:"health"`
}

// ProviderHealth is the last bounded reachability probe of a provider's
// homepage. Reachability never guarantees that any particular episode or
// chapter plays.
type ProviderHealth struct {
	State     string    `json:"state"`
	CheckedAt time.Time `json:"checkedAt,omitempty"`
	Reason    string    `json:"reason,omitempty"`
}
type ProviderCapabilities struct {
	// Kinds lists the content kinds (anime, manga) the source supports.
	Kinds []string `json:"kinds"`
	// AudioModes lists audio modes (sub, dub) the source can serve.
	AudioModes []string `json:"audioModes"`
	// ReaderFormats lists manga reader formats the source can serve.
	ReaderFormats []string `json:"readerFormats"`
}
type Catalog interface {
	Search(context.Context, CatalogQuery) (CatalogPage, error)
	Detail(context.Context, string) (Anime, error)
	Episodes(context.Context, string) ([]Episode, error)
	Resolve(context.Context, ResolveRequest) (Source, error)
	Providers() []ProviderInfo
}
