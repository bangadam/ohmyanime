package media

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

var uriAttribute = regexp.MustCompile(`(?i)(\bURI=")([^"]*)(")`)

func rewrite(s *state, base *url.URL, data []byte) ([]byte, error) {
	if !utf8.Valid(data) || !bytes.HasPrefix(data, []byte("#EXTM3U")) {
		return nil, ErrInvalid
	}
	lines := strings.Split(string(data), "\n")
	var failure error
	resolve := func(raw string) string {
		if failure != nil {
			return ""
		}
		if strings.Contains(raw, "{$") {
			failure = ErrInvalid
			return ""
		}
		u, e := url.Parse(raw)
		if e != nil {
			failure = e
			return ""
		}
		out, e := s.register(base.ResolveReference(u).String(), false)
		if e != nil {
			failure = e
		}
		return out
	}
	for i, line := range lines {
		line = strings.TrimSuffix(line, "\r")
		trim := strings.TrimSpace(line)
		if trim == "" {
			continue
		}
		if strings.HasPrefix(trim, "#") {
			line = uriAttribute.ReplaceAllStringFunc(line, func(attr string) string {
				m := uriAttribute.FindStringSubmatch(attr)
				return m[1] + resolve(m[2]) + m[3]
			})
			if strings.Contains(strings.ToUpper(line), "URI=") && !uriAttribute.MatchString(line) {
				return nil, ErrInvalid
			}
			lines[i] = line
		} else {
			lines[i] = resolve(trim)
		}
	}
	if failure != nil {
		return nil, failure
	}
	return []byte(strings.Join(lines, "\n")), nil
}
func segmentURL(provider string, u *url.URL) (string, bool, error) {
	if provider != "hianime" {
		return u.String(), false, nil
	}
	out := u.String()
	if strings.HasPrefix(u.Path, "/segment/") {
		token := strings.TrimPrefix(u.Path, "/segment/")
		if strings.Contains(token, "/") || len(token) > 16384 {
			return "", false, ErrInvalid
		}
		raw, e := base64.RawURLEncoding.DecodeString(strings.TrimRight(token, "="))
		if e != nil || len(raw) == 0 || len(raw)%aes.BlockSize != 0 {
			return "", false, ErrInvalid
		}
		key := append([]byte("i?LMTAx0Q6,:}50U"), make([]byte, 16)...)
		block, e := aes.NewCipher(key)
		if e != nil {
			return "", false, e
		}
		cipher.NewCBCDecrypter(block, []byte("W0;27ToaUpl_P%'c")).CryptBlocks(raw, raw)
		pad := int(raw[len(raw)-1])
		if pad < 1 || pad > 16 || pad > len(raw) {
			return "", false, ErrInvalid
		}
		for _, b := range raw[len(raw)-pad:] {
			if int(b) != pad {
				return "", false, ErrInvalid
			}
		}
		raw = raw[:len(raw)-pad]
		if !utf8.Valid(raw) {
			return "", false, ErrInvalid
		}
		out = string(raw)
		u, e = validURL(out)
		if e != nil {
			return "", false, e
		}
	}
	host := strings.ToLower(u.Hostname())
	strip := false
	for _, domain := range []string{"ibyteimg.com", "tiktokcdn.com", "ipstatp.com", "yoot.akirax.buzz"} {
		if host == domain || strings.HasSuffix(host, "."+domain) {
			strip = true
		}
	}
	return out, strip, nil
}
func serveStripped(w http.ResponseWriter, r *http.Request, resp *http.Response) {
	// This provider's image-wrapped MPEG-TS is transformed only after three
	// packet sync bytes prove the exact 252-byte framing, never by host alone.
	const capSegment = 32 << 20
	data, e := io.ReadAll(io.LimitReader(resp.Body, capSegment+1))
	if e != nil || len(data) > capSegment {
		http.Error(w, "invalid segment", 502)
		return
	}
	offset := 0
	if len(data) >= 629 && data[252] == 0x47 && data[440] == 0x47 && data[628] == 0x47 {
		offset = 252
	} else if len(data) < 377 || data[0] != 0x47 || data[188] != 0x47 || data[376] != 0x47 {
		http.Error(w, "invalid segment framing", 502)
		return
	}
	w.Header().Set("Content-Type", "video/mp2t")
	http.ServeContent(w, r, "segment.ts", time.Time{}, bytes.NewReader(data[offset:]))
}
