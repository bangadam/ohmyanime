package provider

import (
	"strconv"
	"strings"
	"unicode"

	"myanime/internal/domain"
)

// Explicit provider labels take precedence over frequently incorrect CDN lang
// metadata. Unknown values remain und rather than guessing from their prefix.
var subtitleLanguages = map[string]string{
	"en": "en", "eng": "en", "english": "en",
	"id": "id", "ind": "id", "indonesian": "id", "indonesia": "id", "bahasa": "id", "bahasa indonesia": "id",
	"ja": "ja", "jpn": "ja", "japanese": "ja", "japan": "ja",
	"ar": "ar", "ara": "ar", "arabic": "ar", "arab": "ar",
	"es": "es", "spa": "es", "spanish": "es", "español": "es", "espanol": "es",
	"pt": "pt", "por": "pt", "portuguese": "pt", "português": "pt", "portuguese brazil": "pt-BR", "brazilian portuguese": "pt-BR",
	"fr": "fr", "fra": "fr", "fre": "fr", "french": "fr", "français": "fr", "francais": "fr",
	"de": "de", "deu": "de", "ger": "de", "german": "de", "deutsch": "de",
	"it": "it", "ita": "it", "italian": "it", "italiano": "it",
	"ko": "ko", "kor": "ko", "korean": "ko",
	"zh": "zh", "zho": "zh", "chi": "zh", "chinese": "zh", "chinese simplified": "zh-Hans", "chinese traditional": "zh-Hant",
	"ru": "ru", "rus": "ru", "russian": "ru",
	"th": "th", "tha": "th", "thai": "th",
	"vi": "vi", "vie": "vi", "vietnamese": "vi",
	"tr": "tr", "tur": "tr", "turkish": "tr",
	"hi": "hi", "hin": "hi", "hindi": "hi",
	"cs": "cs", "ces": "cs", "cze": "cs", "czech": "cs",
	"da": "da", "dan": "da", "danish": "da",
	"nl": "nl", "nld": "nl", "dut": "nl", "dutch": "nl",
	"fi": "fi", "fin": "fi", "finnish": "fi",
	"el": "el", "ell": "el", "gre": "el", "greek": "el",
	"hu": "hu", "hun": "hu", "hungarian": "hu",
	"no": "no", "nor": "no", "nob": "nb", "norwegian": "no",
	"pl": "pl", "pol": "pl", "polish": "pl",
	"ro": "ro", "ron": "ro", "rum": "ro", "romanian": "ro",
	"sk": "sk", "slk": "sk", "slo": "sk", "slovak": "sk",
	"sv": "sv", "swe": "sv", "swedish": "sv",
	"ms": "ms", "msa": "ms", "may": "ms", "malay": "ms",
	"tl": "tl", "tagalog": "tl", "fil": "fil", "filipino": "fil",
	"uk": "uk", "ukr": "uk", "ukrainian": "uk",
	"bg": "bg", "bul": "bg", "bulgarian": "bg",
	"hr": "hr", "hrv": "hr", "croatian": "hr",
	"en-us": "en-US", "en-gb": "en-GB", "pt-br": "pt-BR", "pt-pt": "pt-PT", "es-419": "es-419", "es-es": "es-ES", "zh-hans": "zh-Hans", "zh-hant": "zh-Hant", "zh-cn": "zh-CN", "zh-tw": "zh-TW",
}

func subtitleLanguage(label string) string {
	label = strings.ToLower(strings.Join(strings.Fields(label), " "))
	if code, ok := subtitleLanguages[label]; ok {
		return code
	}
	// Provider labels include decorations such as English [Full], English 2,
	// and Portuguese (Brazil). Match whole language words, never substrings.
	words := strings.FieldsFunc(label, func(r rune) bool { return !unicode.IsLetter(r) })
	normalized := strings.Join(words, " ")
	if code, ok := subtitleLanguages[normalized]; ok {
		return code
	}
	for _, word := range words {
		if len([]rune(word)) <= 3 {
			continue
		}
		if code, ok := subtitleLanguages[word]; ok {
			return code
		}
	}
	return ""
}

func normalizeSubtitles(tracks []any, ref string) []domain.Subtitle {
	out := []domain.Subtitle{}
	for _, raw := range tracks {
		t, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		kind := strings.ToLower(strings.TrimSpace(str(t["kind"])))
		if kind != "" && kind != "subtitle" && kind != "subtitles" && kind != "caption" && kind != "captions" {
			continue
		}
		u := absolute(ref, sourceFile(t))
		if u == "" {
			continue
		}
		label := strings.TrimSpace(str(t["label"]))
		lang := subtitleLanguage(label)
		if lang == "" {
			for _, key := range []string{"srclang", "lang", "language"} {
				if lang = subtitleLanguage(str(t[key])); lang != "" {
					break
				}
			}
		}
		if lang == "" {
			lang = "und"
		}
		if label == "" {
			label = "Subtitle " + strconv.Itoa(len(out)+1)
		}
		out = append(out, domain.Subtitle{ID: strconv.Itoa(len(out) + 1), Label: label, Language: lang, URL: u})
	}
	if len(out) > 0 {
		selected := 0
		for _, preferred := range []string{"id", "en"} {
			found := false
			for i, t := range out {
				if strings.Split(t.Language, "-")[0] == preferred {
					selected = i
					found = true
					break
				}
			}
			if found {
				break
			}
		}
		out[selected].Default = true
	}
	return out
}
