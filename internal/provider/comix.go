package provider

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"myanime/internal/domain"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const cxBase = "https://comix.to"

const cxID = "comix"
const cxPrefix = cxID + ":"

// comixAdapter reads Comix's protected JSON API. Requests carry a "_=yti.<tag>"
// token: a double-SHA-256 keyed by comixKey over the canonical path+query,
// truncated to 16 base64url characters. Responses arrive as {"e": ...}
// envelopes: base64url bytes decrypted with a keyed stream cipher (a rotated
// XOR over uint32 words plus a byte tail) whose rotation is embedded in the
// ciphertext header. Both constructions were reproduced exactly from the
// site's secure bundle and verified against live traffic.
type comixAdapter struct {
	s *Service
}

func newComix(s *Service) *comixAdapter { return &comixAdapter{s: s} }

func (a *comixAdapter) Identity() Identity {
	return Identity{ID: cxID, Name: "Comix", Kind: KindManga}
}

// get signs and fetches one API call, decrypting a protected envelope.
func (a *comixAdapter) get(ctx context.Context, path string, params [][2]string, out any) error {
	plain := canonicalQuery(path, params)
	v := url.Values{}
	for _, kv := range params {
		v.Add(cxWireKey(kv[0]), kv[1])
	}
	v.Set("_", comixToken(plain))
	b, e := a.s.get(ctx, cxBase+"/api/v1"+path+"?"+v.Encode(), cxBase+"/")
	if e != nil {
		return e
	}
	var envelope struct {
		E string `json:"e"`
	}
	if e := json.Unmarshal(b, &envelope); e != nil || envelope.E == "" {
		// Unprotected responses (rare) are decoded directly.
		if e := json.Unmarshal(b, out); e != nil {
			return failure("malformed_source", cxID)
		}
		return nil
	}
	plainBytes, e := comixDecrypt(envelope.E)
	if e != nil || len(plainBytes) < 5 {
		return failure("malformed_source", cxID)
	}
	// The plaintext carries a 4-byte header before the JSON body.
	if e := json.Unmarshal(plainBytes[4:], out); e != nil {
		return failure("malformed_source", cxID)
	}
	return nil
}

// cxWireKey converts a signed canonical key to the axios wire form: the
// server canonicalizes "k[]" arrays to "k[0]", "k[1]" before verifying the
// token, so requests must send the bracket form while signing the indexed
// one.
func cxWireKey(key string) string {
	if i := strings.IndexByte(key, '['); i > 0 && key[len(key)-1] == ']' {
		if idx := key[i+1 : len(key)-1]; strings.Trim(idx, "0123456789") == "" {
			return key[:i] + "[]"
		}
	}
	return key
}

func (a *comixAdapter) Search(ctx context.Context, query, cursor string, limit int) ([]domain.Manga, string, error) {
	page := 1
	if cursor != "" {
		n, e := strconv.Atoi(cursor)
		if e != nil || n < 1 || n > 500 {
			return nil, "", failure("invalid_cursor", cxID)
		}
		page = n
	}
	if limit <= 0 || limit > 50 {
		limit = 24
	}
	params := [][2]string{
		{"content_rating[0]", "safe"},
		{"content_rating[1]", "suggestive"},
	}
	if q := strings.TrimSpace(query); q != "" {
		params = append(params,
			[2]string{"keyword", q},
			[2]string{"limit", strconv.Itoa(limit)},
			[2]string{"order[relevance]", "desc"},
			[2]string{"page", strconv.Itoa(page)},
		)
	} else {
		params = append(params,
			[2]string{"limit", strconv.Itoa(limit)},
			[2]string{"order[chapter_updated_at]", "desc"},
			[2]string{"page", strconv.Itoa(page)},
		)
	}
	var result cxSearch
	if e := a.get(ctx, "/manga", params, &result); e != nil {
		return nil, "", e
	}
	out := make([]domain.Manga, 0, len(result.Result.Items))
	for _, it := range result.Result.Items {
		out = append(out, cxManga(it))
	}
	next := ""
	if m := result.Result.Meta; m != nil && m.CurrentPage < m.LastPage {
		next = strconv.Itoa(m.CurrentPage + 1)
	}
	return out, next, nil
}

func (a *comixAdapter) Detail(ctx context.Context, id string) (domain.Manga, error) {
	hid, e := cxHID(id)
	if e != nil {
		return domain.Manga{}, e
	}
	var one cxDirect
	if e := a.get(ctx, "/manga/"+hid, nil, &one); e != nil {
		return domain.Manga{}, e
	}
	return cxMangaDetail(one.Result), nil
}

func (a *comixAdapter) Chapters(ctx context.Context, mangaID string) ([]domain.Chapter, error) {
	hid, e := cxHID(mangaID)
	if e != nil {
		return nil, e
	}
	out := []domain.Chapter{}
	for page := 1; page <= 50; page++ {
		var list cxChapters
		params := [][2]string{
			{"limit", "50"},
			{"order[number]", "desc"},
			{"page", strconv.Itoa(page)},
			{"v", "0"},
			{"w", "0"},
		}
		if e := a.get(ctx, "/manga/"+hid+"/chapters", params, &list); e != nil {
			return nil, e
		}
		for _, it := range list.Result.Items {
			if it.Number > 0 {
				out = append(out, cxChapter(it, mangaID))
			}
		}
		if m := list.Result.Meta; m == nil || page >= m.LastPage || len(list.Result.Items) < 50 {
			break
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Ordinal < out[j].Ordinal })
	return out, nil
}

func (a *comixAdapter) Chapter(ctx context.Context, chapterID string) (domain.Chapter, error) {
	num, e := cxChapterNumber(chapterID)
	if e != nil {
		return domain.Chapter{}, e
	}
	var one cxDirect
	if e := a.get(ctx, "/chapters/"+num, [][2]string{{"v", "0"}, {"w", "0"}}, &one); e != nil {
		return domain.Chapter{}, e
	}
	r := one.Result
	return domain.Chapter{
		ID:        cxPrefix + strconv.Itoa(r.ID),
		MangaID:   cxPrefix + r.MangaHID,
		Label:     "Ch. " + strconv.FormatFloat(r.Number, 'f', -1, 64),
		Title:     strings.TrimSpace(r.Name),
		Ordinal:   r.Number,
		Number:    strconv.FormatFloat(r.Number, 'f', -1, 64),
		Language:  r.Language,
		PageCount: len(r.Pages.Items),
		GroupName: r.Group.Name,
	}, nil
}

func (a *comixAdapter) Pages(ctx context.Context, chapterID string) ([]domain.MangaPage, error) {
	num, e := cxChapterNumber(chapterID)
	if e != nil {
		return nil, e
	}
	var one cxDirect
	if e := a.get(ctx, "/chapters/"+num, [][2]string{{"v", "0"}, {"w", "0"}}, &one); e != nil {
		return nil, e
	}
	items := one.Result.Pages.Items
	if len(items) == 0 || len(items) > 300 {
		return nil, failure("malformed_source", cxID)
	}
	out := make([]domain.MangaPage, 0, len(items))
	for i, p := range items {
		if p.URL == "" {
			return nil, failure("malformed_source", cxID)
		}
		out = append(out, domain.MangaPage{Index: i + 1, URL: p.URL})
	}
	return out, nil
}

func cxManga(it cxItem) domain.Manga {
	return domain.Manga{
		ID:           cxPrefix + it.HID,
		Title:        strings.TrimSpace(it.Title),
		PosterURL:    it.Poster.Medium,
		Type:         strings.Title(strings.ToLower(it.Type)),
		Year:         it.Year,
		Status:       cxStatus(it.Status),
		Availability: []string{cxID},
	}
}

func cxMangaDetail(r cxResult) domain.Manga {
	m := cxManga(cxItem{HID: r.HID, Title: r.Title, Type: r.Type, Year: r.Year, Status: r.Status, Poster: r.Poster})
	m.AlternativeTitles = r.AltTitles
	m.Synopsis = r.Synopsis
	for _, g := range r.Genres {
		m.Tags = append(m.Tags, g.Name)
	}
	return m
}

func cxChapter(it cxChapterItem, mangaID string) domain.Chapter {
	return domain.Chapter{
		ID:        cxPrefix + strconv.Itoa(it.ID),
		MangaID:   mangaID,
		Label:     "Ch. " + strconv.FormatFloat(it.Number, 'f', -1, 64),
		Title:     strings.TrimSpace(it.Name),
		Ordinal:   it.Number,
		Number:    strconv.FormatFloat(it.Number, 'f', -1, 64),
		Language:  it.Language,
		GroupName: it.Group.Name,
	}
}

func cxStatus(s string) string {
	switch s {
	case "releasing":
		return "ongoing"
	case "finished":
		return "completed"
	case "on_hiatus":
		return "hiatus"
	case "cancelled", "abandoned":
		return "cancelled"
	}
	return "unknown"
}

var cxHIDPattern = regexp.MustCompile(`^[a-z0-9]{2,16}$`)
var cxNumber = regexp.MustCompile(`^[0-9]+$`)

func cxHID(id string) (string, error) {
	hid, ok := strings.CutPrefix(id, cxPrefix)
	if !ok || !cxHIDPattern.MatchString(hid) {
		return "", failure("invalid_mapping", cxID)
	}
	return hid, nil
}

func cxChapterNumber(id string) (string, error) {
	num, ok := strings.CutPrefix(id, cxPrefix)
	if !ok || !cxNumber.MatchString(num) {
		return "", failure("invalid_mapping", cxID)
	}
	return num, nil
}

// --- Request token: yti.<16 chars of base64url(HMAC-ish double SHA-256)> ---

const cxAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"

// comixToken reproduces the site's tag: SHA-256 keyed by key[0:32] with a
// 64-byte length prefix over the plaintext, then SHA-256 keyed by key[32:64]
// with a 64-byte prefix over the first digest, base64url-encoded and cut to
// 16 characters.
func comixToken(plain string) string {
	inner := cxHash(comixKey[0:32], []byte(plain), 64)
	outer := cxHash(comixKey[32:64], inner, 64)
	return "yti." + cxB64URL(outer)[:16]
}

// cxHash is SHA-256 with a caller-controlled initial state and an arbitrary
// pre-message length prefix (both digests use 64 = the HMAC block size).
func cxHash(iv, data []byte, prefixLen int) []byte {
	h := newCxSHA()
	h.SetState(iv)
	h.SetTotalBits((prefixLen + len(data)) * 8)
	h.WriteRaw(data)
	return h.Sum(nil)
}

// --- Response decryption ---

// comixDecrypt reverses the site's stream cipher: decode base64url, read the
// rotation from bytes 2:4, rotate key[64:1088] by it, XOR uint32 words with a
// lagged key stream and the byte tail with a wrap-around stream.
func comixDecrypt(enc string) ([]byte, error) {
	raw, e := cxB64URLDecode(enc)
	if e != nil || len(raw) < 5 {
		return nil, failure("malformed_source", cxID)
	}
	n := len(raw)
	k := (int(raw[2])<<8 | int(raw[3])) % 1024
	stream := make([]byte, 1024)
	copy(stream, comixKey[64+k:1088])
	copy(stream[1024-k:], comixKey[64:64+k])

	// Word phase: words 1..n/4 XOR with stream words lagged by one word.
	buf := make([]byte, n)
	copy(buf, raw)
	words := n >> 2
	for s := 1; s < words; s++ {
		v := binary.LittleEndian.Uint32(buf[s*4:]) ^ binary.LittleEndian.Uint32(stream[((s-1)%256)*4:((s-1)%256)*4+4])
		binary.LittleEndian.PutUint32(buf[s*4:], v)
	}
	// Tail phase: remaining bytes XOR with a wrap-around byte stream.
	for p := words << 2; p < n; p++ {
		buf[p] ^= stream[(p-4+1024)%1024]
	}
	return buf, nil
}

// --- base64url (no padding) helpers matching the site's alphabet ---

func cxB64URL(b []byte) string {
	var out strings.Builder
	for i := 0; i < len(b); i += 3 {
		c := int(b[i])
		n1, n2 := 0, 0
		if i+1 < len(b) {
			n1 = int(b[i+1])
		}
		if i+2 < len(b) {
			n2 = int(b[i+2])
		}
		out.WriteByte(cxAlphabet[c>>2])
		out.WriteByte(cxAlphabet[(c&3)<<4|n1>>4])
		if i+1 < len(b) {
			out.WriteByte(cxAlphabet[(n1&15)<<2|n2>>6])
		}
		if i+2 < len(b) {
			out.WriteByte(cxAlphabet[n2&63])
		}
	}
	return out.String()
}

func cxB64URLDecode(s string) ([]byte, error) {
	if len(s)%4 == 1 {
		return nil, failure("malformed_source", cxID)
	}
	// Mirrors the site's decoder: output starts at offset 2 with two spare
	// bytes so the uint32 word view stays aligned with the JS buffers.
	// Envelopes use the standard base64 alphabet (+/), tokens the URL-safe
	// one (-_), so both are accepted.
	out := make([]byte, 2, (9+(3*len(s)>>2))&^3+8)
	acc, bits := 0, 0
	for i := 0; i < len(s); i++ {
		if s[i] == '=' {
			continue
		}
		v := -1
		switch c := s[i]; c {
		case '+':
			v = 62
		case '/':
			v = 63
		default:
			v = strings.IndexByte(cxAlphabet, c)
		}
		if v < 0 {
			return nil, failure("malformed_source", cxID)
		}
		acc = acc<<6 | v
		bits += 6
		if bits >= 8 {
			bits -= 8
			out = append(out, byte(acc>>bits))
		}
	}
	return out, nil
}

// Comix envelope shapes; unknown members are ignored on decode.
type cxSearch struct {
	Result struct {
		Items []cxItem `json:"items"`
		Meta  *struct {
			CurrentPage int `json:"current_page"`
			LastPage    int `json:"last_page"`
		} `json:"meta"`
	} `json:"result"`
}

type cxItem struct {
	ID     int      `json:"id"`
	HID    string   `json:"hid"`
	Title  string   `json:"title"`
	Type   string   `json:"type"`
	Status string   `json:"status"`
	Poster cxPoster `json:"poster"`
	// Upstream reports decimal chapters (e.g. 4.5), so this cannot be int.
	LatestChapter json.Number `json:"latestChapter"`
	Year          int         `json:"year"`
}

type cxPoster struct {
	Medium string `json:"medium"`
	Large  string `json:"large"`
}

type cxDirect struct {
	Result cxResult `json:"result"`
}

type cxResult struct {
	ID        int      `json:"id"`
	HID       string   `json:"hid"`
	MangaHID  string   `json:"-"`
	MangaID   int64    `json:"mangaId"`
	Title     string   `json:"title"`
	AltTitles []string `json:"altTitles"`
	Type      string   `json:"type"`
	Status    string   `json:"status"`
	Poster    cxPoster `json:"poster"`
	Year      int      `json:"year"`
	Synopsis  string   `json:"synopsis"`
	Genres    []struct {
		Name string `json:"name"`
	} `json:"genres"`
	Number   float64 `json:"number"`
	Name     string  `json:"name"`
	Language string  `json:"language"`
	Group    struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	} `json:"group"`
	Pages struct {
		Items []struct {
			URL    string `json:"url"`
			Width  int    `json:"width"`
			Height int    `json:"height"`
		} `json:"items"`
	} `json:"pages"`
}

type cxChapters struct {
	Result struct {
		Items []cxChapterItem `json:"items"`
		Meta  *struct {
			CurrentPage int `json:"current_page"`
			LastPage    int `json:"last_page"`
		} `json:"meta"`
	} `json:"result"`
}

type cxChapterItem struct {
	ID       int     `json:"id"`
	Number   float64 `json:"number"`
	Volume   int     `json:"volume"`
	Name     string  `json:"name"`
	Language string  `json:"language"`
	Group    struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	} `json:"group"`
}
