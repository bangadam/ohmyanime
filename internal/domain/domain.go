package domain

import "context"

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
	Query, Cursor, Genre, Season string
	Year, Limit                  int
}
type CatalogPage struct {
	Items      []Anime `json:"items"`
	NextCursor *string `json:"nextCursor"`
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
}
type Catalog interface {
	Search(context.Context, CatalogQuery) (CatalogPage, error)
	Detail(context.Context, string) (Anime, error)
	Episodes(context.Context, string) ([]Episode, error)
	Resolve(context.Context, ResolveRequest) (Source, error)
	Providers() []ProviderInfo
}
