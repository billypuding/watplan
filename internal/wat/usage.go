package wat

import (
	"encoding/json"
	"sync"
	"time"
)

const usageKeep = 31 * 24 * time.Hour

type usageBucket struct {
	Requests int   `json:"r"`
	Credits  int   `json:"c,omitempty"`
	Bytes    int64 `json:"b,omitempty"`
}

type UsageStats struct {
	Requests24h int   `json:"requests_24h"`
	Requests30d int   `json:"requests_30d"`
	Credits24h  int   `json:"credits_24h,omitempty"`
	Credits30d  int   `json:"credits_30d,omitempty"`
	Bytes24h    int64 `json:"bytes_24h,omitempty"`
	Bytes30d    int64 `json:"bytes_30d,omitempty"`
}

type Usage struct {
	mu      sync.Mutex
	hours   map[string]map[int64]usageBucket
	persist func([]byte)
	now     func() time.Time
}

func NewUsage() *Usage {
	return &Usage{hours: map[string]map[int64]usageBucket{}, now: time.Now}
}

func (u *Usage) SetPersist(fn func([]byte)) {
	if u == nil {
		return
	}
	u.mu.Lock()
	u.persist = fn
	u.mu.Unlock()
}

func (u *Usage) Load(data []byte) error {
	if u == nil || len(data) == 0 {
		return nil
	}
	var stored map[string]map[int64]usageBucket
	if err := json.Unmarshal(data, &stored); err != nil {
		return err
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	for provider, hours := range stored {
		if u.hours[provider] == nil {
			u.hours[provider] = map[int64]usageBucket{}
		}
		for hour, b := range hours {
			cur := u.hours[provider][hour]
			cur.Requests += b.Requests
			cur.Credits += b.Credits
			cur.Bytes += b.Bytes
			u.hours[provider][hour] = cur
		}
	}
	return nil
}

func (u *Usage) Record(provider string, credits int, bytes int64) {
	if u == nil {
		return
	}
	u.mu.Lock()
	now := u.now()
	hour := now.Truncate(time.Hour).Unix()
	if u.hours[provider] == nil {
		u.hours[provider] = map[int64]usageBucket{}
	}
	b := u.hours[provider][hour]
	b.Requests++
	b.Credits += credits
	b.Bytes += bytes
	u.hours[provider][hour] = b
	oldest := now.Add(-usageKeep).Unix()
	for _, hours := range u.hours {
		for h := range hours {
			if h < oldest {
				delete(hours, h)
			}
		}
	}
	persist := u.persist
	var data []byte
	if persist != nil {
		data, _ = json.Marshal(u.hours)
	}
	u.mu.Unlock()
	if persist != nil {
		go persist(data)
	}
}

func (u *Usage) Snapshot() map[string]UsageStats {
	if u == nil {
		return nil
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	now := u.now()
	day := now.Add(-24 * time.Hour).Unix()
	month := now.Add(-30 * 24 * time.Hour).Unix()
	out := map[string]UsageStats{}
	for provider, hours := range u.hours {
		var s UsageStats
		for h, b := range hours {
			if h >= month {
				s.Requests30d += b.Requests
				s.Credits30d += b.Credits
				s.Bytes30d += b.Bytes
			}
			if h >= day {
				s.Requests24h += b.Requests
				s.Credits24h += b.Credits
				s.Bytes24h += b.Bytes
			}
		}
		out[provider] = s
	}
	return out
}

func (u *Usage) Requests24h(provider string) int {
	if u == nil {
		return 0
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	day := u.now().Add(-24 * time.Hour).Unix()
	n := 0
	for h, b := range u.hours[provider] {
		if h >= day {
			n += b.Requests
		}
	}
	return n
}
