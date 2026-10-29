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
func (s *Service) mapping(ctx context.Context, a domain.Anime, p string) (string, error) {
	if p == "anichan" {
		n, e := animeNumber(a.ID)
		return strconv.Itoa(n), e
	}
	key := "mapping:" + p + ":" + a.ID
	if c, ok := s.getCache(key); ok {
		return str(c.value), c.err
	}
	found, searchErr := s.searchProvider(ctx, p, searchQueries(a)...)
	if len(found) == 0 {
		if searchErr != nil {
			return "", searchErr
		}
		return "", failure("invalid_mapping", p)
	}
	slug, ok := matchCandidate(found, a)
	if !ok {
		return "", failure("invalid_mapping", p)
	}
	s.putCache(key, slug, nil, time.Hour)
	return slug, nil
}

// searchProvider queries a provider search page and parses its result cards.
// Query order matters: the first query that yields cards wins.
func (s *Service) searchProvider(ctx context.Context, p string, queries ...string) ([]candidate, error) {
	var last error
	for _, query := range queries {
		target := hiBase + "/search?keyword=" + url.QueryEscape(query)
		if p == "otakudesu" {
			target = otakuBase + "/?post_type=anime&s=" + url.QueryEscape(query)
		}
		b, e := s.get(ctx, target, "")
		if e != nil {
			last = e
			continue
		}
		found := parseCandidates(p, b)
		if len(found) > 0 {
			return found, nil
		}
	}
	return nil, last
}

func parseCandidates(p string, b []byte) []candidate {
	found := []candidate{}
	nodes(b, func(n *html.Node) {
		if n.Data != "a" {
			return
		}
		href := attr(n, "href")
		title := attr(n, "title")
		if p == "hianime" {
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
			return
		}
		if !class(n, "ot-k") {
			return
		}
		title = ""
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

// matchCandidate resolves a provider card to exactly one slug. Exact normalized
// equality wins; otherwise a card may differ only by a non-identity qualifier
// such as "(TV)" or "Subtitle Indonesia". Two equally good cards stay ambiguous:
// the adapter never guesses between different works.
func matchCandidate(found []candidate, a domain.Anime) (string, bool) {
	aliases := append([]string{a.Title}, a.AlternativeTitles...)
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
			if titleCore(name) == titleCore(key) {
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

var trailingYear = regexp.MustCompile(`(?:19|20)[0-9]{2}$`)

// titleCore removes presentation-only suffixes from an already normalized title.
// Season numbers and formats stay: they identify a different work.
func titleCore(name string) string {
	for _, suffix := range []string{"subtitleindonesia", "subindo", "uncensored", "tv", "dub", "sub", "bd"} {
		if strings.HasSuffix(name, suffix) && len(name) > len(suffix)+3 {
			name = strings.TrimSuffix(name, suffix)
		}
	}
	if year := trailingYear.FindString(name); year != "" && len(name) > len(year)+3 {
		name = strings.TrimSuffix(name, year)
	}
	return name
}

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

func (s *Service) providerEpisodes(ctx context.Context, p, slug string) ([]providerEpisode, error) {
	key := "episodes:" + p + ":" + slug
	if c, ok := s.getCache(key); ok {
		return c.value.([]providerEpisode), c.err
	}
	var out []providerEpisode
	var target string
	if p == "hianime" {
		parts := strings.Split(slug, "-")
		n := parts[len(parts)-1]
		if _, e := strconv.Atoi(n); e != nil {
			return nil, failure("invalid_mapping", p)
		}
		target = hiBase + "/api/theme/episode/list/" + n
	} else if p == "otakudesu" {
		target = otakuBase + "/anime/" + url.PathEscape(slug) + "/"
	} else {
		return s.aniEpisodes(ctx, slug)
	}
	b, e := s.get(ctx, target, "")
	if e != nil {
		return nil, e
	}
	seen := map[string]bool{}
	nodes(jsonHTML(b), func(n *html.Node) {
		if n.Data != "a" {
			return
		}
		label, k, title := "", "", text(n)
		if p == "hianime" {
			label = attr(n, "data-number")
			k = attr(n, "data-id")
		} else if class(n, "ot-ep") {
			k = absolute(target, attr(n, "href"))
			m := numberLabel.FindStringSubmatch(title)
			if len(m) > 1 {
				label = m[1]
			} else {
				label = title
			}
		}
		if label == "" || k == "" || seen[k] {
			return
		}
		seen[k] = true
		ordinal, _ := strconv.ParseFloat(label, 64)
		out = append(out, providerEpisode{label: label, key: k, title: title, ordinal: ordinal})
	})
	sort.SliceStable(out, func(i, j int) bool { return out[i].ordinal < out[j].ordinal })
	if len(out) == 0 {
		return nil, failure("unavailable", p)
	}
	s.putCache(key, out, nil, 10*time.Minute)
	return out, nil
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
	for _, p := range s.Providers() {
		if !p.Enabled {
			continue
		}
		slug, e := s.mapping(ctx, a, p.ID)
		if e != nil {
			last = e
			continue
		}
		eps, e := s.providerEpisodes(ctx, p.ID, slug)
		if e != nil {
			last = e
			continue
		}
		for _, ep := range eps {
			if i, ok := index[ep.label]; ok {
				out[i].Availability = append(out[i].Availability, p.ID)
			} else {
				index[ep.label] = len(out)
				out = append(out, domain.Episode{ID: episodeID(id, ep.label), AnimeID: id, Label: ep.label, Title: meaningfulTitle(ep.title, ep.label), Ordinal: ep.ordinal, Availability: []string{p.ID}})
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
	valid := r.Provider == "auto"
	for _, p := range s.Providers() {
		valid = valid || p.ID == r.Provider
	}
	if !valid {
		return domain.Source{}, failure("invalid_provider", r.Provider)
	}
	var last error = failure("unavailable", r.Provider)
	for _, p := range s.Providers() {
		if r.Provider != "auto" && r.Provider != p.ID {
			continue
		}
		if !p.Enabled || (p.ID == "otakudesu" && r.Mode == "dub") {
			last = failure("unavailable", p.ID)
			continue
		}
		key := "resolve:" + p.ID + ":" + r.EpisodeID + ":" + r.Mode
		if c, ok := s.getCache(key); ok {
			if c.err == nil {
				return c.value.(domain.Source), nil
			}
			last = c.err
			continue
		}
		slug, e := s.mapping(ctx, a, p.ID)
		var src domain.Source
		if e == nil {
			if p.ID == "anichan" {
				src, e = s.aniResolve(ctx, slug, label, r.Mode)
			} else {
				eps, er := s.providerEpisodes(ctx, p.ID, slug)
				e = er
				if e == nil {
					matches := []providerEpisode{}
					for _, ep := range eps {
						if ep.label == label {
							matches = append(matches, ep)
						}
					}
					if len(matches) != 1 {
						e = failure("invalid_mapping", p.ID)
					} else if p.ID == "hianime" {
						src, e = s.hiResolve(ctx, matches[0].key, r.Mode)
					} else {
						src, e = s.otakuResolve(ctx, matches[0].key)
					}
				}
			}
		}
		if e == nil {
			src.Provider = p.ID
			src.Mode = r.Mode
			if p.ID == "otakudesu" {
				src.Hardsub = true
				src.HardsubLanguage = "id"
			}
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
