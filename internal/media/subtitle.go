package media

import (
	"bytes"
	"myanime/internal/domain"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

var timestamp = regexp.MustCompile(`^(?:(\d{2,}):)?(\d{2}):(\d{2})\.(\d{3})$`)

func cueTime(s string) (int64, bool) {
	m := timestamp.FindStringSubmatch(s)
	if m == nil {
		return 0, false
	}
	var h int64
	var e error
	if m[1] != "" {
		h, e = strconv.ParseInt(m[1], 10, 32)
		if e != nil {
			return 0, false
		}
	}
	min, _ := strconv.ParseInt(m[2], 10, 64)
	sec, _ := strconv.ParseInt(m[3], 10, 64)
	ms, _ := strconv.ParseInt(m[4], 10, 64)
	if min > 59 || sec > 59 {
		return 0, false
	}
	return ((h*60+min)*60+sec)*1000 + ms, true
}
func validateVTT(data []byte) error {
	if len(data) > maxBody || !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0 {
		return ErrInvalid
	}
	text := strings.TrimPrefix(string(data), "\ufeff")
	text = strings.ReplaceAll(text, "\r\n", "\n")
	if strings.Contains(text, "\r") {
		return ErrInvalid
	}
	lines := strings.Split(text, "\n")
	if len(lines) == 0 || (lines[0] != "WEBVTT" && !strings.HasPrefix(lines[0], "WEBVTT ") && !strings.HasPrefix(lines[0], "WEBVTT\t")) {
		return ErrInvalid
	}
	if strings.Contains(lines[0], "-->") {
		return ErrInvalid
	}
	count := 0
	blocks := strings.Split(text, "\n\n")
	for _, block := range blocks[1:] {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}
		ls := strings.Split(block, "\n")
		if ls[0] == "NOTE" || strings.HasPrefix(ls[0], "NOTE ") {
			continue
		}
		if ls[0] == "STYLE" || ls[0] == "REGION" {
			return ErrInvalid
		}
		at := 0
		if !strings.Contains(ls[0], "-->") {
			at = 1
		}
		if at >= len(ls) {
			return ErrInvalid
		}
		parts := strings.Split(ls[at], "-->")
		if len(parts) != 2 {
			return ErrInvalid
		}
		end := strings.Fields(parts[1])
		if len(end) == 0 {
			return ErrInvalid
		}
		a, ok := cueTime(strings.TrimSpace(parts[0]))
		b, ok2 := cueTime(end[0])
		if !ok || !ok2 || b <= a {
			return ErrInvalid
		}
		count++
		if count > 50000 {
			return ErrLimit
		}
	}
	if len(blocks) == 1 && len(lines) > 1 && strings.TrimSpace(strings.Join(lines[1:], "")) != "" {
		return ErrInvalid
	}
	return nil
}
func (m *Manager) AddSubtitle(id, owner, label string, data []byte) (domain.Subtitle, error) {
	if validateVTT(data) != nil || len(label) > 200 || strings.TrimSpace(label) == "" {
		return domain.Subtitle{}, ErrInvalid
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.expire(time.Now())
	s := m.sessions[m.ids[id]]
	if s == nil || s.owner != owner {
		return domain.Subtitle{}, ErrExpired
	}
	if s.uploads >= 16 || len(s.resources) >= maxResources {
		return domain.Subtitle{}, ErrLimit
	}
	rid := strconv.Itoa(len(s.resources) + 1)
	s.resources[rid] = resource{data: bytes.Clone(data), subtitle: true}
	s.uploads++
	t := domain.Subtitle{ID: rid, Label: label, Language: "und", URL: s.base + "/media/" + s.token + "/" + rid}
	return t, nil
}
