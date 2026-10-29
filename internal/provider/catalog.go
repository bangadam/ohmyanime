package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"myanime/internal/domain"
	"strconv"
	"strings"
	"time"
)

const metadataURL = "https://graphql.anilist.co"
const mediaFields = `id idMal title { romaji english native } synonyms coverImage { large } description format season seasonYear status`

type media struct {
	ID                                  int
	IDMal                               int `json:"idMal"`
	Title                               struct{ Romaji, English, Native string }
	Synonyms                            []string
	CoverImage                          struct{ Large string }
	Description, Format, Season, Status string
	SeasonYear                          int
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
	return domain.Anime{ID: fmt.Sprintf("anilist:%d", m.ID), Title: title, AlternativeTitles: aliases, PosterURL: m.CoverImage.Large, Synopsis: plain(m.Description), Type: m.Format, Season: strings.ToLower(m.Season), Year: m.SeasonYear, Status: m.Status, Availability: []string{}}
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
func (s *Service) Search(ctx context.Context, q domain.CatalogQuery) (domain.CatalogPage, error) {
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	out := domain.CatalogPage{Items: []domain.Anime{}}
	page := 1
	if q.Cursor != "" {
		n, e := strconv.Atoi(q.Cursor)
		if e != nil || n < 1 || n > 10000 {
			return out, failure("invalid_cursor", "catalog")
		}
		page = n
	}
	limit := q.Limit
	if limit < 1 {
		limit = 24
	}
	if limit > 50 {
		limit = 50
	}
	vars := map[string]any{"page": page, "perPage": limit}
	if q.Query != "" {
		vars["search"] = q.Query
	}
	if q.Genre != "" {
		vars["genre"] = q.Genre
	}
	if q.Season != "" {
		season := strings.ToUpper(q.Season)
		if season != "WINTER" && season != "SPRING" && season != "SUMMER" && season != "FALL" {
			return out, failure("invalid_season", "catalog")
		}
		vars["season"] = season
	}
	if q.Year > 0 {
		vars["year"] = q.Year
	}
	query := `query($page:Int,$perPage:Int,$search:String,$genre:String,$season:MediaSeason,$year:Int){Page(page:$page,perPage:$perPage){pageInfo{hasNextPage} media(type:ANIME,isAdult:false,search:$search,genre:$genre,season:$season,seasonYear:$year,sort:POPULARITY_DESC){` + mediaFields + `}}}`
	var result struct {
		Page struct {
			PageInfo struct{ HasNextPage bool }
			Media    []media
		}
	}
	if e := s.graphql(ctx, query, vars, &result); e != nil {
		return out, e
	}
	for _, m := range result.Page.Media {
		a := m.anime()
		out.Items = append(out.Items, a)
		s.putCache("anime:"+a.ID, a, nil, 30*time.Minute)
		s.putCache("mal:"+a.ID, m.IDMal, nil, 24*time.Hour)
	}
	if result.Page.PageInfo.HasNextPage {
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
