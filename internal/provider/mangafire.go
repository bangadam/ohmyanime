package provider

import (
	"context"
	"encoding/json"
	"myanime/internal/domain"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

const mfBase = "https://mangafire.to"

// mfOrigin is the Referer the image CDN requires for hotlink-protected pages.
const mfOrigin = mfBase + "/"

const mfID = "mangafire"
const mfPrefix = mfID + ":"

// mangafireAdapter reads MangaFire's internal JSON API. Every request needs
// a vrf token produced by the site's obfuscated polyfill, so the adapter owns
// a webprotectionOracle instance. Chapter IDs are numeric upstream IDs.
type mangafireAdapter struct {
	s *Service
	o *webprotectionOracle
}

func newMangafire(s *Service) *mangafireAdapter {
	return &mangafireAdapter{s: s, o: newOracle(s, mfBase)}
}

func (a *mangafireAdapter) Identity() Identity {
	return Identity{ID: mfID, Name: "MangaFire", Kind: KindManga}
}

// get signs the canonical query, then issues the request with the same
// params; the token is computed over the sorted key=value form.
func (a *mangafireAdapter) get(ctx context.Context, path string, params [][2]string, out any) error {
	plain := canonicalQuery(path, params)
	vrf, e := a.o.Sign(ctx, plain)
	if e != nil {
		return e
	}
	v := url.Values{}
	for _, kv := range params {
		v.Add(kv[0], kv[1])
	}
	v.Set("vrf", vrf)
	b, e := a.s.get(ctx, mfBase+"/api"+path+"?"+v.Encode(), mfBase+"/")
	if e != nil {
		return e
	}
	if e := json.Unmarshal(b, out); e != nil {
		return failure("malformed_source", mfID)
	}
	return nil
}

func (a *mangafireAdapter) Search(ctx context.Context, query, cursor string, limit int) ([]domain.Manga, string, error) {
	page := 1
	if cursor != "" {
		n, e := strconv.Atoi(cursor)
		if e != nil || n < 1 || n > 200 {
			return nil, "", failure("invalid_cursor", mfID)
		}
		page = n
	}
	if limit <= 0 || limit > 50 {
		limit = 24
	}
	params := [][2]string{
		{"content_rating[0]", "safe"},
		{"content_rating[1]", "suggestive"},
	}
	if q := strings.TrimSpace(query); q != "" {
		params = append(params,
			[2]string{"keyword", q},
			[2]string{"limit", strconv.Itoa(limit)},
			[2]string{"order[relevance]", "desc"},
			[2]string{"page", strconv.Itoa(page)},
		)
	} else {
		params = append(params,
			[2]string{"limit", strconv.Itoa(limit)},
			[2]string{"order[chapter_updated_at]", "desc"},
			[2]string{"page", strconv.Itoa(page)},
		)
	}
	var result mfTitles
	if e := a.get(ctx, "/titles", params, &result); e != nil {
		return nil, "", e
	}
	out := make([]domain.Manga, 0, len(result.Items))
	for _, it := range result.Items {
		out = append(out, mfManga(it))
	}
	next := ""
	if result.Meta != nil && result.Meta.CurrentPage < result.Meta.LastPage {
		next = strconv.Itoa(result.Meta.CurrentPage + 1)
	}
	return out, next, nil
}

func (a *mangafireAdapter) Detail(ctx context.Context, id string) (domain.Manga, error) {
	hid, e := mfHID(id)
	if e != nil {
		return domain.Manga{}, e
	}
	var one mfEntity
	if e := a.get(ctx, "/titles/"+hid, nil, &one); e != nil {
		return domain.Manga{}, e
	}
	return mfMangaDetail(one.Data), nil
}

func (a *mangafireAdapter) Chapters(ctx context.Context, mangaID string) ([]domain.Chapter, error) {
	hid, e := mfHID(mangaID)
	if e != nil {
		return nil, e
	}
	out := []domain.Chapter{}
	// Upstream pages hold at most 500 chapters; a bounded walk covers the
	// longest serials the interface can usefully render.
	for page := 1; page <= 100; page++ {
		var list mfChapters
		params := [][2]string{
			{"language", "en"},
			{"limit", "100"},
			{"order", "desc"},
			{"page", strconv.Itoa(page)},
			{"sort", "number"},
		}
		if e := a.get(ctx, "/titles/"+hid+"/chapters", params, &list); e != nil {
			return nil, e
		}
		for _, it := range list.Items {
			if it.Number > 0 {
				out = append(out, mfChapter(it, mangaID))
			}
		}
		if len(list.Items) < 100 {
			break
		}
	}
	// Upstream is newest-first; the reader wants reading order.
	sort.SliceStable(out, func(i, j int) bool { return out[i].Ordinal < out[j].Ordinal })
	return out, nil
}

func (a *mangafireAdapter) Chapter(ctx context.Context, chapterID string) (domain.Chapter, error) {
	num, e := mfChapterNumber(chapterID)
	if e != nil {
		return domain.Chapter{}, e
	}
	var one mfChapterEntity
	if e := a.get(ctx, "/chapters/"+num, nil, &one); e != nil {
		return domain.Chapter{}, e
	}
	mangaID := mfPrefix + one.Data.Title.HID
	return mfChapter(mfChapterItem{
		ID:       one.Data.ID,
		Number:   one.Data.Number,
		Name:     one.Data.Name,
		Language: "en",
	}, mangaID), nil
}

func (a *mangafireAdapter) Pages(ctx context.Context, chapterID string) ([]domain.MangaPage, error) {
	num, e := mfChapterNumber(chapterID)
	if e != nil {
		return nil, e
	}
	var one mfChapterEntity
	if e := a.get(ctx, "/chapters/"+num, nil, &one); e != nil {
		return nil, e
	}
	if len(one.Data.Pages) == 0 || len(one.Data.Pages) > 300 {
		return nil, failure("malformed_source", mfID)
	}
	out := make([]domain.MangaPage, 0, len(one.Data.Pages))
	for i, p := range one.Data.Pages {
		if p.URL == "" {
			return nil, failure("malformed_source", mfID)
		}
		// The image CDN rejects hotlinked requests without the site origin.
		out = append(out, domain.MangaPage{Index: i + 1, URL: p.URL, Referer: mfOrigin})
	}
	return out, nil
}

func mfManga(it mfItem) domain.Manga {
	return domain.Manga{
		ID:           mfPrefix + it.HID,
		Title:        strings.TrimSpace(it.Title),
		PosterURL:    it.Poster.Medium,
		Type:         strings.Title(strings.ToLower(it.Type)),
		Year:         it.Year,
		Status:       mfStatus(it.Status),
		Availability: []string{mfID},
	}
}

func mfMangaDetail(d mfEntityData) domain.Manga {
	m := mfManga(mfItem{HID: d.HID, Title: d.Title, Type: d.Type, Year: d.Year, Status: d.Status, Poster: d.Poster})
	m.Synopsis = plainHTML(d.SynopsisHTML)
	m.AlternativeTitles = d.AltTitles
	if len(d.Authors) > 0 {
		m.Author = d.Authors[0].Title
	}
	for _, g := range d.Genres {
		m.Tags = append(m.Tags, g.Title)
	}
	return m
}

func mfChapter(it mfChapterItem, mangaID string) domain.Chapter {
	label := "Ch. " + strconv.FormatFloat(it.Number, 'f', -1, 64)
	return domain.Chapter{
		ID:          mfPrefix + strconv.Itoa(it.ID),
		MangaID:     mangaID,
		Label:       label,
		Title:       strings.TrimSpace(it.Name),
		Ordinal:     it.Number,
		Number:      strconv.FormatFloat(it.Number, 'f', -1, 64),
		Language:    it.Language,
		PublishedAt: mfUnixTime(it.CreatedAt),
	}
}

func mfStatus(s string) string {
	switch s {
	case "releasing":
		return "ongoing"
	case "completed":
		return "completed"
	case "on_hiatus":
		return "hiatus"
	case "discontinued", "cancelled":
		return "cancelled"
	}
	return "unknown"
}

func mfUnixTime(sec int64) string {
	if sec <= 0 {
		return ""
	}
	return strconv.FormatInt(sec, 10)
}

var mfHIDPattern = regexp.MustCompile(`^[a-z0-9]{2,16}$`)
var mfNumber = regexp.MustCompile(`^[0-9]+$`)

func mfHID(id string) (string, error) {
	hid, ok := strings.CutPrefix(id, mfPrefix)
	if !ok || !mfHIDPattern.MatchString(hid) {
		return "", failure("invalid_mapping", mfID)
	}
	return hid, nil
}

func mfChapterNumber(id string) (string, error) {
	num, ok := strings.CutPrefix(id, mfPrefix)
	if !ok || !mfNumber.MatchString(num) {
		return "", failure("invalid_mapping", mfID)
	}
	return num, nil
}

// MangaFire envelope shapes; unknown members are ignored on decode.
type mfTitles struct {
	Items []mfItem `json:"items"`
	Meta  *struct {
		CurrentPage int `json:"current_page"`
		LastPage    int `json:"last_page"`
	} `json:"meta"`
}

type mfItem struct {
	ID             int         `json:"id"`
	HID            string      `json:"hid"`
	Slug           string      `json:"slug"`
	Title          string      `json:"title"`
	Type           string      `json:"type"`
	Status         string      `json:"status"`
	Poster         mfPoster    `json:"poster"`
	LatestChapter  json.Number `json:"latestChapter"`
	Year           int         `json:"year"`
	Rank           int         `json:"rank"`
	ChapterUpdated string      `json:"chapterUpdatedAt"`
	URL            string      `json:"url"`
}

type mfPoster struct {
	Small  string `json:"small"`
	Medium string `json:"medium"`
	Large  string `json:"large"`
}

type mfEntity struct {
	Data mfEntityData `json:"data"`
}

type mfEntityData struct {
	ID     int      `json:"id"`
	HID    string   `json:"hid"`
	Slug   string   `json:"slug"`
	Title  string   `json:"title"`
	Type   string   `json:"type"`
	Status string   `json:"status"`
	Poster mfPoster `json:"poster"`
	// Upstream reports decimal chapters (e.g. 111.1), so this cannot be int.
	LatestChapter json.Number `json:"latestChapter"`
	Year          int         `json:"year"`
	SynopsisHTML  string      `json:"synopsisHtml"`
	AltTitles     []string    `json:"altTitles"`
	Authors       []struct {
		ID    int    `json:"id"`
		Title string `json:"title"`
	} `json:"authors"`
	Genres []struct {
		ID    int    `json:"id"`
		Title string `json:"title"`
	} `json:"genres"`
}

type mfChapters struct {
	Items []mfChapterItem `json:"items"`
}

type mfChapterItem struct {
	ID        int     `json:"id"`
	Number    float64 `json:"number"`
	Name      string  `json:"name"`
	Language  string  `json:"language"`
	Type      string  `json:"type"`
	CreatedAt int64   `json:"createdAt"`
}

type mfChapterEntity struct {
	Data struct {
		ID     int     `json:"id"`
		Number float64 `json:"number"`
		Name   string  `json:"name"`
		Type   string  `json:"type"`
		Title  struct {
			ID   int    `json:"id"`
			HID  string `json:"hid"`
			Slug string `json:"slug"`
			Name string `json:"name"`
		} `json:"title"`
		Pages []struct {
			URL    string `json:"url"`
			Width  int    `json:"width"`
			Height int    `json:"height"`
		} `json:"pages"`
	} `json:"data"`
}

// plainHTML strips tags from an HTML synopsis, keeping words separated and
// decoding the small entity set the site emits.
func plainHTML(raw string) string {
	var b strings.Builder
	skip := false
	for _, r := range raw {
		switch {
		case r == '<':
			skip = true
		case r == '>':
			skip = false
			b.WriteByte(' ')
		case !skip && (unicode.IsPrint(r) || r == '\n'):
			b.WriteRune(r)
		}
	}
	s := strings.Join(strings.Fields(b.String()), " ")
	s = strings.NewReplacer("&quot;", `"`, "&amp;", "&", "&#39;", "'", "&lt;", "<", "&gt;", ">", "&nbsp;", " ").Replace(s)
	return s
}
