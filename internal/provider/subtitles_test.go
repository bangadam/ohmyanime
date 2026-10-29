package provider

import "testing"

func TestSubtitleLabelsOverrideMisleadingLanguage(t *testing.T) {
	tracks := []any{
		map[string]any{"label": "Czech", "lang": "en", "file": "/cs.vtt", "default": true},
		map[string]any{"label": "English", "file": "/en.vtt"},
		map[string]any{"label": "Bahasa Indonesia", "lang": "en", "file": "/id.vtt"},
		map[string]any{"label": "Preview", "kind": "thumbnails", "file": "/preview.vtt"},
		map[string]any{"label": "Unknown", "language": "madeup", "file": "/unknown.vtt"},
	}
	got := normalizeSubtitles(tracks, "https://example.com/")
	if len(got) != 4 {
		t.Fatalf("tracks: %#v", got)
	}
	for i, want := range []string{"cs", "en", "id", "und"} {
		if got[i].Language != want {
			t.Errorf("track %d language %q want %q", i, got[i].Language, want)
		}
		if got[i].Default != (i == 2) {
			t.Errorf("track %d default %v", i, got[i].Default)
		}
	}
}
func TestSubtitleEnglishThenFirstDefault(t *testing.T) {
	for _, tt := range []struct {
		labels []string
		want   int
	}{{[]string{"Arabic", "English [Full]"}, 1}, {[]string{"Czech", "Danish"}, 0}} {
		tracks := []any{}
		for _, label := range tt.labels {
			tracks = append(tracks, map[string]any{"label": label, "file": "/track.vtt"})
		}
		got := normalizeSubtitles(tracks, "https://example.com/")
		for i, v := range got {
			if v.Default != (i == tt.want) {
				t.Fatalf("defaults: %#v", got)
			}
		}
	}
}
func TestExplicitLanguageMappings(t *testing.T) {
	for _, tt := range []struct{ label, want string }{{"English 2", "en"}, {"Portuguese (Brazil)", "pt-BR"}, {"Japanese", "ja"}, {"Danish", "da"}, {"engineering", ""}, {"unrecognized", ""}, {"zh-Hant", "zh-Hant"}} {
		if got := subtitleLanguage(tt.label); got != tt.want {
			t.Errorf("%q = %q want %q", tt.label, got, tt.want)
		}
	}
}
