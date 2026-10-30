package provider

import (
	"errors"
	"fmt"
	"myanime/internal/domain"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestCatalogEnumMaps(t *testing.T) {
	tests := []struct {
		name string
		got  map[string]string
		want map[string]string
	}{
		{
			name: "sorts",
			got:  catalogSorts,
			want: map[string]string{
				"popular":    "POPULARITY_DESC",
				"trending":   "TRENDING_DESC",
				"score":      "SCORE_DESC",
				"newest":     "START_DATE_DESC",
				"updated":    "UPDATED_AT_DESC",
				"title_asc":  "TITLE_ROMAJI",
				"title_desc": "TITLE_ROMAJI_DESC",
			},
		},
		{
			name: "statuses",
			got:  catalogStatuses,
			want: map[string]string{
				"releasing":        "RELEASING",
				"finished":         "FINISHED",
				"not_yet_released": "NOT_YET_RELEASED",
				"cancelled":        "CANCELLED",
				"hiatus":           "HIATUS",
			},
		},
		{
			name: "formats",
			got:  catalogFormats,
			want: map[string]string{
				"tv":       "TV",
				"tv_short": "TV_SHORT",
				"movie":    "MOVIE",
				"special":  "SPECIAL",
				"ova":      "OVA",
				"ona":      "ONA",
				"music":    "MUSIC",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !reflect.DeepEqual(tt.got, tt.want) {
				t.Fatalf("tokens = %#v, want %#v", tt.got, tt.want)
			}
		})
	}
}

func TestCatalogQueryInvalidEnums(t *testing.T) {
	tests := []struct {
		name  string
		query domain.CatalogQuery
		code  string
	}{
		{name: "sort", query: domain.CatalogQuery{Sort: "popularity"}, code: "invalid_sort"},
		{name: "status", query: domain.CatalogQuery{Status: "airing"}, code: "invalid_status"},
		{name: "type", query: domain.CatalogQuery{Type: "film"}, code: "invalid_type"},
		{name: "season", query: domain.CatalogQuery{Season: "monsoon"}, code: "invalid_season"},
		{name: "cursor not a number", query: domain.CatalogQuery{Cursor: "abc"}, code: "invalid_cursor"},
		{name: "cursor zero", query: domain.CatalogQuery{Cursor: "0"}, code: "invalid_cursor"},
		{name: "cursor out of range", query: domain.CatalogQuery{Cursor: "10001"}, code: "invalid_cursor"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			document, vars, err := catalogQuery(tt.query)
			if err == nil {
				t.Fatalf("catalogQuery() = %q, %v; want %s", document, vars, tt.code)
			}
			var pe *Error
			if !errors.As(err, &pe) || pe.Code != tt.code || pe.Provider != "catalog" {
				t.Fatalf("error = %v, want %s from catalog", err, tt.code)
			}
			if document != "" || vars != nil {
				t.Fatalf("document = %q, vars = %v; want empty on failure", document, vars)
			}
		})
	}
}

// Cursor boundaries are inclusive: page 1 and page 10000 are valid, anything
// outside that range fails before a request is made.
func TestCatalogQueryCursorBoundaries(t *testing.T) {
	for _, cursor := range []string{"1", "10000"} {
		if _, vars, err := catalogQuery(domain.CatalogQuery{Cursor: cursor}); err != nil {
			t.Fatalf("cursor %q rejected: %v", cursor, err)
		} else if vars["page"] != func() int { n, _ := strconv.Atoi(cursor); return n }() {
			t.Fatalf("cursor %q mapped to page %v", cursor, vars["page"])
		}
	}
}

// Genres and tags each travel as one list variable, which AniList ANDs; they
// are never expanded into repeated OR clauses.
func TestCatalogQueryGenreAND(t *testing.T) {
	document, vars, err := catalogQuery(domain.CatalogQuery{Genres: []string{"Action", "Drama"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := vars["genreIn"]; !reflect.DeepEqual(got, []string{"Action", "Drama"}) {
		t.Fatalf("genreIn = %#v, want both entries", got)
	}
	if strings.Count(document, "genre_in:") != 1 {
		t.Fatalf("genres must stay in one AND clause:\n%s", document)
	}
	if strings.Contains(document, "genre_in:Action") {
		t.Fatalf("genre values must not be interpolated:\n%s", document)
	}
}

func TestNormalizeTerms(t *testing.T) {
	twelve := make([]string, 0, 12)
	for i := range 12 {
		twelve = append(twelve, fmt.Sprintf("tag-%02d", i))
	}
	overCap := make([]string, 0, 12)
	for i := range 12 {
		overCap = append(overCap, fmt.Sprintf(" tag-%02d ", i))
	}
	tests := []struct {
		name  string
		in    []string
		limit int
		want  []string
	}{
		{name: "trims and keeps first spelling", in: []string{" Action ", "action", "ACTION", " Fantasy "}, limit: 10, want: []string{"Action", "Fantasy"}},
		{name: "drops empty", in: []string{"", "  ", "Isekai"}, limit: 10, want: []string{"Isekai"}},
		{name: "caps at limit", in: twelve, limit: 10, want: twelve[:10]},
		{name: "trims before capping", in: overCap, limit: 10, want: twelve[:10]},
		{name: "no limit keeps every unique value", in: []string{"a", "b", "a"}, limit: 0, want: []string{"a", "b"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeTerms(tt.in, tt.limit); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("normalizeTerms(%v, %d) = %v, want %v", tt.in, tt.limit, got, tt.want)
			}
		})
	}
}

func TestAnimeTags(t *testing.T) {
	many := make([]mediaTag, 0, 25)
	for i := range 25 {
		many = append(many, mediaTag{Name: fmt.Sprintf("tag-%02d", i), Rank: 90 - i})
	}
	tests := []struct {
		name string
		in   []mediaTag
		want []string
	}{
		{
			name: "drops spoilers and low rank",
			in: []mediaTag{
				{Name: "Spoiler", Rank: 99, IsMediaSpoiler: true},
				{Name: "Noise", Rank: 59},
				{Name: "Isekai", Rank: 88},
				{Name: "Magic", Rank: 72},
			},
			want: []string{"Isekai", "Magic"},
		},
		{
			name: "orders by rank then name",
			in: []mediaTag{
				{Name: "Zeta", Rank: 80},
				{Name: "Alpha", Rank: 80},
				{Name: "Top", Rank: 90},
			},
			want: []string{"Top", "Alpha", "Zeta"},
		},
		{
			name: "caps at twenty",
			in:   many,
			want: []string{"tag-00", "tag-01", "tag-02", "tag-03", "tag-04", "tag-05", "tag-06", "tag-07", "tag-08", "tag-09", "tag-10", "tag-11", "tag-12", "tag-13", "tag-14", "tag-15", "tag-16", "tag-17", "tag-18", "tag-19"},
		},
		{
			name: "empty input",
			in:   nil,
			want: []string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := animeTags(tt.in); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("animeTags() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMediaAnimeGenresAndTags(t *testing.T) {
	m := media{
		ID:     7,
		Title:  struct{ Romaji, English, Native string }{Romaji: "Test"},
		Genres: []string{"Action", "", "Action", "Fantasy"},
		Tags: []mediaTag{
			{Name: "Spoiler", Rank: 99, IsMediaSpoiler: true},
			{Name: "Isekai", Rank: 88},
		},
	}
	a := m.anime()
	if want := []string{"Action", "Fantasy"}; !reflect.DeepEqual(a.Genres, want) {
		t.Fatalf("genres = %v, want %v", a.Genres, want)
	}
	if want := []string{"Isekai"}; !reflect.DeepEqual(a.Tags, want) {
		t.Fatalf("tags = %v, want %v", a.Tags, want)
	}
}

func TestCatalogQueryDocument(t *testing.T) {
	tests := []struct {
		name  string
		query domain.CatalogQuery
		decls []string
		args  []string
		vars  map[string]any
	}{
		{
			name:  "defaults",
			query: domain.CatalogQuery{},
			decls: []string{"$page:Int", "$perPage:Int", "$sort:[MediaSort]"},
			args:  []string{"type:ANIME", "isAdult:false", "sort:$sort"},
			vars:  map[string]any{"page": 1, "perPage": 24, "sort": []string{"POPULARITY_DESC"}},
		},
		{
			name:  "no filters keeps the minimal document",
			query: domain.CatalogQuery{Cursor: "2", Limit: 0},
			decls: []string{"$page:Int", "$perPage:Int", "$sort:[MediaSort]"},
			args:  []string{"type:ANIME", "isAdult:false", "sort:$sort"},
			vars:  map[string]any{"page": 2, "perPage": 24, "sort": []string{"POPULARITY_DESC"}},
		},
		{
			name:  "sort token travels as a variable",
			query: domain.CatalogQuery{Sort: domain.CatalogSortTitleDesc, Limit: 50},
			decls: []string{"$page:Int", "$perPage:Int", "$sort:[MediaSort]"},
			args:  []string{"type:ANIME", "isAdult:false", "sort:$sort"},
			vars:  map[string]any{"page": 1, "perPage": 50, "sort": []string{"TITLE_ROMAJI_DESC"}},
		},
		{
			name:  "status and format travel as variables",
			query: domain.CatalogQuery{Status: "not_yet_released", Type: "tv_short"},
			decls: []string{"$page:Int", "$perPage:Int", "$status:MediaStatus", "$format:MediaFormat", "$sort:[MediaSort]"},
			args:  []string{"type:ANIME", "isAdult:false", "status:$status", "format:$format", "sort:$sort"},
			vars:  map[string]any{"page": 1, "perPage": 24, "status": "NOT_YET_RELEASED", "format": "TV_SHORT", "sort": []string{"POPULARITY_DESC"}},
		},
		{
			name:  "genres and tags are normalized AND lists",
			query: domain.CatalogQuery{Genres: []string{" Action ", "action", "", "Fantasy"}, Tags: []string{"Isekai", "ISEKAI"}},
			decls: []string{"$page:Int", "$perPage:Int", "$genreIn:[String]", "$tags:[String]", "$sort:[MediaSort]"},
			args:  []string{"type:ANIME", "isAdult:false", "genre_in:$genreIn", "tag_in:$tags", "sort:$sort"},
			vars:  map[string]any{"page": 1, "perPage": 24, "genreIn": []string{"Action", "Fantasy"}, "tags": []string{"Isekai"}, "sort": []string{"POPULARITY_DESC"}},
		},
		{
			name:  "search season and year",
			query: domain.CatalogQuery{Query: "frieren", Season: "fall", Year: 2023, Sort: domain.CatalogSortNewest},
			decls: []string{"$page:Int", "$perPage:Int", "$search:String", "$season:MediaSeason", "$year:Int", "$sort:[MediaSort]"},
			args:  []string{"type:ANIME", "isAdult:false", "search:$search", "season:$season", "seasonYear:$year", "sort:$sort"},
			vars:  map[string]any{"page": 1, "perPage": 24, "search": "frieren", "season": "FALL", "year": 2023, "sort": []string{"START_DATE_DESC"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			document, vars, err := catalogQuery(tt.query)
			if err != nil {
				t.Fatalf("catalogQuery() error = %v", err)
			}
			want := "query(" + strings.Join(tt.decls, ",") + "){Page(page:$page,perPage:$perPage){pageInfo{hasNextPage} media(" + strings.Join(tt.args, ",") + "){" + mediaFields + "}}}"
			if document != want {
				t.Fatalf("document =\n%s\nwant\n%s", document, want)
			}
			if !reflect.DeepEqual(vars, tt.vars) {
				t.Fatalf("vars = %#v, want %#v", vars, tt.vars)
			}
		})
	}
}

func TestCatalogQueryPinsExactDefaultDocument(t *testing.T) {
	document, _, err := catalogQuery(domain.CatalogQuery{})
	if err != nil {
		t.Fatalf("catalogQuery() error = %v", err)
	}
	want := "query($page:Int,$perPage:Int,$sort:[MediaSort]){Page(page:$page,perPage:$perPage){pageInfo{hasNextPage} media(type:ANIME,isAdult:false,sort:$sort){" + mediaFields + "}}}"
	if document != want {
		t.Fatalf("document =\n%s\nwant\n%s", document, want)
	}
}

func TestTagVocabulary(t *testing.T) {
	items := tagVocabulary([]mediaTagEntry{
		{Name: "Isekai", Category: "Setting"},
		{Name: "isekai", Category: "Setting"}, // duplicate spelling, first wins
		{Name: "Gore", Category: "Theme", IsAdult: true},
		{Name: "Plot Twist", Category: "Theme", IsGeneralSpoiler: true},
		{Name: "Tragedy", Category: "Theme", IsMediaSpoiler: true},
		{Name: "", Category: "Theme"},
		{Name: "Band", Category: "Theme"},
		{Name: "Another World", Category: "Setting"},
	})
	want := []map[string]string{
		{"id": "Another World", "name": "Another World", "category": "Setting"},
		{"id": "Isekai", "name": "Isekai", "category": "Setting"},
		{"id": "Band", "name": "Band", "category": "Theme"},
	}
	if !reflect.DeepEqual(items, want) {
		t.Fatalf("tagVocabulary() = %#v, want %#v", items, want)
	}
	if got := tagVocabulary(nil); len(got) != 0 {
		t.Fatalf("tagVocabulary(nil) = %#v, want empty", got)
	}
}

func TestCachedTagEntriesFallback(t *testing.T) {
	s := New()
	s.putCache("anime:a", domain.Anime{Tags: []string{"Isekai", "Time Travel"}}, nil, time.Hour)
	// A second title repeats one tag with different casing; the first spelling
	// after sorting must win, and the duplicate must disappear.
	s.putCache("anime:b", domain.Anime{Tags: []string{"isekai", "Band"}}, nil, time.Hour)
	// Wrong key namespace, wrong value type and expired entries are ignored.
	s.putCache("manga:x", domain.Manga{Tags: []string{"Ignored"}}, nil, time.Hour)
	s.putCache("anime:wrong-type", "not an anime", nil, time.Hour)
	s.putCache("anime:expired", domain.Anime{Tags: []string{"Expired"}}, nil, -time.Minute)

	items := tagVocabulary(s.cachedTagEntries())
	want := []map[string]string{
		{"id": "Band", "name": "Band", "category": ""},
		{"id": "Isekai", "name": "Isekai", "category": ""},
		{"id": "Time Travel", "name": "Time Travel", "category": ""},
	}
	if !reflect.DeepEqual(items, want) {
		t.Fatalf("cached tags = %#v, want %#v", items, want)
	}
}

func TestCachedTagEntriesEmptyCache(t *testing.T) {
	if got := New().cachedTagEntries(); len(got) != 0 {
		t.Fatalf("cachedTagEntries() = %#v, want empty", got)
	}
}
