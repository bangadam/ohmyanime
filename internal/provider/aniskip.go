package provider

import (
	"context"
	"encoding/json"
	"math"
	"myanime/internal/domain"
	"strconv"
	"time"
)

// Close releases pooled idle connections; active requests follow their context.
func (s *Service) Close() { s.client.CloseIdleConnections() }

// enrichSkip is optional: failure never prevents playback. The complete lookup,
// including a missing MAL mapping, shares a 1.8 second child deadline.
func (s *Service) enrichSkip(ctx context.Context, anime, label string, source *domain.Source) {
	if source.Intro != nil && source.Outro != nil {
		return
	}
	episode, e := strconv.Atoi(label)
	if e != nil || episode < 1 {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 1800*time.Millisecond)
	defer cancel()
	mal := 0
	if v, ok := s.getCache("mal:" + anime); ok {
		mal, _ = v.value.(int)
	} else {
		id, e := animeNumber(anime)
		if e != nil {
			return
		}
		var data struct {
			Media struct {
				IDMal int `json:"idMal"`
			}
		}
		if s.graphql(ctx, `query($id:Int){Media(id:$id,type:ANIME,isAdult:false){idMal}}`, map[string]any{"id": id}, &data) != nil {
			return
		}
		mal = data.Media.IDMal
		s.putCache("mal:"+anime, mal, nil, 24*time.Hour)
	}
	if mal < 1 {
		return
	}
	key := "skip:" + strconv.Itoa(mal) + ":" + label
	var skips [2]*domain.Interval
	if cached, ok := s.getCache(key); ok {
		skips, _ = cached.value.([2]*domain.Interval)
	} else {
		raw, e := s.get(ctx, "https://api.aniskip.com/v2/skip-times/"+strconv.Itoa(mal)+"/"+strconv.Itoa(episode)+"?types=op&types=ed&episodeLength=0", "")
		if e != nil {
			return
		}
		var result struct {
			Found   bool `json:"found"`
			Results []struct {
				SkipType      string  `json:"skipType"`
				EpisodeLength float64 `json:"episodeLength"`
				Interval      struct {
					StartTime float64 `json:"startTime"`
					EndTime   float64 `json:"endTime"`
				} `json:"interval"`
			} `json:"results"`
		}
		if json.Unmarshal(raw, &result) != nil {
			return
		}
		if result.Found {
			for _, v := range result.Results {
				a, b := v.Interval.StartTime, v.Interval.EndTime
				if math.IsNaN(a) || math.IsNaN(b) || math.IsInf(a, 0) || math.IsInf(b, 0) || a < 0 || b <= a || (v.EpisodeLength > 0 && b > v.EpisodeLength) {
					continue
				}
				interval := &domain.Interval{Start: a, End: b}
				switch v.SkipType {
				case "op":
					skips[0] = interval
				case "ed":
					skips[1] = interval
				}
			}
		}
		s.putCache(key, skips, nil, 30*time.Minute)
	}
	if source.Intro == nil {
		source.Intro = skips[0]
	}
	if source.Outro == nil {
		source.Outro = skips[1]
	}
}
