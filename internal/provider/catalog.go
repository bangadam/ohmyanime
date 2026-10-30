package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"myanime/internal/domain"
	"sort"
	"strconv"
	"strings"
	"time"
)

const metadataURL = "https://graphql.anilist.co"
const mediaFields = `id idMal title { romaji english native } synonyms coverImage { large } description format season seasonYear status genres tags { name rank isMediaSpoiler }`

type mediaTag struct {
	Name           string
	Rank           int
	IsMediaSpoiler bool `json:"isMediaSpoiler"`
}

type media struct {
	ID                                  int
	IDMal                               int `json:"idMal"`
	Title                               struct{ Romaji, English, Native string }
	Synonyms                            []string
	CoverImage                          struct{ Large string }
	Description, Format, Season, Status string
	SeasonYear                          int
	Genres                              []string
	Tags                                []mediaTag
}

func (m media) anime() domain.Anime {
	title := m.Title.Romaji
	if title == "" {
		title = m.Title.English
	}
	aliases := append([]string{}, m.Synonyms...)
	for _, t := range []string{m.Title.English, m.Title.Native} {
		if t != "" && t != title {
			aliases = append(aliases, t)
		}
	}
	return domain.Anime{ID: fmt.Sprintf("anilist:%d", m.ID), Title: title, AlternativeTitles: aliases, PosterURL: m.CoverImage.Large, Synopsis: plain(m.Description), Type: m.Format, Season: strings.ToLower(m.Season), Year: m.SeasonYear, Status: m.Status, Genres: normalizeTerms(m.Genres, 0), Tags: animeTags(m.Tags), Availability: []string{}}
}

// normalizeTerms trims values, drops empty and repeated ones (case-insensitive,
// first spelling wins) and caps the result at limit (0 means no cap).
func normalizeTerms(in []string, limit int) []string {
	out := make([]string, 0, len(in))
	seen := make(map[string]bool, len(in))
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		key := strings.ToLower(v)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, v)
		if limit > 0 && len(out) == limit {
			break
		}
	}
	return out
}

// animeTags drops spoiler and low-rank tags: spoiler tags leak plot, and
// low-rank tags are shared noise that would bloat every title payload. The rest
// are ordered by relevance and capped so payloads stay small.
func animeTags(in []mediaTag) []string {
	tags := make([]mediaTag, 0, len(in))
	for _, t := range in {
		if t.IsMediaSpoiler || t.Rank < animeTagRankFloor || t.Name == "" {
			continue
		}
		tags = append(tags, t)
	}
	sort.Slice(tags, func(i, j int) bool {
		if tags[i].Rank != tags[j].Rank {
			return tags[i].Rank > tags[j].Rank
		}
		return tags[i].Name < tags[j].Name
	})
	if len(tags) > maxAnimeTags {
		tags = tags[:maxAnimeTags]
	}
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		out = append(out, t.Name)
	}
	return out
}
func (s *Service) graphql(ctx context.Context, q string, vars map[string]any, out any) error {
	b, e := s.request(ctx, "POST", metadataURL, "", map[string]any{"query": q, "variables": vars}, nil)
	if e != nil {
		return e
	}
	var envelope struct {
		Data   json.RawMessage
		Errors []json.RawMessage
	}
	if json.Unmarshal(b, &envelope) != nil || len(envelope.Errors) > 0 || len(envelope.Data) == 0 {
		return failure("malformed_source", "anilist")
	}
	if e = json.Unmarshal(envelope.Data, out); e != nil {
		return failure("malformed_source", "anilist")
	}
	return nil
}

const (
	animeTagRankFloor      = 60
	maxAnimeTags           = 20
	maxCatalogTerms        = 10
	defaultCatalogPageSize = 24
	maxCatalogPageSize     = 50
	maxCatalogPageNumber   = 10000
)

// catalogStatuses and catalogFormats travel as GraphQL variables, so their
// tokens only need to be valid enum members.
var catalogStatuses = map[string]string{
	"releasing":        "RELEASING",
	"finished":         "FINISHED",
	"not_yet_released": "NOT_YET_RELEASED",
	"cancelled":        "CANCELLED",
	"hiatus":           "HIATUS",
}

var catalogFormats = map[string]string{
	"tv":       "TV",
	"tv_short": "TV_SHORT",
	"movie":    "MOVIE",
	"special":  "SPECIAL",
	"ova":      "OVA",
	"ona":      "ONA",
	"music":    "MUSIC",
}

var catalogSeasons = map[string]string{"WINTER": "WINTER", "SPRING": "SPRING", "SUMMER": "SUMMER", "FALL": "FALL"}

// catalogSorts keeps the request token next to its upstream enum. The value
// travels as a [MediaSort] variable, so it must already be an allowlisted
// token; validation happens before any request is made.
var catalogSorts = map[string]string{
	domain.CatalogSortPopular:   "POPULARITY_DESC",
	domain.CatalogSortTrending:  "TRENDING_DESC",
	domain.CatalogSortScore:     "SCORE_DESC",
	domain.CatalogSortNewest:    "START_DATE_DESC",
	domain.CatalogSortUpdated:   "UPDATED_AT_DESC",
	domain.CatalogSortTitleAsc:  "TITLE_ROMAJI",
	domain.CatalogSortTitleDesc: "TITLE_ROMAJI_DESC",
}

// enumToken validates a request value against an allowlist. Empty means the
// filter is absent; anything unknown fails before a request is made.
func enumToken(value string, allowed map[string]string, code string) (string, error) {
	if value == "" {
		return "", nil
	}
	if token, ok := allowed[value]; ok {
		return token, nil
	}
	return "", failure(code, "catalog")
}

// catalogQuery builds the AniList document and variables for one request.
// Only filters that are set are declared, since GraphQL rejects unused
// variables. Genre and tag lists reach upstream as genre_in/tag_in, which ANDs
// their entries.
func catalogQuery(q domain.CatalogQuery) (string, map[string]any, error) {
	page := 1
	if q.Cursor != "" {
		n, err := strconv.Atoi(q.Cursor)
		if err != nil || n < 1 || n > maxCatalogPageNumber {
			return "", nil, failure("invalid_cursor", "catalog")
		}
		page = n
	}
	limit := q.Limit
	if limit < 1 {
		limit = defaultCatalogPageSize
	}
	if limit > maxCatalogPageSize {
		limit = maxCatalogPageSize
	}
	sortToken, err := enumToken(q.Sort, catalogSorts, "invalid_sort")
	if err != nil {
		return "", nil, err
	}
	if sortToken == "" {
		sortToken = catalogSorts[domain.CatalogSortPopular]
	}
	status, err := enumToken(q.Status, catalogStatuses, "invalid_status")
	if err != nil {
		return "", nil, err
	}
	format, err := enumToken(q.Type, catalogFormats, "invalid_type")
	if err != nil {
		return "", nil, err
	}
	season, err := enumToken(strings.ToUpper(q.Season), catalogSeasons, "invalid_season")
	if err != nil {
		return "", nil, err
	}
	decls := []string{"$page:Int", "$perPage:Int"}
	args := []string{"type:ANIME", "isAdult:false"}
	vars := map[string]any{"page": page, "perPage": limit}
	if q.Query != "" {

		decls = append(decls, "$search:String")
		args = append(args, "search:$search")
		vars["search"] = q.Query
	}
	if status != "" {
		decls = append(decls, "$status:MediaStatus")
		args = append(args, "status:$status")
		vars["status"] = status
	}
	if format != "" {
		decls = append(decls, "$format:MediaFormat")
		args = append(args, "format:$format")
		vars["format"] = format
	}
	if genres := normalizeTerms(q.Genres, maxCatalogTerms); len(genres) > 0 {
		decls = append(decls, "$genreIn:[String]")
		args = append(args, "genre_in:$genreIn")
		vars["genreIn"] = genres
	}
	if tags := normalizeTerms(q.Tags, maxCatalogTerms); len(tags) > 0 {
		decls = append(decls, "$tags:[String]")
		args = append(args, "tag_in:$tags")
		vars["tags"] = tags
	}
	if season != "" {
		decls = append(decls, "$season:MediaSeason")
		args = append(args, "season:$season")
		vars["season"] = season
	}
	if q.Year > 0 {
		decls = append(decls, "$year:Int")
		args = append(args, "seasonYear:$year")
		vars["year"] = q.Year
	}
	// Sort is always present, so it stays a declared variable rather than an
	// interpolated literal: the value is allowlisted above and travels through
	// the GraphQL variable map like every other filter.
	decls = append(decls, "$sort:[MediaSort]")
	args = append(args, "sort:$sort")
	vars["sort"] = []string{sortToken}
	document := `query(` + strings.Join(decls, ",") + `){Page(page:$page,perPage:$perPage){pageInfo{hasNextPage} media(` + strings.Join(args, ",") + `){` + mediaFields + `}}}`
	return document, vars, nil
}

func (s *Service) Search(ctx context.Context, q domain.CatalogQuery) (domain.CatalogPage, error) {
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	out := domain.CatalogPage{Items: []domain.Anime{}}
	document, vars, err := catalogQuery(q)
	if err != nil {
		return out, err
	}
	var result struct {
		Page struct {
			PageInfo struct{ HasNextPage bool }
			Media    []media
		}
	}
	if e := s.graphql(ctx, document, vars, &result); e != nil {
		return out, e
	}
	for _, m := range result.Page.Media {
		a := m.anime()
		out.Items = append(out.Items, a)
		s.putCache("anime:"+a.ID, a, nil, 30*time.Minute)
		s.putCache("mal:"+a.ID, m.IDMal, nil, 24*time.Hour)
	}
	if result.Page.PageInfo.HasNextPage {
		page, _ := vars["page"].(int)
		v := strconv.Itoa(page + 1)
		out.NextCursor = &v
	}
	return out, nil
}
func (s *Service) Detail(ctx context.Context, id string) (domain.Anime, error) {
	if c, ok := s.getCache("anime:" + id); ok {
		return c.value.(domain.Anime), c.err
	}
	n, e := animeNumber(id)
	if e != nil {
		return domain.Anime{}, e
	}
	var result struct{ Media media }
	if e = s.graphql(ctx, `query($id:Int){Media(id:$id,type:ANIME,isAdult:false){`+mediaFields+`}}`, map[string]any{"id": n}, &result); e != nil {
		return domain.Anime{}, e
	}
	if result.Media.ID != n {
		return domain.Anime{}, failure("unavailable", "anilist")
	}
	a := result.Media.anime()
	s.putCache("anime:"+id, a, nil, 30*time.Minute)
	s.putCache("mal:"+id, result.Media.IDMal, nil, 24*time.Hour)
	return a, nil
}
func (s *Service) Genres(ctx context.Context) (any, error) {
	var result struct{ GenreCollection []string }
	if e := s.graphql(ctx, `{GenreCollection}`, nil, &result); e != nil {
		return nil, e
	}
	items := []map[string]string{}
	for _, g := range result.GenreCollection {
		if !strings.EqualFold(g, "hentai") {
			items = append(items, map[string]string{"id": g, "name": g})
		}
	}
	return map[string]any{"items": items}, nil
}

// mediaTagEntry is one entry of the upstream tag vocabulary. The collection's
// rank is null upstream (rank only exists per media), so the vocabulary must be
// filtered by its spoiler and adult flags instead.
type mediaTagEntry struct {
	Name             string
	Category         string
	IsAdult          bool `json:"isAdult"`
	IsGeneralSpoiler bool `json:"isGeneralSpoiler"`
	IsMediaSpoiler   bool `json:"isMediaSpoiler"`
}

// tagVocabulary keeps the facet list free of adult and spoiler tags: the
// catalogue is not adult-filtered, and a spoiler tag offered as a filter would
// either leak plot or always return zero titles, because per-title tag mapping
// drops spoilers. Category rides on the vocabulary entry rather than on every
// title so per-title payloads stay small.
func tagVocabulary(in []mediaTagEntry) []map[string]string {
	type tag struct{ name, category string }
	seen := make(map[string]bool, len(in))
	tags := make([]tag, 0, len(in))
	for _, t := range in {
		if t.IsAdult || t.IsGeneralSpoiler || t.IsMediaSpoiler || t.Name == "" {
			continue
		}
		key := strings.ToLower(t.Name)
		if seen[key] {
			continue
		}
		seen[key] = true
		tags = append(tags, tag{name: t.Name, category: t.Category})
	}
	sort.Slice(tags, func(i, j int) bool {
		if tags[i].category != tags[j].category {
			return tags[i].category < tags[j].category
		}
		return tags[i].name < tags[j].name
	})
	items := make([]map[string]string, 0, len(tags))
	for _, t := range tags {
		items = append(items, map[string]string{"id": t.name, "name": t.name, "category": t.category})
	}
	return items
}

// cachedTagEntries rebuilds a tag vocabulary from titles already fetched when
// the upstream collection is unavailable or has no entries. Per-title tags
// carry no category, so the fallback entries leave it empty rather than
// inventing a grouping. The result is sorted by tagVocabulary like the live
// collection, keeping facet order deterministic across both paths.
func (s *Service) cachedTagEntries() []mediaTagEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	entries := make([]mediaTagEntry, 0)
	for key, cached := range s.cache {
		if !strings.HasPrefix(key, "anime:") || cached.err != nil || !now.Before(cached.until) {
			continue
		}
		a, ok := cached.value.(domain.Anime)
		if !ok {
			continue
		}
		for _, name := range a.Tags {
			name = strings.TrimSpace(name)
			if name != "" {
				entries = append(entries, mediaTagEntry{Name: name})
			}
		}
	}
	// Map iteration is unordered and duplicates may carry different spellings;
	// sort before tagVocabulary so the chosen spelling is deterministic.
	sort.Slice(entries, func(i, j int) bool {
		a, b := strings.ToLower(entries[i].Name), strings.ToLower(entries[j].Name)
		if a != b {
			return a < b
		}
		return entries[i].Name < entries[j].Name
	})
	return entries
}

// Tags lists the upstream tag vocabulary for catalogue facets. When the
// collection query succeeds but returns nothing (the field is missing for this
// upstream revision), the vocabulary is derived from cached titles so the
// browse filters stay usable instead of silently disappearing.
func (s *Service) Tags(ctx context.Context) (any, error) {
	var result struct {
		MediaTagCollection []mediaTagEntry
	}
	if e := s.graphql(ctx, `{MediaTagCollection{name category isAdult isGeneralSpoiler isMediaSpoiler}}`, nil, &result); e != nil {
		return nil, e
	}
	entries := result.MediaTagCollection
	if len(entries) == 0 {
		entries = s.cachedTagEntries()
	}
	return map[string]any{"items": tagVocabulary(entries)}, nil
}
func (s *Service) Schedule(ctx context.Context) (any, error) {
	now := time.Now().Unix()
	var result struct {
		Page struct {
			AiringSchedules []struct {
				ID       int     `json:"id"`
				AiringAt int64   `json:"airingAt"`
				Episode  float64 `json:"episode"`
				Media    media   `json:"media"`
			}
		}
	}
	q := `query($from:Int,$to:Int){Page(page:1,perPage:50){airingSchedules(airingAt_greater:$from,airingAt_lesser:$to,sort:TIME){id airingAt episode media{` + mediaFields + `}}}}`
	if e := s.graphql(ctx, q, map[string]any{"from": now, "to": now + 7*86400}, &result); e != nil {
		return nil, e
	}
	items := []map[string]any{}
	for _, a := range result.Page.AiringSchedules {
		items = append(items, map[string]any{"id": a.ID, "airingAt": a.AiringAt, "episode": a.Episode, "anime": a.Media.anime()})
	}
	return map[string]any{"items": items}, nil
}

// aniZipURL returns every episode title for one title in a single response:
// {"episodes":{"1":{"title":{"en":"…","x-jat":"…"}},…}}. AniList has no
// per-episode titles and its streamingEpisodes list only covers recent
// episodes, so it is not usable here.
const aniZipURL = "https://api.ani.zip/mappings?anilist_id="

// episodeTitles maps episode numbers to titles. Failure is not fatal:
// providers still supply labels, and the interface falls back to them.
func (s *Service) episodeTitles(ctx context.Context, animeID string) map[float64]string {
	key := "titles:" + animeID
	if c, ok := s.getCache(key); ok {
		titles, _ := c.value.(map[float64]string)
		return titles
	}
	n, e := animeNumber(animeID)
	if e != nil {
		return nil
	}
	b, e := s.requestLimit(ctx, "GET", aniZipURL+strconv.Itoa(n), "", nil, nil, 8<<20)
	if e != nil {
		s.putCache(key, map[float64]string{}, e, 2*time.Minute)
		return nil
	}
	var result struct {
		Episodes map[string]struct {
			Title map[string]string
		}
	}
	if json.Unmarshal(b, &result) != nil {
		s.putCache(key, map[float64]string{}, failure("malformed_source", "anizip"), 2*time.Minute)
		return nil
	}
	titles := make(map[float64]string, len(result.Episodes))
	for raw, ep := range result.Episodes {
		number, e := strconv.ParseFloat(raw, 64)
		if e != nil || number <= 0 {
			continue
		}
		for _, lang := range []string{"en", "x-jat", "ja"} {
			if title := strings.TrimSpace(ep.Title[lang]); title != "" {
				titles[number] = title
				break
			}
		}
	}
	s.putCache(key, titles, nil, 6*time.Hour)
	return titles
}
