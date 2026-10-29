package provider

import (
	"context"
	"strconv"
	"time"
)

// AniChan's watch endpoint declares the available sequential episode count.
// Catalog planned episode counts are deliberately not treated as playable inventory.
func (s *Service) aniEpisodes(ctx context.Context, id string) ([]providerEpisode, error) {
	key := "episodes:anichan:" + id
	if c, ok := s.getCache(key); ok {
		return c.value.([]providerEpisode), c.err
	}
	last := failure("unavailable", "anichan")
	for _, target := range []string{aniBase + "/api/watch/episodes?anilistId=" + id} {
		b, e := s.get(ctx, target, aniBase+"/anime/"+id)
		if e != nil {
			last = e
			continue
		}
		c, e := config(b)
		if e != nil {
			last = e
			continue
		}
		total, ok := c["episodes"].(float64)
		if !ok || total < 1 || total > 10000 || total != float64(int(total)) {
			continue
		}
		out := make([]providerEpisode, 0, int(total))
		for i := range int(total) {
			label := strconv.Itoa(i + 1)
			out = append(out, providerEpisode{label: label, key: label, title: "Episode " + label, ordinal: float64(i + 1)})
		}
		s.putCache(key, out, nil, 10*time.Minute)
		return out, nil
	}
	return nil, last
}
