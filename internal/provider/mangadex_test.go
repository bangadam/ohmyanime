package provider

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

// Chapter fixture IDs must be UUIDs: dexUUIDOf rejects anything else before
// any request is made.
const (
	dexCh1ID = "mangadex:11111111-1111-4111-8111-111111111111"
	dexCh2ID = "mangadex:22222222-2222-4222-8222-222222222222"
	dexCh3ID = "mangadex:33333333-3333-4333-8333-333333333333"
	dexCh4ID = "mangadex:44444444-4444-4444-8444-444444444444"
	dexManga = "mangadex:f9c33607-9180-4ba6-b85c-e4b5faee7192"
)

// dexRouter serves recorded MangaDex fixtures by endpoint so one test can
// drive the whole search→chapters→pages flow without any network access.
type dexRouter map[string]string

func (r dexRouter) RoundTrip(req *http.Request) (*http.Response, error) {
	body, ok := r.match(req.URL.Path)
	status := 200
	if !ok {
		body, status = `{"result":"error"}`, 404
	}
	return &http.Response{StatusCode: status, Header: make(http.Header),
		Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
}

// match resolves one recorded body for a request path. Endpoints are matched
// in a fixed order so overlapping /manga prefixes stay deterministic.
func (r dexRouter) match(path string) (string, bool) {
	switch {
	case strings.HasSuffix(path, "/feed"):
		body, ok := r["feed"]
		return body, ok
	case strings.HasPrefix(path, "/at-home/"):
		body, ok := r["at-home"]
		return body, ok
	case strings.HasPrefix(path, "/chapter/"):
		body, ok := r["chapter"]
		return body, ok
	case path == "/manga":
		body, ok := r["search"]
		return body, ok
	case strings.HasPrefix(path, "/manga/"):
		body, ok := r["detail"]
		return body, ok
	}
	return "", false
}

func dexFixtureService(r dexRouter) *Service {
	s := New()
	s.client.Transport = r
	return s
}

const dexSearchPage = `{"result":"ok","data":[
 {"id":"f9c33607-9180-4ba6-b85c-e4b5faee7192","type":"manga","attributes":{
   "title":{"ja-ro":"One Piece","en":"One Piece"},"altTitles":[{"ja":"ワンピース"},{"en":"OP"}],
   "description":{"en":"Pirate king."},"originalLanguage":"ja","status":"ongoing","year":1997,
   "tags":[{"attributes":{"name":{"en":"Adventure"}}}]}},
 {"id":"a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d","type":"manga","attributes":{
   "title":{"ko":"원피스"},"altTitles":[],"description":{},
   "originalLanguage":"ko","status":"completed","year":0,"tags":[]}}
],"total":32}`

const dexFeedPage = `{"result":"ok","data":[
 {"id":"11111111-1111-4111-8111-111111111111","type":"chapter","attributes":{"chapter":"12.5",
   "volume":"3","title":"Recap","pages":4,"publishAt":"2024-01-03T00:00:00Z",
   "translatedLanguage":"en"},"relationships":[]},
 {"id":"22222222-2222-4222-8222-222222222222","type":"chapter","attributes":{"chapter":"2",
   "volume":"1","title":"","pages":20,"publishAt":"2024-01-01T00:00:00Z",
   "translatedLanguage":"en"},"relationships":[
   {"id":"g1","type":"scanlation_group","attributes":{"name":"Group A"}}]},
 {"id":"33333333-3333-4333-8333-333333333333","type":"chapter","attributes":{"chapter":"12",
   "volume":"3","title":"","pages":19,"publishAt":"2024-01-02T00:00:00Z",
   "translatedLanguage":"en"},"relationships":[]},
 {"id":"44444444-4444-4444-8444-444444444444","type":"chapter","attributes":{"chapter":"",
   "volume":"","title":"Oneshot","pages":50,"publishAt":"2024-01-04T00:00:00Z",
   "translatedLanguage":"en"},"relationships":[]},
 {"id":"55555555-5555-4555-8555-555555555555","type":"chapter","attributes":{"chapter":"3",
   "volume":"1","title":"","pages":0,"publishAt":"2024-01-05T00:00:00Z",
   "translatedLanguage":"en"},"relationships":[]},
 {"id":"66666666-6666-4666-8666-666666666666","type":"chapter","attributes":{"chapter":"99",
   "volume":"1","title":"","pages":18,"publishAt":"2024-01-06T00:00:00Z",
   "externalUrl":"https://mangaplus.shueisha.co.jp/viewer/1",
   "translatedLanguage":"en"},"relationships":[]}
],"total":4}`

const dexAtHomePage = `{"result":"ok","baseUrl":"https://mangadex.org/data",
 "chapter":{"hash":"abc123","data":["001.png","002.png","003.png"],"dataSaver":[]}}`

func TestDexSearchChaptersPagesHappyPath(t *testing.T) {
	s := dexFixtureService(dexRouter{
		"search":  dexSearchPage,
		"feed":    dexFeedPage,
		"at-home": dexAtHomePage,
	})
	ctx := context.Background()

	results, next, e := s.MangaSearch(ctx, "one piece", "", 24)
	if e != nil {
		t.Fatal(e)
	}
	if len(results) != 2 {
		t.Fatalf("results %d", len(results))
	}
	if next != dexID+":2" {
		t.Fatalf("next cursor %q, want %s:2 (total 32, page returned 2)", next, dexID)
	}
	first := results[0]
	if first.ID != dexManga || first.Title != "One Piece" || first.Type != "Manga" ||
		first.Year != 1997 || first.Status != "ongoing" ||
		len(first.AlternativeTitles) != 2 || first.AlternativeTitles[1] != "OP" ||
		first.Synopsis != "Pirate king." || len(first.Tags) != 1 || first.Tags[0] != "Adventure" {
		t.Fatalf("mismatched mapping: %+v", first)
	}
	if first.PosterURL != "" {
		t.Fatalf("poster set without cover_art relationship: %q", first.PosterURL)
	}
	// Korean-original entry maps to Manhwa and falls back to any title.
	if results[1].Type != "Manhwa" || results[1].Title != "원피스" {
		t.Fatalf("ko mapping: %+v", results[1])
	}

	chapters, e := s.MangaChapters(ctx, dexManga)
	if e != nil {
		t.Fatal(e)
	}
	// The zero-page release and the externally hosted (externalUrl) chapter are
	// dropped, not zeroed; the other four keep order 2 < 12 < 12.5 < oneshot.
	if len(chapters) != 4 {
		t.Fatalf("chapters %d, want 4 (unavailable/external dropped)", len(chapters))
	}
	for _, ch := range chapters {
		if ch.ID == "mangadex:66666666-6666-4666-8666-666666666666" {
			t.Fatalf("externalUrl chapter offered: %+v", ch)
		}
	}
	wantOrder := []string{dexCh2ID, dexCh3ID, dexCh1ID, dexCh4ID}
	for i, ch := range chapters {
		if ch.ID != wantOrder[i] {
			t.Fatalf("order[%d]=%s, want %s (%+v)", i, ch.ID, wantOrder[i], chapters)
		}
	}
	ch12_5 := chapters[2]
	if ch12_5.Number != "12.5" || ch12_5.Volume != "3" || ch12_5.Language != "en" ||
		ch12_5.Special != "" || ch12_5.Label != "Ch. 12.5" || ch12_5.PageCount != 4 ||
		ch12_5.Title != "Recap" || ch12_5.MangaID != dexManga {
		t.Fatalf("12.5 identity lost: %+v", ch12_5)
	}
	if chapters[0].GroupName != "Group A" {
		t.Fatalf("scanlation group lost: %+v", chapters[0])
	}
	oneshot := chapters[3]
	if oneshot.Number != "" || oneshot.Special != "oneshot" || oneshot.Label != "Oneshot" {
		t.Fatalf("oneshot identity lost: %+v", oneshot)
	}

	pages, e := s.MangaPages(ctx, dexCh1ID)
	if e != nil {
		t.Fatal(e)
	}
	if len(pages) != 3 {
		t.Fatalf("pages %d", len(pages))
	}
	for i, p := range pages {
		want := "https://mangadex.org/data/data/abc123/00" + string(rune('1'+i)) + ".png"
		if p.Index != i+1 || p.URL != want {
			t.Fatalf("page[%d] = (%d, %s), want (%d, %s)", i, p.Index, p.URL, i+1, want)
		}
	}
}

func TestDexSearchOffsetPagination(t *testing.T) {
	s := dexFixtureService(dexRouter{"search": dexSearchPage})
	// Offset 2 returns 2 of 32 total, so a further page is offered.
	_, next, e := s.MangaSearch(context.Background(), "x", dexID+":2", 24)
	if e != nil || next != dexID+":4" {
		t.Fatalf("offset pagination broken: next=%q err=%v", next, e)
	}
	// Offset 32 is past the end: no next token.
	_, next, e = s.MangaSearch(context.Background(), "x", dexID+":32", 24)
	if e != nil || next != "" {
		t.Fatalf("end cursor: next=%q err=%v", next, e)
	}
}

func TestDexSearchRejectsBadCursor(t *testing.T) {
	s := dexFixtureService(dexRouter{})
	for _, cursor := range []string{"abc", "-1", "10001"} {
		if _, _, e := s.MangaSearch(context.Background(), "x", cursor, 24); e == nil {
			t.Fatalf("accepted cursor %q", cursor)
		} else if pe, ok := e.(*Error); !ok || pe.Code != "invalid_cursor" {
			t.Fatalf("cursor %q: %v", cursor, e)
		}
	}
}

func TestDexDetailMapsCoverAndAuthor(t *testing.T) {
	const one = `{"result":"ok","data":{"id":"f9c33607-9180-4ba6-b85c-e4b5faee7192","type":"manga","attributes":{
	 "title":{"en":"One Piece"},"altTitles":[],"description":{},"originalLanguage":"ja",
	 "status":"ongoing","year":1997,"tags":[]},
	 "relationships":[
	  {"id":"f9c33607-9180-4ba6-b85c-e4b5faee7192","type":"cover_art","attributes":{"fileName":"cover.jpg"}},
	  {"id":"a1","type":"author","attributes":{"name":"Eiichiro Oda"}}]}}`
	s := dexFixtureService(dexRouter{"detail": one})
	d, e := s.MangaDetail(context.Background(), dexManga)
	if e != nil {
		t.Fatal(e)
	}
	if d.Title != "One Piece" || d.Author != "Eiichiro Oda" ||
		d.PosterURL != "https://mangadex.org/covers/f9c33607-9180-4ba6-b85c-e4b5faee7192/cover.jpg.512.jpg" {
		t.Fatalf("detail mapping: %+v", d)
	}
}

func TestDexRejectsInvalidIDs(t *testing.T) {
	s := dexFixtureService(dexRouter{})
	ctx := context.Background()
	for _, id := range []string{"", "mangadex:", "mangadex:zzz", "hianime:123", "mangadex:f9c33607-9180-4ba6-b85c-e4b5faee719"} {
		if _, e := s.MangaDetail(ctx, id); e == nil {
			t.Fatalf("detail accepted id %q", id)
		}
		if _, e := s.MangaChapters(ctx, id); e == nil {
			t.Fatalf("chapters accepted id %q", id)
		}
		if _, e := s.MangaPages(ctx, id); e == nil {
			t.Fatalf("pages accepted id %q", id)
		}
	}
}

func TestDexChapterSingleUsesMangaRelationship(t *testing.T) {
	const one = `{"result":"ok","data":{"id":"22222222-2222-4222-8222-222222222222","type":"chapter","attributes":{
	 "chapter":"2","volume":"1","title":"","pages":20,
	 "publishAt":"2024-01-01T00:00:00Z","translatedLanguage":"en","mangaId":"stale"},
	 "relationships":[{"id":"f9c33607-9180-4ba6-b85c-e4b5faee7192","type":"manga"}]}}`
	s := dexFixtureService(dexRouter{"chapter": one})
	ch, e := s.MangaChapter(context.Background(), dexCh2ID)
	if e != nil {
		t.Fatal(e)
	}
	if ch.MangaID != dexManga {
		t.Fatalf("mangaId %q, want relationship-derived id", ch.MangaID)
	}
	// An unavailable single chapter (no pages) is a hard error, not a zero value.
	const dead = `{"result":"ok","data":{"id":"99999999-9999-4999-8999-999999999999","type":"chapter","attributes":{
	 "chapter":"9","volume":"","title":"","pages":0,
	 "publishAt":"2024-01-01T00:00:00Z","translatedLanguage":"en"},"relationships":[]}}`
	s2 := dexFixtureService(dexRouter{"chapter": dead})
	if _, e := s2.MangaChapter(context.Background(), "mangadex:99999999-9999-4999-8999-999999999999"); e == nil {
		t.Fatal("accepted unavailable chapter")
	}
}

func TestDexMalformedResponses(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name, endpoint, body string
		call                 func(*Service) error
	}{
		{"search not json", "search", `<html>blocked</html>`,
			func(s *Service) error { _, _, e := s.MangaSearch(ctx, "x", "", 24); return e }},
		{"search envelope mismatch", "search", `{"data":"nope"}`,
			func(s *Service) error { _, _, e := s.MangaSearch(ctx, "x", "", 24); return e }},
		{"detail not json", "detail", `{`,
			func(s *Service) error { _, e := s.MangaDetail(ctx, dexManga); return e }},
		{"feed not json", "feed", `[]`,
			func(s *Service) error { _, e := s.MangaChapters(ctx, dexManga); return e }},
		{"at-home empty pages", "at-home", `{"result":"ok","baseUrl":"x","chapter":{"hash":"h","data":[],"dataSaver":[]}}`,
			func(s *Service) error { _, e := s.MangaPages(ctx, dexCh1ID); return e }},
		{"at-home not json", "at-home", ``,
			func(s *Service) error { _, e := s.MangaPages(ctx, dexCh1ID); return e }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := dexFixtureService(dexRouter{tc.endpoint: tc.body})
			if e := tc.call(s); e == nil {
				t.Fatal("accepted malformed response")
			}
		})
	}
}

func TestDexPagesFallBackToDataSaver(t *testing.T) {
	s := dexFixtureService(dexRouter{"at-home": `{"result":"ok","baseUrl":"https://h.example",
		"chapter":{"hash":"h1","data":[],"dataSaver":["a.webp","b.webp"]}}`})
	pages, e := s.MangaPages(context.Background(), dexCh1ID)
	if e != nil {
		t.Fatal(e)
	}
	if len(pages) != 2 || pages[0].URL != "https://h.example/data/h1/a.webp" {
		t.Fatalf("dataSaver fallback: %+v", pages)
	}
}
