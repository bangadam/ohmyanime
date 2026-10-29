package provider

import (
	"context"
	"errors"
	"myanime/internal/domain"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

func (s *Service) aniSession(ctx context.Context, force bool) (string, error) {
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()
	if !force && s.token != "" && time.Until(s.expiry) > time.Minute {
		return s.token, nil
	}
	b, e := s.request(ctx, "POST", aniBase+"/api/watch/session", aniBase+"/", map[string]string{"token": ""}, nil)
	if e != nil {
		return "", e
	}
	c, e := config(b)
	if e != nil {
		return "", e
	}
	token := str(c["n"])
	exp, ok := c["exp"].(float64)
	if token == "" || !ok || exp <= float64(time.Now().Unix()) {
		return "", failure("auth", "anichan")
	}
	s.token = token
	s.expiry = time.Unix(int64(exp), 0)
	return token, nil
}
func (s *Service) aniKeys() [3]string { s.sessionMu.Lock(); defer s.sessionMu.Unlock(); return s.keys }
func (s *Service) aniKeysStale(max time.Duration) bool {
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()
	return time.Since(s.keysAt) > max
}
func (s *Service) refreshAniKeys(ctx context.Context) error {
	b, e := s.get(ctx, aniBase+"/", aniBase+"/")
	if e != nil {
		return e
	}
	chunks := regexp.MustCompile(`src=["'](/_next/static/chunks/[^"']+)["']`).FindAllSubmatch(b, 12)
	pattern := regexp.MustCompile(`[a-zA-Z0-9_$]+="([0-9a-f]{8})",[a-zA-Z0-9_$]+="([A-Za-z0-9+/=]{44})",[a-zA-Z0-9_$]+="([A-Za-z0-9+/=]{44})"`)
	for _, chunk := range chunks {
		b, e := s.get(ctx, absolute(aniBase, string(chunk[1])), aniBase+"/")
		if e != nil {
			continue
		}
		if !strings.Contains(string(b), "watch/servers") {
			continue
		}
		m := pattern.FindSubmatch(b)
		if len(m) != 4 {
			continue
		}
		left, e := decode64(string(m[2]))
		if e != nil || len(left) != 32 {
			continue
		}
		right, e := decode64(string(m[3]))
		if e != nil || len(right) != 32 {
			continue
		}
		s.sessionMu.Lock()
		s.keys = [3]string{string(m[1]), string(m[2]), string(m[3])}
		s.keysAt = time.Now()
		s.sessionMu.Unlock()
		return nil
	}
	return failure("malformed_source", "anichan")
}
func (s *Service) aniResolve(ctx context.Context, id, label, mode string) (domain.Source, error) {
	n, e := strconv.ParseFloat(label, 64)
	if e != nil || n <= 0 {
		return domain.Source{}, failure("invalid_mapping", "anichan")
	}
	episodes, e := s.aniEpisodes(ctx, id)
	if e != nil {
		return domain.Source{}, e
	}
	matched := false
	for _, ep := range episodes {
		if ep.label == label {
			matched = true
			break
		}
	}
	if !matched {
		return domain.Source{}, failure("invalid_mapping", "anichan")
	}
	token, e := s.aniSession(ctx, false)
	if e != nil {
		return domain.Source{}, e
	}
	ref := aniBase + "/anime/" + id
	refreshed := false
	keysRefreshed := false
	last := failure("unavailable", "anichan")
	// AniChan rotates its request key without an error signal: a stale key still
	// answers 200 with an empty server list. Treat that as a rotation hint and
	// retry the same tier once with keys re-read from the site bundle.
	const keyRefreshTTL = 10 * time.Minute
	for _, tier := range []string{"fast", "rest"} {
		target := aniBase + "/api/watch/servers?" + url.Values{"anilistId": {id}, "ep": {label}, "category": {mode}, "tier": {tier}}.Encode()
		for {
			keys := s.aniKeys()
			b, e := s.request(ctx, "GET", target, ref, nil, map[string]string{"X-Wk": keys[0]})
			var upstream *Error
			if errors.As(e, &upstream) && upstream.Code == "auth" && !refreshed {
				refreshed = true
				token, e = s.aniSession(ctx, true)
				if e != nil {
					return domain.Source{}, e
				}
				b, e = s.request(ctx, "GET", target, ref, nil, map[string]string{"X-Wk": keys[0]})
			}
			if e != nil {
				last = e
				break
			}
			raw, e := config(b)
			if e != nil {
				last = e
				break
			}
			data, e := aniDecrypt(raw, token, keys[1], keys[2])
			if e != nil && !keysRefreshed {
				keysRefreshed = true
				if er := s.refreshAniKeys(ctx); er == nil {
					keys = s.aniKeys()
					data, e = aniDecrypt(raw, token, keys[1], keys[2])
				}
			}
			if e != nil {
				last = e
				break
			}
			servers, _ := data["servers"].([]any)
			if len(servers) == 0 && !keysRefreshed && s.aniKeysStale(keyRefreshTTL) {
				keysRefreshed = true
				if er := s.refreshAniKeys(ctx); er == nil {
					continue
				}
			}
			for _, v := range servers {
				server, ok := v.(map[string]any)
				if !ok || str(server["type"]) != "hls" {
					continue
				}
				if actual := str(server["category"]); actual != "" && actual != mode {
					continue
				}
				stream := str(server["stream"])
				if stream == "" {
					continue
				}
				u, e := url.Parse(stream)
				if e != nil {
					continue
				}
				streamRef := aniBase + "/"
				if direct := u.Query().Get("url"); direct != "" {
					stream = direct
					streamRef = u.Query().Get("ref")
					if streamRef == "" {
						streamRef = "https://megaplay.buzz/"
					}
				}
				cfg := map[string]any{"file": stream}
				for _, k := range []string{"tracks", "subtitles", "intro", "outro"} {
					if v, ok := server[k]; ok {
						cfg[k] = v
					}
				}
				src, e := s.source(ctx, cfg, streamRef)
				if e == nil {
					return src, nil
				}
				last = e
			}
			break
		}
	}
	return domain.Source{}, last
}
