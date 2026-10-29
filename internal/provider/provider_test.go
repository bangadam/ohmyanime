package provider

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"errors"
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
	hi, ok := s.reg.lookupAnime("hianime")
	if !ok {
		t.Fatal("hianime not registered")
	}
	eps, e := hi.Episodes(context.Background(), "show-123")
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
	for _, rules := range []matchRules{hianimeRulesForTest, otakudesuRulesForTest} {
		slug, ok := matchCandidate(found, a, rules)
		if !ok || slug != "jujutsu-kaisen-237" {
			t.Fatalf("got %q %v", slug, ok)
		}
	}
}

func TestMatchCandidateRefusesAmbiguity(t *testing.T) {
	a := domain.Anime{ID: "anilist:1", Title: "Jujutsu Kaisen"}
	found := []candidate{
		{slug: "a", title: "Jujutsu Kaisen"},
		{slug: "b", title: "Jujutsu Kaisen"},
	}
	for _, rules := range []matchRules{hianimeRulesForTest, otakudesuRulesForTest} {
		if slug, ok := matchCandidate(found, a, rules); ok {
			t.Fatalf("ambiguous cards resolved to %q", slug)
		}
	}
}

func TestMatchCandidateUsesAlternativeTitles(t *testing.T) {
	a := domain.Anime{ID: "anilist:16498", Title: "Shingeki no Kyojin", AlternativeTitles: []string{"Attack on Titan"}}
	found := []candidate{
		{slug: "attack-on-titan-240", title: "Attack on Titan"},
		{slug: "attack-on-titan-season-2-865", title: "Attack on Titan Season 2"},
	}
	if slug, ok := matchCandidate(found, a, hianimeRulesForTest); !ok || slug != "attack-on-titan-240" {
		t.Fatalf("got %q %v", slug, ok)
	}
}

// Season two must never map to season one, in either direction: the season
// number is part of the work's identity, not a provider qualifier.
func TestSeasonTwoNeverMapsToSeasonOne(t *testing.T) {
	season1 := domain.Anime{ID: "anilist:101", Title: "Jujutsu Kaisen"}
	season2 := domain.Anime{ID: "anilist:102", Title: "Jujutsu Kaisen 2nd Season"}
	season2Alt := domain.Anime{ID: "anilist:103", Title: "Jujutsu Kaisen", AlternativeTitles: []string{"Jujutsu Kaisen Season 2"}}
	for _, rules := range []matchRules{hianimeRulesForTest, otakudesuRulesForTest} {
		if _, ok := matchCandidate([]candidate{{slug: "s1", title: "Jujutsu Kaisen"}}, season2, rules); ok {
			t.Fatal("season 2 anime mapped to a season 1 card")
		}
		if _, ok := matchCandidate([]candidate{{slug: "s1", title: "Jujutsu Kaisen (TV)"}}, season2Alt, rules); ok {
			t.Fatal("season 2 alias mapped to a season 1 card")
		}
		if _, ok := matchCandidate([]candidate{{slug: "s2", title: "Jujutsu Kaisen 2nd Season"}}, season1, rules); ok {
			t.Fatal("season 1 anime mapped to a season 2 card")
		}
		// The same season still resolves to its own card.
		if slug, ok := matchCandidate([]candidate{{slug: "s2", title: "Jujutsu Kaisen 2nd Season"}}, season2, rules); !ok || slug != "s2" {
			t.Fatalf("season 2 self-match: %q %v", slug, ok)
		}
	}
	// Numeric season wording is identity too.
	if _, ok := matchCandidate([]candidate{{slug: "s1", title: "Jujutsu Kaisen"}}, domain.Anime{ID: "anilist:104", Title: "Jujutsu Kaisen 2"}, hianimeRulesForTest); ok {
		t.Fatal("numeric season 2 mapped to season 1")
	}
}

// Each adapter owns its qualifier list: a suffix Otakudesu treats as noise
// stays meaningful under HiAnime's rules.
func TestMatchRulesArePerAdapter(t *testing.T) {
	a := domain.Anime{ID: "anilist:1", Title: "Jujutsu Kaisen"}
	// "BD" is an Otakudesu release variant; HiAnime cards never carry it as
	// a non-identity suffix, so HiAnime must not strip it.
	bd := []candidate{{slug: "bd", title: "Jujutsu Kaisen BD"}}
	if _, ok := matchCandidate(bd, a, hianimeRulesForTest); ok {
		t.Fatal("hianime stripped a suffix only otakudesu knows")
	}
	if slug, ok := matchCandidate(bd, a, otakudesuRulesForTest); !ok || slug != "bd" {
		t.Fatalf("otakudesu BD card: %q %v", slug, ok)
	}
	// Both providers mark TV cards "(TV)".
	tv := []candidate{{slug: "tv", title: "Jujutsu Kaisen (TV)"}}
	for _, rules := range []matchRules{hianimeRulesForTest, otakudesuRulesForTest} {
		if slug, ok := matchCandidate(tv, a, rules); !ok || slug != "tv" {
			t.Fatalf("TV card with %v: %q %v", rules, slug, ok)
		}
	}
}

// Decimal and special episode labels are distinct identities: 12.5 never
// resolves to 12, and duplicate labels stay unresolved.
func TestDecimalAndSpecialEpisodeIdentity(t *testing.T) {
	eps := parseEpisodes([]byte(`{"html":"<a data-id=\"12\" data-number=\"12\">Ep 12</a><a data-id=\"125\" data-number=\"12.5\">Recap</a><a data-id=\"ova\" data-number=\"OVA\">OVA</a>"}`), "", hiEpisodeCard)
	if ep, ok := matchEpisode(eps, "12.5"); !ok || ep.key != "125" {
		t.Fatalf("12.5 resolved to %q ok=%v", ep.key, ok)
	}
	if ep, ok := matchEpisode(eps, "12"); !ok || ep.key != "12" {
		t.Fatalf("12 resolved to %q ok=%v", ep.key, ok)
	}
	if ep, ok := matchEpisode(eps, "OVA"); !ok || ep.key != "ova" {
		t.Fatalf("OVA resolved to %q ok=%v", ep.key, ok)
	}
	if _, ok := matchEpisode(eps, "13"); ok {
		t.Fatal("resolved a label the provider does not have")
	}
	// Two cards claiming the same label are ambiguous: never guess.
	dup := append(eps, providerEpisode{label: "12.5", key: "dup"})
	if _, ok := matchEpisode(dup, "12.5"); ok {
		t.Fatal("duplicate 12.5 label resolved")
	}
}

var (
	hianimeRulesForTest   = newHianime(New()).rules
	otakudesuRulesForTest = newOtakudesu(New()).rules
)

func TestParseCandidatesPerProvider(t *testing.T) {
	hi := parseHiCandidates([]byte(`<h3 class="film-name"><a href="https://hianime.at/jujutsu-kaisen-237" title="Jujutsu Kaisen">Jujutsu Kaisen</a></h3><a href="https://hianime.at/no-id" title="No id">No id</a>`))
	if len(hi) != 1 || hi[0].slug != "jujutsu-kaisen-237" || hi[0].title != "Jujutsu Kaisen" {
		t.Fatalf("hianime %v", hi)
	}
	ot := parseOtakuCandidates([]byte(`<a class="ot-k" href="https://ww2.otakudesu.biz/anime/jujutsu-kaisen-tv-6i3h8t6/"><h2 class="ot-k-j">Jujutsu Kaisen (TV)</h2></a>`))
	if len(ot) != 1 || ot[0].slug != "jujutsu-kaisen-tv-6i3h8t6" || ot[0].title != "Jujutsu Kaisen (TV)" {
		t.Fatalf("otakudesu %v", ot)
	}
}

// An explicit provider that simply lacks the episode is actionable: the
// upstream answered, so the caller must see no_source rather than a
// retryable failure. Everything needed is cached, so any transport call is a
// bug.
func TestResolveTitleAbsenceIsNoSource(t *testing.T) {
	s := New()
	const id = "anilist:5"
	s.putCache("anime:"+id, domain.Anime{ID: id, Title: "Test"}, nil, time.Hour)
	s.putCache("mapping:hianime:"+id, "test-5", nil, time.Hour)
	s.putCache("episodes:hianime:test-5", []providerEpisode{{label: "1", key: "a", title: "Episode 1", ordinal: 1}}, nil, time.Hour)
	s.client.Transport = transportFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("title absence must not contact the network")
		return nil, nil
	})
	_, e := s.Resolve(context.Background(), domain.ResolveRequest{AnimeID: id, EpisodeID: episodeID(id, "9"), Mode: "sub", Provider: "hianime"})
	var pe *Error
	if !errors.As(e, &pe) || pe.Code != "no_source" {
		t.Fatalf("resolve error %v", e)
	}
}
