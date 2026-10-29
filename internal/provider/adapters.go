package provider

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"golang.org/x/net/html"
	"myanime/internal/domain"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
)

const hiBase = "https://hianime.at"
const otakuBase = "https://ww2.otakudesu.biz"
const aniBase = "https://anichan.to"

type candidate struct{ slug, title, typ string }
type providerEpisode struct {
	label, title, key string
	ordinal           float64
}

func nodes(raw []byte, visit func(*html.Node)) {
	root, e := html.Parse(strings.NewReader(string(raw)))
	if e != nil {
		return
	}
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		visit(n)
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
}
func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}
func class(n *html.Node, c string) bool {
	for _, v := range strings.Fields(attr(n, "class")) {
		if v == c {
			return true
		}
	}
	return false
}
func text(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(x *html.Node) {
		if x.Type == html.TextNode {
			b.WriteString(x.Data)
		}
		for c := x.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return strings.Join(strings.Fields(b.String()), " ")
}
func plain(raw string) string {
	var b strings.Builder
	nodes([]byte(raw), func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
			b.WriteByte(' ')
		}
	})
	return strings.Join(strings.Fields(b.String()), " ")
}
func normalize(t string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, t)
}
func jsonHTML(b []byte) []byte {
	var v struct{ HTML string }
	if json.Unmarshal(b, &v) == nil && v.HTML != "" {
		return []byte(v.HTML)
	}
	return b
}

// matchEpisode returns the single episode whose label matches exactly. Zero or
// several cards with the same label stay unresolved: the adapter never guesses.
func matchEpisode(eps []providerEpisode, label string) (providerEpisode, bool) {
	var match providerEpisode
	n := 0
	for _, ep := range eps {
		if ep.label == label {
			match = ep
			n++
		}
	}
	return match, n == 1
}

// htmlCatalog maps AniList IDs to provider slugs by searching a provider's
// HTML catalogue. Adapters that discover slugs this way embed it.
type htmlCatalog struct {
	s      *Service
	id     string
	target func(query string) string
	cards  func(b []byte) []candidate
	// rules are this provider's own title-matching semantics: which
	// non-identity qualifiers its cards append, and whether it suffixes
	// re-releases with a year. Season numbers and formats are never
	// qualifiers; they identify a different work.
	rules matchRules
}

func (c htmlCatalog) Map(ctx context.Context, a domain.Anime) (string, error) {
	key := "mapping:" + c.id + ":" + a.ID
	if v, ok := c.s.getCache(key); ok {
		return str(v.value), v.err
	}
	found, searchErr := c.search(ctx, searchQueries(a)...)
	if len(found) == 0 {
		if searchErr != nil {
			return "", searchErr
		}
		return "", failure("invalid_mapping", c.id)
	}
	slug, ok := matchCandidate(found, a, c.rules)
	if !ok {
		return "", failure("invalid_mapping", c.id)
	}
	c.s.putCache(key, slug, nil, time.Hour)
	return slug, nil
}

// search queries a provider search page and parses its result cards.
// Query order matters: the first query that yields cards wins.
func (c htmlCatalog) search(ctx context.Context, queries ...string) ([]candidate, error) {
	var last error
	for _, query := range queries {
		b, e := c.s.get(ctx, c.target(query), "")
		if e != nil {
			last = e
			continue
		}
		if found := c.cards(b); len(found) > 0 {
			return found, nil
		}
	}
	return nil, last
}

func parseHiCandidates(b []byte) []candidate {
	found := []candidate{}
	nodes(b, func(n *html.Node) {
		if n.Data != "a" {
			return
		}
		href := attr(n, "href")
		title := attr(n, "title")
		if n.Parent == nil || !class(n.Parent, "film-name") {
			return
		}
		if title == "" {
			title = text(n)
		}
		u, _ := url.Parse(href)
		if u == nil {
			return
		}
		slug := strings.Trim(strings.TrimPrefix(strings.Trim(u.Path, "/"), "watch/"), "/")
		if slug == "" || strings.Contains(slug, "/") {
			return
		}
		// The numeric suffix is the provider's stable episode-list key.
		if _, e := strconv.Atoi(slug[strings.LastIndex(slug, "-")+1:]); e != nil {
			return
		}
		found = append(found, candidate{slug: slug, title: title})
	})
	return found
}

func parseOtakuCandidates(b []byte) []candidate {
	found := []candidate{}
	nodes(b, func(n *html.Node) {
		if n.Data != "a" || !class(n, "ot-k") {
			return
		}
		href := attr(n, "href")
		title := ""
		var walk func(*html.Node)
		walk = func(c *html.Node) {
			if c.Data == "h2" {
				title = text(c)
			}
			for x := c.FirstChild; x != nil; x = x.NextSibling {
				walk(x)
			}
		}
		walk(n)
		u, _ := url.Parse(href)
		if u != nil && strings.HasPrefix(u.Path, "/anime/") {
			found = append(found, candidate{slug: strings.Trim(strings.TrimPrefix(u.Path, "/anime/"), "/"), title: strings.TrimSpace(strings.TrimSuffix(title, "Subtitle Indonesia"))})
		}
	})
	return found
}

// searchQueries tries the catalogue title first, then Latin-script aliases that
// providers are likely to index. Native-script aliases rarely match their search.
func searchQueries(a domain.Anime) []string {
	out := []string{a.Title}
	for _, alias := range a.AlternativeTitles {
		if len(out) >= 3 {
			break
		}
		if !latinTitle(alias) || strings.EqualFold(alias, a.Title) {
			continue
		}
		out = append(out, alias)
	}
	return out
}

func latinTitle(v string) bool {
	letters := 0
	for _, r := range v {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
			letters++
		case r > unicode.MaxASCII:
			return false
		}
	}
	return letters > 1
}

// matchRules holds one adapter's title-matching semantics. Only the stripping
// mechanics are shared; each adapter owns the qualifier list it strips, so a
// suffix one provider treats as noise stays meaningful on another.
type matchRules struct {
	// qualifiers are normalized suffixes this provider appends to cards
	// without changing which work the card identifies.
	qualifiers []string
	// stripYear drops a trailing 19xx/20xx year this provider appends to
	// re-releases of the same work.
	stripYear bool
}

// core removes this provider's presentation-only suffixes from an already
// normalized title. Season numbers and formats stay: they identify a
// different work, so season two never collapses onto season one.
func (r matchRules) core(name string) string {
	for _, suffix := range r.qualifiers {
		if strings.HasSuffix(name, suffix) && len(name) > len(suffix)+3 {
			name = strings.TrimSuffix(name, suffix)
		}
	}
	if r.stripYear {
		if year := trailingYear.FindString(name); year != "" && len(name) > len(year)+3 {
			name = strings.TrimSuffix(name, year)
		}
	}
	return name
}

// matchCandidate resolves a provider card to exactly one slug. Exact normalized
// equality wins; otherwise a card may differ only by a qualifier this
// provider's rules treat as non-identity. Two equally good cards stay
// ambiguous: the adapter never guesses between different works.
func matchCandidate(found []candidate, a domain.Anime, rules matchRules) (string, bool) {
	aliases := matchAliases(a)
	exact := map[string]bool{}
	loose := map[string]bool{}
	for _, c := range found {
		name := normalize(c.title)
		if name == "" {
			continue
		}
		for _, alias := range aliases {
			key := normalize(alias)
			if key == "" {
				continue
			}
			if name == key {
				exact[c.slug] = true
			}
			if rules.core(name) == rules.core(key) {
				loose[c.slug] = true
			}
		}
	}
	if len(exact) == 1 {
		for slug := range exact {
			return slug, true
		}
	}
	if len(exact) > 1 {
		return "", false
	}
	if len(loose) == 1 {
		for slug := range loose {
			return slug, true
		}
	}
	return "", false
}

var (
	seasonSuffix  = regexp.MustCompile(`(?:season|part)s?([0-9]+)$`)
	ordinalSeason = regexp.MustCompile(`([0-9]+)(?:st|nd|rd|th)season$`)
)

// seasonMark reports the trailing season number a normalized title declares,
// or 0 when the title carries none. The mark is identity, not presentation:
// "Season 2" names a different work than the unmarked title.
func seasonMark(name string) int {
	if m := ordinalSeason.FindStringSubmatch(name); m != nil {
		n, _ := strconv.Atoi(m[1])
		return n
	}
	if m := seasonSuffix.FindStringSubmatch(name); m != nil {
		n, _ := strconv.Atoi(m[1])
		return n
	}
	return 0
}

// matchAliases returns the titles a work may be matched under. When the work
// itself declares a season beyond the first, unmarked titles are dropped:
// providers list every season under the same plain title, so an unmarked
// alias of a later season names season one's card, not this work's.
func matchAliases(a domain.Anime) []string {
	aliases := append([]string{a.Title}, a.AlternativeTitles...)
	mark := 0
	for _, alias := range aliases {
		if n := seasonMark(normalize(alias)); n > mark {
			mark = n
		}
	}
	if mark <= 1 {
		return aliases
	}
	kept := make([]string, 0, len(aliases))
	for _, alias := range aliases {
		if seasonMark(normalize(alias)) == mark {
			kept = append(kept, alias)
		}
	}
	return kept
}

var trailingYear = regexp.MustCompile(`(?:19|20)[0-9]{2}$`)

// meaningfulTitle drops anchor text that carries no title at all, such as
// "Episode 5" or HiAnime's concatenated "401 Episode 401", so the interface
// shows the label once instead of repeating the number.
func meaningfulTitle(title, label string) string {
	t := strings.TrimSpace(title)
	if t == "" || t == label {
		return ""
	}
	for _, word := range strings.Fields(strings.ToLower(t)) {
		switch strings.Trim(word, "#.:-–—|()[]") {
		case "", "episode", "episodes", "ep", "eps", "ova", "specials":
			continue
		}
		if _, e := strconv.ParseFloat(strings.Trim(word, "#.:-–—|()[]"), 64); e == nil {
			continue
		}
		return t
	}
	return ""
}

var numberLabel = regexp.MustCompile(`(?i)(?:episode|eps|ep)\s*#?\s*([0-9]+(?:\.[0-9]+)?)`)

// episodeCard extracts one episode link from an anchor node.
type episodeCard func(n *html.Node, target string) (label, key, title string)

// parseEpisodes parses an episode list page, deduplicating by link key and
// sorting by parsed ordinal.
func parseEpisodes(b []byte, target string, card episodeCard) []providerEpisode {
	out := []providerEpisode{}
	seen := map[string]bool{}
	nodes(jsonHTML(b), func(n *html.Node) {
		if n.Data != "a" {
			return
		}
		label, key, title := card(n, target)
		if label == "" || key == "" || seen[key] {
			return
		}
		seen[key] = true
		ordinal, _ := strconv.ParseFloat(label, 64)
		out = append(out, providerEpisode{label: label, key: key, title: title, ordinal: ordinal})
	})
	sort.SliceStable(out, func(i, j int) bool { return out[i].ordinal < out[j].ordinal })
	return out
}

func hiEpisodeCard(n *html.Node, _ string) (string, string, string) {
	return attr(n, "data-number"), attr(n, "data-id"), text(n)
}

func otakuEpisodeCard(n *html.Node, target string) (string, string, string) {
	if !class(n, "ot-ep") {
		return "", "", ""
	}
	title := text(n)
	key := absolute(target, attr(n, "href"))
	if m := numberLabel.FindStringSubmatch(title); len(m) > 1 {
		return m[1], key, title
	}
	return title, key, title
}

// episodesCached wraps an adapter episode fetch with the shared inventory
// cache. Only successful lists are cached, for ten minutes.
func (s *Service) episodesCached(ctx context.Context, id, slug string, fetch func(context.Context) ([]providerEpisode, error)) ([]providerEpisode, error) {
	key := "episodes:" + id + ":" + slug
	if c, ok := s.getCache(key); ok {
		return c.value.([]providerEpisode), c.err
	}
	out, e := fetch(ctx)
	if e != nil {
		return nil, e
	}
	s.putCache(key, out, nil, 10*time.Minute)
	return out, nil
}

// hianimeAdapter resolves streams from HiAnime's themed API.
type hianimeAdapter struct{ htmlCatalog }

func newHianime(s *Service) *hianimeAdapter {
	return &hianimeAdapter{htmlCatalog{
		s:      s,
		id:     "hianime",
		target: func(query string) string { return hiBase + "/search?keyword=" + url.QueryEscape(query) },
		cards:  parseHiCandidates,
		// HiAnime labels its TV cards "(TV)" and marks dub/sub variants in
		// the card title.
		rules: matchRules{qualifiers: []string{"tv", "dub", "sub"}, stripYear: true},
	}}
}

func (a *hianimeAdapter) Identity() Identity {
	return Identity{ID: "hianime", Name: "HiAnime", Kind: KindAnime}
}
func (a *hianimeAdapter) Modes() []string  { return []string{"sub", "dub"} }
func (a *hianimeAdapter) HomePage() string { return hiBase }
func (a *hianimeAdapter) Episodes(ctx context.Context, slug string) ([]providerEpisode, error) {
	return a.s.episodesCached(ctx, a.id, slug, func(ctx context.Context) ([]providerEpisode, error) {
		parts := strings.Split(slug, "-")
		n := parts[len(parts)-1]
		if _, e := strconv.Atoi(n); e != nil {
			return nil, failure("invalid_mapping", a.id)
		}
		b, e := a.s.get(ctx, hiBase+"/api/theme/episode/list/"+n, "")
		if e != nil {
			return nil, e
		}
		out := parseEpisodes(b, "", hiEpisodeCard)
		if len(out) == 0 {
			return nil, failure("unavailable", a.id)
		}
		return out, nil
	})
}
func (a *hianimeAdapter) Resolve(ctx context.Context, slug, label, mode string) (domain.Source, error) {
	eps, e := a.Episodes(ctx, slug)
	if e != nil {
		return domain.Source{}, e
	}
	ep, ok := matchEpisode(eps, label)
	if !ok {
		// Episode absence at this provider, distinct from a timeout or outage:
		// retrying the same source cannot help, choosing another provider can.
		return domain.Source{}, failure("no_source", a.id)
	}
	return a.s.hiResolve(ctx, ep.key, mode)
}

// otakudesuAdapter resolves hard-subbed Indonesian streams from Otakudesu.
type otakudesuAdapter struct{ htmlCatalog }

func newOtakudesu(s *Service) *otakudesuAdapter {
	return &otakudesuAdapter{htmlCatalog{
		s:      s,
		id:     "otakudesu",
		target: func(query string) string { return otakuBase + "/?post_type=anime&s=" + url.QueryEscape(query) },
		cards:  parseOtakuCandidates,
		// Otakudesu suffixes release variants ("BD", "Uncensored") and
		// Indonesian localization markers; parseOtakuCandidates already
		// trims the visible "Subtitle Indonesia" text.
		rules: matchRules{qualifiers: []string{"subtitleindonesia", "subindo", "uncensored", "tv", "dub", "sub", "bd"}, stripYear: true},
	}}
}

func (a *otakudesuAdapter) Identity() Identity {
	return Identity{ID: "otakudesu", Name: "Otakudesu", Kind: KindAnime}
}
func (a *otakudesuAdapter) Modes() []string  { return []string{"sub"} }
func (a *otakudesuAdapter) HomePage() string { return otakuBase }
func (a *otakudesuAdapter) Episodes(ctx context.Context, slug string) ([]providerEpisode, error) {
	return a.s.episodesCached(ctx, a.id, slug, func(ctx context.Context) ([]providerEpisode, error) {
		target := otakuBase + "/anime/" + url.PathEscape(slug) + "/"
		b, e := a.s.get(ctx, target, "")
		if e != nil {
			return nil, e
		}
		out := parseEpisodes(b, target, otakuEpisodeCard)
		if len(out) == 0 {
			return nil, failure("unavailable", a.id)
		}
		return out, nil
	})
}
func (a *otakudesuAdapter) Resolve(ctx context.Context, slug, label, mode string) (domain.Source, error) {
	eps, e := a.Episodes(ctx, slug)
	if e != nil {
		return domain.Source{}, e
	}
	ep, ok := matchEpisode(eps, label)
	if !ok {
		// Episode absence at this provider, distinct from a timeout or outage:
		// retrying the same source cannot help, choosing another provider can.
		return domain.Source{}, failure("no_source", a.id)
	}
	src, e := a.s.otakuResolve(ctx, ep.key)
	if e != nil {
		return domain.Source{}, e
	}
	src.Hardsub = true
	src.HardsubLanguage = "id"
	return src, nil
}

// anichanAdapter resolves streams from AniChan's encrypted watch API. AniList
// IDs are AniChan's own work identifiers, so mapping is the identity itself.
type anichanAdapter struct{ s *Service }

func newAnichan(s *Service) *anichanAdapter { return &anichanAdapter{s: s} }

func (a *anichanAdapter) Identity() Identity {
	return Identity{ID: "anichan", Name: "AniChan", Kind: KindAnime}
}
func (a *anichanAdapter) Modes() []string  { return []string{"sub", "dub"} }
func (a *anichanAdapter) HomePage() string { return aniBase }
func (a *anichanAdapter) Map(ctx context.Context, anime domain.Anime) (string, error) {
	n, e := animeNumber(anime.ID)
	return strconv.Itoa(n), e
}
func (a *anichanAdapter) Episodes(ctx context.Context, slug string) ([]providerEpisode, error) {
	return a.s.aniEpisodes(ctx, slug)
}
func (a *anichanAdapter) Resolve(ctx context.Context, slug, label, mode string) (domain.Source, error) {
	return a.s.aniResolve(ctx, slug, label, mode)
}

func (s *Service) Episodes(ctx context.Context, id string) ([]domain.Episode, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	a, e := s.Detail(ctx, id)
	if e != nil {
		return nil, e
	}
	out := []domain.Episode{}
	index := map[string]int{}
	last := failure("unavailable", "catalog")
	// Independent of provider discovery, so it runs alongside it; the buffered
	// channel keeps the goroutine from blocking if the list ends up empty.
	titles := make(chan map[float64]string, 1)
	go func() { titles <- s.episodeTitles(ctx, id) }()
	for _, p := range s.animeProviders() {
		pid := p.Identity().ID
		if !s.isEnabled(pid) {
			continue
		}
		slug, e := p.Map(ctx, a)
		if e != nil {
			last = e
			continue
		}
		eps, e := p.Episodes(ctx, slug)
		if e != nil {
			last = e
			continue
		}
		for _, ep := range eps {
			if i, ok := index[ep.label]; ok {
				out[i].Availability = append(out[i].Availability, pid)
			} else {
				index[ep.label] = len(out)
				out = append(out, domain.Episode{ID: episodeID(id, ep.label), AnimeID: id, Label: ep.label, Title: meaningfulTitle(ep.title, ep.label), Ordinal: ep.ordinal, Availability: []string{pid}})
			}
		}
	}
	named := <-titles
	for i := range out {
		if out[i].Ordinal <= 0 {
			continue
		}
		if title, ok := named[out[i].Ordinal]; ok {
			out[i].Title = title
		} else {
			out[i].Title = meaningfulTitle(out[i].Title, out[i].Label)
		}
	}
	if len(out) == 0 {
		return out, last
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Ordinal < out[j].Ordinal })
	return out, nil
}
func (s *Service) Resolve(ctx context.Context, r domain.ResolveRequest) (domain.Source, error) {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	if r.Mode != "sub" && r.Mode != "dub" {
		return domain.Source{}, failure("invalid_mode", "catalog")
	}
	label, e := episodeLabel(r.AnimeID, r.EpisodeID)
	if e != nil {
		return domain.Source{}, e
	}
	a, e := s.Detail(ctx, r.AnimeID)
	if e != nil {
		return domain.Source{}, e
	}
	if r.Provider == "" {
		r.Provider = "auto"
	}
	providers := s.animeProviders()
	valid := r.Provider == "auto"
	for _, p := range providers {
		valid = valid || p.Identity().ID == r.Provider
	}
	if !valid {
		return domain.Source{}, failure("invalid_provider", r.Provider)
	}
	var last error = failure("unavailable", r.Provider)
	for _, p := range providers {
		pid := p.Identity().ID
		// An explicit selection never falls back to another provider.
		if r.Provider != "auto" && r.Provider != pid {
			continue
		}
		if !s.isEnabled(pid) || !supportsMode(p, r.Mode) {
			last = failure("unavailable", pid)
			continue
		}
		key := "resolve:" + pid + ":" + r.EpisodeID + ":" + r.Mode
		if c, ok := s.getCache(key); ok {
			if c.err == nil {
				return c.value.(domain.Source), nil
			}
			last = c.err
			continue
		}
		slug, e := p.Map(ctx, a)
		var src domain.Source
		if e == nil {
			src, e = p.Resolve(ctx, slug, label, r.Mode)
		}
		if e == nil {
			src.Provider = pid
			src.Mode = r.Mode
			s.enrichSkip(ctx, r.AnimeID, label, &src)
			s.putCache(key, src, nil, 2*time.Minute)
			return src, nil
		}
		last = e
		s.putCache(key, nil, e, 20*time.Second)
		if ctx.Err() != nil {
			return domain.Source{}, ctx.Err()
		}
	}
	return domain.Source{}, last
}

func supportsMode(p AnimeProvider, mode string) bool {
	// Fail closed: a mode the provider never declares is never offered, even
	// though a provider-level capability never promises any specific title.
	return animeCapabilities(p).supportsAudioMode(mode)
}
func (s *Service) hiResolve(ctx context.Context, ep, mode string) (domain.Source, error) {
	b, e := s.get(ctx, hiBase+"/api/theme/episode/servers?episodeId="+url.QueryEscape(ep), hiBase+"/")
	if e != nil {
		return domain.Source{}, e
	}
	var embeds []string
	nodes(jsonHTML(b), func(n *html.Node) {
		if attr(n, "data-type") != mode {
			return
		}
		raw, e := base64.StdEncoding.DecodeString(attr(n, "data-hash"))
		if e == nil && absolute(hiBase, string(raw)) == string(raw) {
			embeds = append(embeds, string(raw))
		}
	})
	sort.SliceStable(embeds, func(i, j int) bool {
		return strings.Contains(embeds[i], "megaplay") && !strings.Contains(embeds[j], "megaplay")
	})
	last := failure("unavailable", "hianime")
	for _, embed := range embeds {
		src, e := s.embed(ctx, embed)
		if e == nil {
			return src, nil
		}
		last = e
	}
	return domain.Source{}, last
}
