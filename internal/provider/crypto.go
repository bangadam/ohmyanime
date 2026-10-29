package provider

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"myanime/internal/domain"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

var megaKey = append([]byte("i?LMTAx0Q6,:}50U"), make([]byte, 16)...)
var megaIV = []byte("W0;27ToaUpl_P%'c")

func decode64(s string) ([]byte, error) {
	if len(s) > 2*1024*1024 {
		return nil, failure("malformed_source", "crypto")
	}
	return base64.RawStdEncoding.DecodeString(strings.TrimRight(strings.NewReplacer("-", "+", "_", "/").Replace(strings.TrimSpace(s)), "="))
}
func decryptCBC(raw string) ([]byte, error) {
	b, e := decode64(raw)
	if e != nil || len(b) == 0 || len(b)%aes.BlockSize != 0 {
		return nil, failure("malformed_source", "megaplay")
	}
	block, e := aes.NewCipher(megaKey)
	if e != nil {
		return nil, e
	}
	cipher.NewCBCDecrypter(block, megaIV).CryptBlocks(b, b)
	pad := int(b[len(b)-1])
	if pad < 1 || pad > 16 || pad > len(b) {
		return nil, failure("malformed_source", "megaplay")
	}
	for _, v := range b[len(b)-pad:] {
		if int(v) != pad {
			return nil, failure("malformed_source", "megaplay")
		}
	}
	b = b[:len(b)-pad]
	if !utf8.Valid(b) {
		return nil, failure("malformed_source", "megaplay")
	}
	return b, nil
}
func decryptGCM(key, nonce, b []byte) ([]byte, error) {
	block, e := aes.NewCipher(key)
	if e != nil {
		return nil, failure("malformed_source", "crypto")
	}
	g, e := cipher.NewGCM(block)
	if e != nil || len(nonce) != g.NonceSize() || len(b) < g.Overhead() {
		return nil, failure("malformed_source", "crypto")
	}
	out, e := g.Open(nil, nonce, b, nil)
	if e != nil {
		return nil, failure("malformed_source", "crypto")
	}
	return out, nil
}

// DecodeSegmentURL is only for the Megaplay /segment/{AES-CBC token} format.
// The media proxy must opt into it for a resolved Megaplay session, never globally.
func DecodeSegmentURL(raw string) (string, error) {
	u, e := url.Parse(raw)
	if e != nil {
		return "", e
	}
	if !strings.HasPrefix(u.Path, "/segment/") {
		return raw, nil
	}
	token := strings.TrimPrefix(u.Path, "/segment/")
	b, e := decryptCBC(token)
	if e != nil {
		return "", e
	}
	v, e := url.Parse(string(b))
	if e != nil {
		return "", e
	}
	if e = validateURL(v); e != nil {
		return "", e
	}
	return v.String(), nil
}
func capture(pattern string, b []byte) string {
	m := regexp.MustCompile(pattern).FindSubmatch(b)
	if len(m) < 2 {
		return ""
	}
	return string(m[1])
}
func config(b []byte) (map[string]any, error) {
	var c map[string]any
	if json.Unmarshal(b, &c) != nil || c == nil {
		return nil, failure("malformed_source", "config")
	}
	return c, nil
}
func (s *Service) embed(ctx context.Context, embed string) (domain.Source, error) {
	page, e := s.get(ctx, embed, hiBase+"/")
	if e != nil {
		return domain.Source{}, e
	}
	ref := origin(embed)
	var cfg map[string]any
	if strings.Contains(strings.ToLower(embed), "megaplay") {
		id := capture(`data-id=["']([0-9]+)["']`, page)
		if id == "" {
			return domain.Source{}, failure("malformed_source", "megaplay")
		}
		var last error
		for _, endpoint := range []string{"getSourcesNew", "getSources"} {
			b, e := s.get(ctx, ref+"stream/"+endpoint+"?id="+id, embed)
			if e != nil {
				last = e
				continue
			}
			cfg, e = config(b)
			if e != nil {
				last = e
				continue
			}
			if enc, ok := cfg["enc"].(string); ok && enc != "" {
				plain, e := decryptCBC(enc)
				if e != nil {
					last = e
					continue
				}
				dec, e := config(plain)
				if e != nil {
					last = e
					continue
				}
				for k, v := range dec {
					cfg[k] = v
				}
			}
			src, e := s.source(ctx, cfg, ref)
			if e == nil {
				return src, nil
			}
			last = e
		}
		return domain.Source{}, last
	}
	blob := capture(`window\.__P\s*=\s*["']([^"']+)["']`, page)
	raw, e := decode64(blob)
	if e != nil || len(raw) == 0 {
		return domain.Source{}, failure("malformed_source", "legacy")
	}
	key := []byte("otaku-embed-v1")
	for i := range raw {
		raw[i] ^= key[i%len(key)]
	}
	cfg, e = config(raw)
	if e != nil {
		return domain.Source{}, e
	}
	return s.source(ctx, cfg, ref)
}
func (s *Service) otakuResolve(ctx context.Context, episode string) (domain.Source, error) {
	b, e := s.get(ctx, episode, otakuBase+"/")
	if e != nil {
		return domain.Source{}, e
	}
	embed := capture(`<iframe[^>]*src=["']([^"']*putarin\.[^"']+)["']`, b)
	if embed == "" {
		embed = capture(`(?s)class=["']ot-player["'][^>]*>.*?<iframe[^>]*src=["']([^"']+)["']`, b)
	}
	embed = absolute(episode, embed)
	if embed == "" {
		return domain.Source{}, failure("malformed_source", "otakudesu")
	}
	b, e = s.get(ctx, embed, episode)
	if e != nil {
		return domain.Source{}, e
	}
	px, e := config([]byte(capture(`window\.__PX\s*=\s*(\{[^<]+?\})\s*;`, b)))
	if e != nil {
		return domain.Source{}, e
	}
	nonce := str(px["n"])
	if nonce == "" {
		return domain.Source{}, failure("malformed_source", "otakudesu")
	}
	kb, e := s.get(ctx, origin(embed)+"api/pk?n="+url.QueryEscape(nonce), embed)
	if e != nil {
		return domain.Source{}, e
	}
	key, e := hex.DecodeString(strings.TrimSpace(string(kb)))
	if e != nil || len(key) != 32 {
		return domain.Source{}, failure("malformed_source", "otakudesu")
	}
	raw, e := decode64(str(px["d"]))
	if e != nil || len(raw) < 28 {
		return domain.Source{}, failure("malformed_source", "otakudesu")
	}
	dec, e := decryptGCM(key, raw[:12], raw[12:])
	if e != nil {
		return domain.Source{}, e
	}
	cfg, e := config(dec)
	if e != nil {
		return domain.Source{}, e
	}
	return s.source(ctx, cfg, origin(embed))
}
func aniDecrypt(raw map[string]any, token, o, b string) (map[string]any, error) {
	if raw["v"] != float64(1) {
		return raw, nil
	}
	left, e := decode64(o)
	if e != nil {
		return nil, e
	}
	right, e := decode64(b)
	if e != nil || len(left) != 32 || len(right) != 32 {
		return nil, failure("malformed_source", "anichan")
	}
	for i := range left {
		left[i] ^= right[i]
	}
	mac := hmac.New(sha256.New, left)
	mac.Write([]byte(token))
	iv, e := decode64(str(raw["i"]))
	if e != nil {
		return nil, e
	}
	ct, e := decode64(str(raw["d"]))
	if e != nil {
		return nil, e
	}
	plain, e := decryptGCM(mac.Sum(nil), iv, ct)
	if e != nil {
		return nil, e
	}
	return config(plain)
}
func interval(v any) *domain.Interval {
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	a, ok := m["start"].(float64)
	b, ok2 := m["end"].(float64)
	if !ok || !ok2 || a < 0 || b <= a {
		return nil
	}
	return &domain.Interval{Start: a, End: b}
}
func sourceFile(c map[string]any) string {
	for _, k := range []string{"file", "src", "url"} {
		if s, ok := c[k].(string); ok && s != "" {
			return s
		}
	}
	switch v := c["sources"].(type) {
	case map[string]any:
		return sourceFile(v)
	case []any:
		for _, x := range v {
			if m, ok := x.(map[string]any); ok {
				if f := sourceFile(m); f != "" {
					return f
				}
			}
		}
	}
	return ""
}
func (s *Service) source(ctx context.Context, c map[string]any, ref string) (domain.Source, error) {
	file := sourceFile(c)
	if file == "" {
		return domain.Source{}, failure("malformed_source", "source")
	}
	master := absolute(ref, file)
	if master == "" {
		return domain.Source{}, failure("malformed_source", "source")
	}
	variants, e := s.manifest(ctx, master, ref, true)
	if e != nil {
		return domain.Source{}, e
	}
	out := domain.Source{MasterURL: master, Referer: ref, Variants: variants, Intro: interval(c["intro"]), Outro: interval(c["outro"])}
	tracks, _ := c["tracks"].([]any)
	if len(tracks) == 0 {
		tracks, _ = c["subtitles"].([]any)
	}
	out.Subtitles = normalizeSubtitles(tracks, ref)
	return out, nil
}
func (s *Service) manifest(ctx context.Context, raw, ref string, probe bool) ([]domain.Variant, error) {
	b, e := s.get(ctx, raw, ref)
	if e != nil {
		return nil, e
	}
	body := strings.TrimSpace(strings.TrimPrefix(string(b), "\ufeff"))
	if !strings.HasPrefix(body, "#EXTM3U\n") && !strings.HasPrefix(body, "#EXTM3U\r\n") {
		return nil, failure("malformed_source", "manifest")
	}
	lines := strings.Split(body, "\n")
	var variants []domain.Variant
	pending := ""
	segments := 0
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#EXT-X-STREAM-INF:") {
			pending = line
			continue
		}
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		u := absolute(raw, line)
		if u == "" {
			return nil, failure("malformed_source", "manifest")
		}
		if pending != "" {
			h, _ := strconv.Atoi(capture(`RESOLUTION=[0-9]+x([0-9]+)`, []byte(pending)))
			label := "Auto"
			if h > 0 {
				label = strconv.Itoa(h) + "p"
			}
			variants = append(variants, domain.Variant{ID: strconv.Itoa(len(variants) + 1), Label: label, Height: h, URL: u})
			pending = ""
		} else {
			segments++
		}
	}
	if pending != "" || len(variants) == 0 && (segments == 0 || !strings.Contains(body, "#EXTINF:")) {
		return nil, failure("malformed_source", "manifest")
	}
	if len(variants) > 0 && probe {
		if _, e = s.manifest(ctx, variants[0].URL, ref, false); e != nil {
			return nil, e
		}
	} else if len(variants) > 0 && !probe {
		return nil, failure("malformed_source", "manifest")
	}
	if len(variants) == 0 {
		variants = []domain.Variant{{ID: "auto", Label: "Auto", URL: raw}}
	}
	return variants, nil
}
