package provider

import (
	"errors"
	"fmt"
	"myanime/internal/domain"
	"reflect"
	"strings"
	"testing"
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
			decls: []string{"$page:Int", "$perPage:Int"},
			args:  []string{"type:ANIME", "isAdult:false", "sort:POPULARITY_DESC"},
			vars:  map[string]any{"page": 1, "perPage": 24},
		},
		{
			name:  "no filters keeps the minimal document",
			query: domain.CatalogQuery{Cursor: "2", Limit: 0},
			decls: []string{"$page:Int", "$perPage:Int"},
			args:  []string{"type:ANIME", "isAdult:false", "sort:POPULARITY_DESC"},
			vars:  map[string]any{"page": 2, "perPage": 24},
		},
		{
			name:  "sort token is interpolated",
			query: domain.CatalogQuery{Sort: domain.CatalogSortTitleDesc, Limit: 50},
			decls: []string{"$page:Int", "$perPage:Int"},
			args:  []string{"type:ANIME", "isAdult:false", "sort:TITLE_ROMAJI_DESC"},
			vars:  map[string]any{"page": 1, "perPage": 50},
		},
		{
			name:  "status and format travel as variables",
			query: domain.CatalogQuery{Status: "not_yet_released", Type: "tv_short"},
			decls: []string{"$page:Int", "$perPage:Int", "$status:MediaStatus", "$format:MediaFormat"},
			args:  []string{"type:ANIME", "isAdult:false", "status:$status", "format:$format", "sort:POPULARITY_DESC"},
			vars:  map[string]any{"page": 1, "perPage": 24, "status": "NOT_YET_RELEASED", "format": "TV_SHORT"},
		},
		{
			name:  "genres and tags are normalized AND lists",
			query: domain.CatalogQuery{Genres: []string{" Action ", "action", "", "Fantasy"}, Tags: []string{"Isekai", "ISEKAI"}},
			decls: []string{"$page:Int", "$perPage:Int", "$genres:[String]", "$tags:[String]"},
			args:  []string{"type:ANIME", "isAdult:false", "genre_in:$genres", "tag_in:$tags", "sort:POPULARITY_DESC"},
			vars:  map[string]any{"page": 1, "perPage": 24, "genres": []string{"Action", "Fantasy"}, "tags": []string{"Isekai"}},
		},
		{
			name:  "search season and year",
			query: domain.CatalogQuery{Query: "frieren", Season: "fall", Year: 2023, Sort: domain.CatalogSortNewest},
			decls: []string{"$page:Int", "$perPage:Int", "$search:String", "$season:MediaSeason", "$year:Int"},
			args:  []string{"type:ANIME", "isAdult:false", "search:$search", "season:$season", "seasonYear:$year", "sort:START_DATE_DESC"},
			vars:  map[string]any{"page": 1, "perPage": 24, "search": "frieren", "season": "FALL", "year": 2023},
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
	want := "query($page:Int,$perPage:Int){Page(page:$page,perPage:$perPage){pageInfo{hasNextPage} media(type:ANIME,isAdult:false,sort:POPULARITY_DESC){" + mediaFields + "}}}"
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
