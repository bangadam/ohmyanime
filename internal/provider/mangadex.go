package provider

import (
	"context"
	"encoding/json"
	"myanime/internal/domain"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const dexAPI = "https://api.mangadex.org"
const dexCovers = "https://mangadex.org/covers"
const dexRef = "https://mangadex.org/"

const dexID = "mangadex"
const dexPrefix = dexID + ":"

// MangaDex documents ~5 requests/second; the shared transport cooldown covers
// burst violations, and this adapter never issues parallel upstream calls.
var dexUUID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// mangadexAdapter reads the public MangaDex JSON API. Reads need no
// authentication; user features are out of scope.
type mangadexAdapter struct{ s *Service }

func newMangadex(s *Service) *mangadexAdapter { return &mangadexAdapter{s} }

func (a *mangadexAdapter) Identity() Identity {
	return Identity{ID: dexID, Name: "MangaDex", Kind: KindManga}
}
func (a *mangadexAdapter) HomePage() string { return dexRef }

func (a *mangadexAdapter) Search(ctx context.Context, query, cursor string, limit int) ([]domain.Manga, string, error) {
	offset, e := dexCursor(cursor)
	if e != nil {
		return nil, "", e
	}
	if limit <= 0 || limit > 50 {
		limit = 24
	}
	v := url.Values{}
	v.Set("limit", strconv.Itoa(limit))
	v.Set("offset", strconv.Itoa(offset))
	v.Add("includes[]", "cover_art")
	v.Add("includes[]", "author")
	v.Add("contentRating[]", "safe")
	v.Add("contentRating[]", "suggestive")
	v.Add("hasAvailableChapters", "true")
	if q := strings.TrimSpace(query); q != "" {
		v.Set("title", q)
	} else {
		// An empty query stays a browsable page: most followed titles.
		v.Add("order[followedCount]", "desc")
	}
	var page dexCollection
	if e := a.dexGet(ctx, dexAPI+"/manga?"+v.Encode(), &page); e != nil {
		return nil, "", e
	}
	out := make([]domain.Manga, 0, len(page.Data))
	for _, item := range page.Data {
		out = append(out, a.manga(item))
	}
	next := ""
	if page.Total > offset+len(page.Data) {
		next = strconv.Itoa(offset + len(page.Data))
	}
	return out, next, nil
}

func (a *mangadexAdapter) Detail(ctx context.Context, id string) (domain.Manga, error) {
	uuid, e := dexUUIDOf(id)
	if e != nil {
		return domain.Manga{}, e
	}
	v := url.Values{}
	v.Add("includes[]", "cover_art")
	v.Add("includes[]", "author")
	var one dexSingle
	if e := a.dexGet(ctx, dexAPI+"/manga/"+uuid+"?"+v.Encode(), &one); e != nil {
		return domain.Manga{}, e
	}
	return a.manga(one.Data), nil
}

func (a *mangadexAdapter) Chapters(ctx context.Context, mangaID string) ([]domain.Chapter, error) {
	uuid, e := dexUUIDOf(mangaID)
	if e != nil {
		return nil, e
	}
	out := []domain.Chapter{}
	// Feed pages hold at most 500 chapters; long runs need a bounded walk.
	for offset := 0; offset < 1000; offset += 100 {
		v := url.Values{}
		v.Set("limit", "100")
		v.Set("offset", strconv.Itoa(offset))
		v.Add("translatedLanguage[]", "en")
		v.Add("includes[]", "scanlation_group")
		v.Add("order[chapter]", "desc")
		v.Add("order[volume]", "desc")
		v.Add("contentRating[]", "safe")
		v.Add("contentRating[]", "suggestive")
		v.Add("contentRating[]", "erotica")
		var page dexChapterCollection
		if e := a.dexGet(ctx, dexAPI+"/manga/"+uuid+"/feed?"+v.Encode(), &page); e != nil {
			return nil, e
		}
		for _, item := range page.Data {
			ch, ok := a.chapter(item, mangaID)
			if ok {
				out = append(out, ch)
			}
		}
		if len(page.Data) < 100 || len(out) >= page.Total {
			break
		}
	}
	domain.SortChapters(out)
	return out, nil
}

func (a *mangadexAdapter) Chapter(ctx context.Context, chapterID string) (domain.Chapter, error) {
	uuid, e := dexUUIDOf(chapterID)
	if e != nil {
		return domain.Chapter{}, e
	}
	v := url.Values{}
	v.Add("includes[]", "scanlation_group")
	v.Add("includes[]", "manga")
	var one dexChapterSingle
	if e := a.dexGet(ctx, dexAPI+"/chapter/"+uuid+"?"+v.Encode(), &one); e != nil {
		return domain.Chapter{}, e
	}
	// MangaDex does not echo mangaId in chapter attributes; the parent
	// manga arrives as a relationship.
	mangaID := dexPrefix + one.Data.Attributes.MangaID
	for _, rel := range one.Data.Relationships {
		if rel.Type == "manga" {
			mangaID = dexPrefix + rel.ID
		}
	}
	ch, ok := a.chapter(one.Data, mangaID)
	if !ok {
		return domain.Chapter{}, failure("unavailable", dexID)
	}
	return ch, nil
}

func (a *mangadexAdapter) Pages(ctx context.Context, chapterID string) ([]domain.MangaPage, error) {
	uuid, e := dexUUIDOf(chapterID)
	if e != nil {
		return nil, e
	}
	var home dexAtHome
	if e := a.dexGet(ctx, dexAPI+"/at-home/server/"+uuid, &home); e != nil {
		return nil, e
	}
	files := home.Chapter.Data
	if len(files) == 0 {
		files = home.Chapter.DataSaver
	}
	if len(files) == 0 || len(files) > 200 {
		return nil, failure("malformed_source", dexID)
	}
	out := make([]domain.MangaPage, 0, len(files))
	for i, file := range files {
		u := absolute(home.BaseURL+"/data/"+home.Chapter.Hash+"/", file)
		if u == "" {
			return nil, failure("malformed_source", dexID)
		}
		out = append(out, domain.MangaPage{Index: i + 1, URL: u})
	}
	return out, nil
}

// dexGet performs one GET and decodes the documented envelope.
func (a *mangadexAdapter) dexGet(ctx context.Context, target string, out any) error {
	b, e := a.s.get(ctx, target, dexRef)
	if e != nil {
		return e
	}
	if e := json.Unmarshal(b, out); e != nil {
		return failure("malformed_source", dexID)
	}
	return nil
}

// manga maps one MangaDex entity to the application model.
func (a *mangadexAdapter) manga(item dexEntity) domain.Manga {
	attr := item.Attributes
	cover, author := "", ""
	for _, rel := range item.Relationships {
		switch rel.Type {
		case "cover_art":
			cover = dexCovers + "/" + item.ID + "/" + rel.Attributes.FileName
			if ext := dexThumbExt(rel.Attributes.FileName); ext != "" {
				cover += ext
			}
		case "author", "artist":
			if author == "" {
				author = rel.Attributes.Name
			}
		}
	}
	alt := []string{}
	seen := map[string]bool{}
	for _, titles := range attr.AltTitles {
		for _, t := range titles {
			if t = strings.TrimSpace(t); t != "" && !seen[t] && t != attr.Title.pick() {
				seen[t] = true
				alt = append(alt, t)
			}
		}
	}
	tags := []string{}
	for _, tag := range attr.Tags {
		if name := tag.Attributes.Name.pick(); name != "" {
			tags = append(tags, name)
		}
	}
	return domain.Manga{
		ID:                dexPrefix + item.ID,
		Title:             attr.Title.pick(),
		AlternativeTitles: alt,
		PosterURL:         cover,
		Synopsis:          attr.Description.pick(),
		Type:              dexType(attr.OriginalLanguage),
		Year:              attr.Year,
		Status:            attr.Status,
		Tags:              tags,
		Author:            author,
		Availability:      []string{dexID},
	}
}

// chapter maps one feed entity. Unavailable chapters are dropped, not zeroed.
func (a *mangadexAdapter) chapter(item dexChapterEntity, mangaID string) (domain.Chapter, bool) {
	attr := item.Attributes
	// Chapters hosted outside MangaDex (externalUrl, e.g. MangaPlus) have no
	// at-home page inventory, so they cannot be read here. Listing them would
	// offer a chapter that always fails on open.
	if attr.Pages <= 0 || attr.ExternalURL != "" {
		return domain.Chapter{}, false
	}
	ordinal := 0.0
	label := "Oneshot"
	// Numberless MangaDex releases are oneshots; a synthetic special keeps
	// domain.ChapterKey.Valid true so progress and ordering stay stable.
	special := "oneshot"
	if attr.Chapter != "" {
		ordinal, _ = strconv.ParseFloat(attr.Chapter, 64)
		label = "Ch. " + attr.Chapter
		special = ""
	}
	group := ""
	for _, rel := range item.Relationships {
		if rel.Type == "scanlation_group" && group == "" {
			group = rel.Attributes.Name
		}
	}
	return domain.Chapter{
		ID:          dexPrefix + item.ID,
		MangaID:     mangaID,
		Label:       label,
		Title:       strings.TrimSpace(attr.Title),
		Ordinal:     ordinal,
		Volume:      attr.Volume,
		Number:      attr.Chapter,
		Special:     special,
		Language:    attr.Language,
		PageCount:   attr.Pages,
		PublishedAt: attr.PublishAt,
		GroupName:   group,
	}, true
}

// dexCursor decodes the opaque pagination token; it is the upstream offset.
func dexCursor(cursor string) (int, error) {
	if cursor == "" {
		return 0, nil
	}
	n, e := strconv.Atoi(cursor)
	if e != nil || n < 0 || n > 10000 {
		return 0, failure("invalid_cursor", dexID)
	}
	return n, nil
}

func dexUUIDOf(id string) (string, error) {
	uuid, ok := strings.CutPrefix(id, dexPrefix)
	if !ok || !dexUUID.MatchString(uuid) {
		return "", failure("invalid_mapping", dexID)
	}
	return uuid, nil
}

// dexThumbExt turns cover.jpg into the MangaDex thumbnail suffix.
func dexThumbExt(fileName string) string {
	if i := strings.LastIndexByte(fileName, '.'); i > 0 && len(fileName)-i <= 5 {
		return ".512.jpg"
	}
	return ""
}

func dexType(original string) string {
	switch original {
	case "ko":
		return "Manhwa"
	case "zh":
		return "Manhua"
	default:
		return "Manga"
	}
}

// dexTitle picks the English title first, then romanized, then any.
type dexTitle map[string]string

func (t dexTitle) pick() string {
	if t == nil {
		return ""
	}
	for _, k := range []string{"en", "ja-ro", "ko-ro", "zh-ro"} {
		if v := strings.TrimSpace(t[k]); v != "" {
			return v
		}
	}
	for _, v := range t {
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}
	return ""
}

// MangaDex envelope shapes. Only the fields the application consumes are
// declared; unknown members are ignored on decode.
type dexCollection struct {
	Result string      `json:"result"`
	Data   []dexEntity `json:"data"`
	Total  int         `json:"total"`
}

type dexSingle struct {
	Result string    `json:"result"`
	Data   dexEntity `json:"data"`
}

type dexEntity struct {
	ID            string            `json:"id"`
	Type          string            `json:"type"`
	Attributes    dexAttributes     `json:"attributes"`
	Relationships []dexRelationship `json:"relationships"`
}

type dexAttributes struct {
	Title            dexTitle            `json:"title"`
	AltTitles        []map[string]string `json:"altTitles"`
	Description      dexTitle            `json:"description"`
	OriginalLanguage string              `json:"originalLanguage"`
	Status           string              `json:"status"`
	Year             int                 `json:"year"`
	Tags             []dexTag            `json:"tags"`
}

// dexChapterEntity carries chapter attributes, whose title is a plain
// string unlike the localized manga title map.
type dexChapterCollection struct {
	Result string             `json:"result"`
	Data   []dexChapterEntity `json:"data"`
	Total  int                `json:"total"`
}

type dexChapterSingle struct {
	Result string           `json:"result"`
	Data   dexChapterEntity `json:"data"`
}

type dexChapterEntity struct {
	ID            string            `json:"id"`
	Type          string            `json:"type"`
	Attributes    dexChapterAttrs   `json:"attributes"`
	Relationships []dexRelationship `json:"relationships"`
}

type dexChapterAttrs struct {
	Chapter     string `json:"chapter"`
	Volume      string `json:"volume"`
	Title       string `json:"title"`
	Pages       int    `json:"pages"`
	ExternalURL string `json:"externalUrl"`
	PublishAt   string `json:"publishAt"`
	Language    string `json:"translatedLanguage"`
	MangaID     string `json:"mangaId"`
}

type dexTag struct {
	Attributes struct {
		Name dexTitle `json:"name"`
	} `json:"attributes"`
}

type dexRelationship struct {
	ID         string `json:"id"`
	Type       string `json:"type"`
	Attributes struct {
		Name     string `json:"name"`
		FileName string `json:"fileName"`
	} `json:"attributes"`
}

type dexAtHome struct {
	Result  string `json:"result"`
	BaseURL string `json:"baseUrl"`
	Chapter struct {
		Hash      string   `json:"hash"`
		Data      []string `json:"data"`
		DataSaver []string `json:"dataSaver"`
	} `json:"chapter"`
}

// mangaProviderOf resolves a provider-prefixed manga/chapter ID to its
// adapter, e.g. mangadex:<uuid> -> the MangaDex adapter.
func (s *Service) mangaProviderOf(id string) (MangaProvider, error) {
	slug, _, ok := strings.Cut(id, ":")
	if !ok {
		return nil, failure("invalid_mapping", id)
	}
	p, registered := s.reg.lookupManga(slug)
	if !registered {
		// An unknown source is a caller mistake, not a transient outage.
		return nil, failure("invalid_provider", slug)
	}
	if !s.isEnabled(slug) {
		return nil, failure("provider_disabled", slug)
	}
	return p, nil
}

// mangaProviders lists the enabled manga adapters in registration order.
func (s *Service) mangaProviders() []MangaProvider {
	out := []MangaProvider{}
	for _, p := range s.reg.mangaProviders() {
		if s.isEnabled(p.Identity().ID) {
			out = append(out, p)
		}
	}
	return out
}

// MangaSearch queries every enabled manga adapter and merges the first page
// of each, caching the merged result for two minutes. Cursor pagination
// continues on the provider named by the cursor.
func (s *Service) MangaSearch(ctx context.Context, query, cursor string, limit int) ([]domain.Manga, string, error) {
	key := "manga-search:" + query + ":" + cursor + ":" + strconv.Itoa(limit)
	if c, ok := s.getCache(key); ok {
		v := c.value.(mangaSearchEntry)
		return v.items, v.next, c.err
	}
	if cursor != "" {
		slug, rest, ok := strings.Cut(cursor, ":")
		if !ok {
			return nil, "", failure("invalid_cursor", cursor)
		}
		p, e := s.mangaProviderOf(slug + ":")
		if e != nil {
			return nil, "", e
		}
		items, next, e := p.Search(ctx, query, rest, limit)
		if e != nil {
			return nil, "", e
		}
		if next != "" {
			next = slug + ":" + next
		}
		s.putCache(key, mangaSearchEntry{items, next}, nil, 2*time.Minute)
		return items, next, nil
	}
	items := []domain.Manga{}
	next := ""
	for _, p := range s.mangaProviders() {
		page, n, e := p.Search(ctx, query, "", limit)
		if e != nil {
			continue
		}
		items = append(items, page...)
		if n != "" {
			next = p.Identity().ID + ":" + n
		}
	}
	if len(items) == 0 && next == "" {
		return nil, "", failure("unavailable", "manga")
	}
	s.putCache(key, mangaSearchEntry{items, next}, nil, 2*time.Minute)
	return items, next, nil
}

// MangaSearchProvider restricts a search to one explicitly chosen adapter.
// The cursor is that adapter's own token (no cross-provider prefix), because
// the selection is fixed for the whole result set. An unknown or disabled
// provider fails instead of silently searching everywhere.
func (s *Service) MangaSearchProvider(ctx context.Context, provider, query, cursor string, limit int) ([]domain.Manga, string, error) {
	slug := provider
	if !strings.HasSuffix(slug, ":") {
		slug += ":"
	}
	p, e := s.mangaProviderOf(slug)
	if e != nil {
		return nil, "", e
	}
	if !s.isEnabled(p.Identity().ID) {
		return nil, "", failure("provider_disabled", p.Identity().ID)
	}
	key := "manga-search:" + p.Identity().ID + ":" + query + ":" + cursor + ":" + strconv.Itoa(limit)
	if c, ok := s.getCache(key); ok {
		v := c.value.(mangaSearchEntry)
		return v.items, v.next, c.err
	}
	items, next, e := p.Search(ctx, query, cursor, limit)
	if e != nil {
		return nil, "", e
	}
	if len(items) == 0 && next == "" {
		return nil, "", failure("unavailable", p.Identity().ID)
	}
	s.putCache(key, mangaSearchEntry{items, next}, nil, 2*time.Minute)
	return items, next, nil
}

// MangaDetail returns one manga with its metadata, cached for ten minutes.
func (s *Service) MangaDetail(ctx context.Context, id string) (domain.Manga, error) {
	p, e := s.mangaProviderOf(id)
	if e != nil {
		return domain.Manga{}, e
	}
	key := "manga:" + id
	if c, ok := s.getCache(key); ok {
		return c.value.(domain.Manga), c.err
	}
	v, e := p.Detail(ctx, id)
	if e != nil {
		return domain.Manga{}, e
	}
	s.putCache(key, v, nil, 10*time.Minute)
	return v, nil
}

// MangaChapters lists readable chapters, cached for five minutes.
func (s *Service) MangaChapters(ctx context.Context, mangaID string) ([]domain.Chapter, error) {
	p, e := s.mangaProviderOf(mangaID)
	if e != nil {
		return nil, e
	}
	key := "manga-chapters:" + mangaID
	if c, ok := s.getCache(key); ok {
		return c.value.([]domain.Chapter), c.err
	}
	v, e := p.Chapters(ctx, mangaID)
	if e != nil {
		return nil, e
	}
	s.putCache(key, v, nil, 5*time.Minute)
	return v, nil
}

// MangaChapter returns one chapter's metadata.
func (s *Service) MangaChapter(ctx context.Context, chapterID string) (domain.Chapter, error) {
	p, e := s.mangaProviderOf(chapterID)
	if e != nil {
		return domain.Chapter{}, e
	}
	key := "manga-chapter:" + chapterID
	if c, ok := s.getCache(key); ok {
		return c.value.(domain.Chapter), c.err
	}
	v, e := p.Chapter(ctx, chapterID)
	if e != nil {
		return domain.Chapter{}, e
	}
	s.putCache(key, v, nil, 10*time.Minute)
	return v, nil
}

// MangaPages resolves one chapter's image URLs, cached for ten minutes.
func (s *Service) MangaPages(ctx context.Context, chapterID string) ([]domain.MangaPage, error) {
	p, e := s.mangaProviderOf(chapterID)
	if e != nil {
		return nil, e
	}
	key := "manga-pages:" + chapterID
	if c, ok := s.getCache(key); ok {
		return c.value.([]domain.MangaPage), c.err
	}
	v, e := p.Pages(ctx, chapterID)
	if e != nil {
		return nil, e
	}
	s.putCache(key, v, nil, 10*time.Minute)
	return v, nil
}

type mangaSearchEntry struct {
	items []domain.Manga
	next  string
}
