package provider

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"io"
	"myanime/internal/domain"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func fixtureService(body string) *Service {
	s := New()
	s.client.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	return s
}
func TestCanonicalEpisodeLabels(t *testing.T) {
	for _, label := range []string{"1", "12.5", "SP 1", "OVA", "01"} {
		id := episodeID("anilist:1", label)
		got, e := episodeLabel("anilist:1", id)
		if e != nil || got != label {
			t.Fatalf("round trip %q: %q %v", label, got, e)
		}
		if _, e := episodeLabel("anilist:2", id); e == nil {
			t.Fatal("accepted another anime's episode")
		}
	}
}
func TestCBCPaddingAndMalformed(t *testing.T) {
	plain := []byte(`{"file":"https://cdn.example/master.m3u8"}`)
	padding := 16 - len(plain)%16
	for range padding {
		plain = append(plain, byte(padding))
	}
	block, _ := aes.NewCipher(megaKey)
	ct := make([]byte, len(plain))
	cipher.NewCBCEncrypter(block, megaIV).CryptBlocks(ct, plain)
	got, e := decryptCBC(base64.RawURLEncoding.EncodeToString(ct))
	if e != nil || !strings.Contains(string(got), "master.m3u8") {
		t.Fatalf("decrypt: %s %v", got, e)
	}
	for _, bad := range []string{"", "abc", "!", base64.StdEncoding.EncodeToString(make([]byte, 16))} {
		if _, e := decryptCBC(bad); e == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
}
func TestGCMAuthentication(t *testing.T) {
	key := make([]byte, 32)
	nonce := make([]byte, 12)
	block, _ := aes.NewCipher(key)
	g, _ := cipher.NewGCM(block)
	ct := g.Seal(nil, nonce, []byte(`{"ok":true}`), nil)
	if _, e := decryptGCM(key, nonce, ct); e != nil {
		t.Fatal(e)
	}
	ct[len(ct)-1] ^= 1
	if _, e := decryptGCM(key, nonce, ct); e == nil {
		t.Fatal("accepted invalid GCM tag")
	}
	if _, e := decryptGCM(key, nonce[:2], ct); e == nil {
		t.Fatal("accepted invalid nonce")
	}
}
func TestHiEpisodeHTMLPreservesLabels(t *testing.T) {
	s := fixtureService(`{"html":"<a data-id=\"10\" data-number=\"12.5\">Recap</a><a data-number=\"SP1\" data-id=\"11\">Special</a>"}`)
	eps, e := s.providerEpisodes(context.Background(), "hianime", "show-123")
	if e != nil || len(eps) != 2 {
		t.Fatalf("episodes %v %v", eps, e)
	}
	seen := map[string]bool{}
	for _, ep := range eps {
		seen[ep.label] = true
	}
	if !seen["12.5"] || !seen["SP1"] {
		t.Fatal(eps)
	}
}
func TestManifestRejectsHTMLAndEmpty(t *testing.T) {
	for _, body := range []string{"<html>ok</html>", "#EXTM3U\n", "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=5\n"} {
		s := fixtureService(body)
		if _, e := s.manifest(context.Background(), "https://cdn.example/master.m3u8", "", true); e == nil {
			t.Fatalf("accepted %q", body)
		}
	}
}
func TestManifestQualityFromActualPlaylist(t *testing.T) {
	s := fixtureService("#EXTM3U\n#EXTINF:5,\nsegment.ts\n#EXT-X-ENDLIST\n")
	v, e := s.manifest(context.Background(), "https://cdn.example/media.m3u8", "", true)
	if e != nil || len(v) != 1 || v[0].Height != 0 || v[0].Label != "Auto" {
		t.Fatalf("variants %v %v", v, e)
	}
}
func TestManualDubNeverFallsBack(t *testing.T) {
	s := New()
	a := domain.Anime{ID: "anilist:1", Title: "Test"}
	s.putCache("anime:"+a.ID, a, nil, 100000000000)
	s.client.Transport = transportFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("manual unsupported provider must not contact another provider")
		return nil, nil
	})
	_, e := s.Resolve(context.Background(), domain.ResolveRequest{AnimeID: a.ID, EpisodeID: episodeID(a.ID, "1"), Mode: "dub", Provider: "otakudesu"})
	if e == nil {
		t.Fatal("expected unavailable")
	}
}

func TestEpisodeTitlesFromSingleAniZipResponse(t *testing.T) {
	s := fixtureService(`{"episodes":{"1":{"title":{"en":"To You, 2000 Years","x-jat":"Nisen-nengo no Kimi e","ja":"二千年後の君へ"}},"12.5":{"title":{"x-jat":"Recap"}},"3":{"title":{"ja":"日本語"}},"sp":{"title":{"en":"Special"}}}}`)
	titles := s.episodeTitles(context.Background(), "anilist:5")
	if len(titles) != 3 || titles[1] != "To You, 2000 Years" || titles[12.5] != "Recap" || titles[3] != "日本語" {
		t.Fatalf("titles %v", titles)
	}
}

func TestEpisodeTitlesMissingMetadataIsNotFatal(t *testing.T) {
	s := New()
	s.client.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 404, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`"Not Found"`)), Request: r}, nil
	})
	if titles := s.episodeTitles(context.Background(), "anilist:5"); len(titles) != 0 {
		t.Fatalf("titles %v", titles)
	}
}

func TestMeaningfulTitleDropsFiller(t *testing.T) {
	for _, tc := range []struct{ title, label, want string }{
		{"401 Episode 401", "401", ""},
		{"Episode 401", "401", ""},
		{"401", "401", ""},
		{"OVA", "2", ""},
		{"401 Episode 401 Fall of the Straw Hat", "401", "401 Episode 401 Fall of the Straw Hat"},
		{"To You, 2000 Years", "1", "To You, 2000 Years"},
	} {
		if got := meaningfulTitle(tc.title, tc.label); got != tc.want {
			t.Fatalf("%q/%s: %q", tc.title, tc.label, got)
		}
	}
}

func TestEpisodesMergeTitlesOverProviderLabels(t *testing.T) {
	s := fixtureService(`{"episodes":{"1":{"title":{"en":"Locusts"}},"2":{"title":{"en":""}}}}`)
	const id = "anilist:5"
	s.putCache("anime:"+id, domain.Anime{ID: id, Title: "Test"}, nil, time.Hour)
	s.putCache("mapping:hianime:"+id, "test-5", nil, time.Hour)
	s.putCache("episodes:hianime:test-5", []providerEpisode{
		{label: "1", key: "a", title: "Episode 1", ordinal: 1},
		{label: "2", key: "b", title: "2 Episode 2", ordinal: 2},
	}, nil, time.Hour)
	eps, e := s.Episodes(context.Background(), id)
	if e != nil || len(eps) != 2 {
		t.Fatalf("episodes %v %v", eps, e)
	}
	if eps[0].Title != "Locusts" {
		t.Fatalf("metadata title not applied: %+v", eps[0])
	}
	if eps[1].Title != "" {
		t.Fatalf("provider filler kept: %+v", eps[1])
	}
}

// A stale request key still answers 200 with an empty server list, so the
// adapter must re-read the rotated key instead of reporting the title as
// unavailable.
func TestAniChanStaleKeyRefreshOnEmptyServers(t *testing.T) {
	oldKeys := [3]string{"00000000", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="}
	newKeys := [3]string{"deadbeef", "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8=", "ICEiIyQlJicoKSorLC0uLzAxMjM0NTY3ODk6Ozw9Pj8="}
	s := New()
	s.keys = oldKeys
	s.keysAt = time.Time{}
	s.putCache("episodes:anichan:113415", []providerEpisode{{label: "1", key: "1", title: "Episode 1", ordinal: 1}}, nil, time.Hour)
	expiry := float64(time.Now().Add(time.Hour).Unix())
	s.client.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		var body string
		switch {
		case r.URL.Path == "/api/watch/session":
			body = `{"ok":true,"n":"token","exp":` + strconv.FormatFloat(expiry, 'f', 0, 64) + `}`
		case r.URL.Path == "/api/watch/servers":
			body = `{"servers":[]}`
		case r.URL.Path == "/":
			body = `<script src="/_next/static/chunks/watch.js"></script>`
		case r.URL.Path == "/_next/static/chunks/watch.js":
			body = `a="/api/watch/servers";O="` + newKeys[0] + `",k="` + newKeys[1] + `",G="` + newKeys[2] + `"`
		default:
			body = `{}`
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	_, err := s.aniResolve(context.Background(), "113415", "1", "sub")
	if err == nil {
		t.Fatal("expected unavailable for an empty server list")
	}
	if s.aniKeys()[0] != newKeys[0] {
		t.Fatalf("stale key was not refreshed: %v", s.aniKeys())
	}
}

func TestMatchCandidatePrefersExactOverQualifiedCard(t *testing.T) {
	a := domain.Anime{ID: "anilist:113415", Title: "Jujutsu Kaisen"}
	found := []candidate{
		{slug: "jujutsu-kaisen-237", title: "Jujutsu Kaisen (TV)"},
		{slug: "jujutsu-kaisen-season-2-415", title: "Jujutsu Kaisen 2nd Season"},
		{slug: "jujutsu-kaisen-0-413", title: "Jujutsu Kaisen 0 Movie"},
	}
	slug, ok := matchCandidate(found, a)
	if !ok || slug != "jujutsu-kaisen-237" {
		t.Fatalf("got %q %v", slug, ok)
	}
}

func TestMatchCandidateRefusesAmbiguity(t *testing.T) {
	a := domain.Anime{ID: "anilist:1", Title: "Jujutsu Kaisen"}
	found := []candidate{
		{slug: "a", title: "Jujutsu Kaisen"},
		{slug: "b", title: "Jujutsu Kaisen"},
	}
	if slug, ok := matchCandidate(found, a); ok {
		t.Fatalf("ambiguous cards resolved to %q", slug)
	}
}

func TestMatchCandidateUsesAlternativeTitles(t *testing.T) {
	a := domain.Anime{ID: "anilist:16498", Title: "Shingeki no Kyojin", AlternativeTitles: []string{"Attack on Titan"}}
	found := []candidate{
		{slug: "attack-on-titan-240", title: "Attack on Titan"},
		{slug: "attack-on-titan-season-2-865", title: "Attack on Titan Season 2"},
	}
	if slug, ok := matchCandidate(found, a); !ok || slug != "attack-on-titan-240" {
		t.Fatalf("got %q %v", slug, ok)
	}
}

func TestParseCandidatesPerProvider(t *testing.T) {
	hi := parseCandidates("hianime", []byte(`<h3 class="film-name"><a href="https://hianime.at/jujutsu-kaisen-237" title="Jujutsu Kaisen">Jujutsu Kaisen</a></h3><a href="https://hianime.at/no-id" title="No id">No id</a>`))
	if len(hi) != 1 || hi[0].slug != "jujutsu-kaisen-237" || hi[0].title != "Jujutsu Kaisen" {
		t.Fatalf("hianime %v", hi)
	}
	ot := parseCandidates("otakudesu", []byte(`<a class="ot-k" href="https://ww2.otakudesu.biz/anime/jujutsu-kaisen-tv-6i3h8t6/"><h2 class="ot-k-j">Jujutsu Kaisen (TV)</h2></a>`))
	if len(ot) != 1 || ot[0].slug != "jujutsu-kaisen-tv-6i3h8t6" || ot[0].title != "Jujutsu Kaisen (TV)" {
		t.Fatalf("otakudesu %v", ot)
	}
}
