package push

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"os"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"

	"github.com/billypuding/watplan/internal/durable"
)

const (
	maxPlansPerSub = 10
	maxSubs        = 50000
	sendTTL        = 12 * 60 * 60
)

var ErrInvalid = errors.New("nieprawidłowa subskrypcja powiadomień")

var allowedHosts = []string{
	"fcm.googleapis.com",
	"updates.push.services.mozilla.com",
	"push.services.mozilla.com",
	".push.apple.com",
	".notify.windows.com",
}

type Keys struct {
	P256dh string `json:"p256dh"`
	Auth   string `json:"auth"`
}

type Subscription struct {
	Endpoint string   `json:"endpoint"`
	Keys     Keys     `json:"keys"`
	Plans    []string `json:"plans"`
	Updated  string   `json:"updated"`
}

type Payload struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	URL   string `json:"url"`
	Tag   string `json:"tag"`
}

type sender func(payload []byte, sub Subscription) (int, error)

type Service struct {
	public  string
	private string
	subject string
	path    string

	mu     sync.Mutex
	subs   map[string]Subscription
	send   sender
	mirror durable.Mirror
}

const mirrorKey = "push/subscriptions"

func (s *Service) Restore(m durable.Mirror) (int, error) {
	remote, err := m.All(mirrorKey)
	if err != nil {
		return 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mirror = m
	added := 0
	if data, ok := remote[mirrorKey]; ok {
		var list []Subscription
		if err := json.Unmarshal(data, &list); err != nil {
			return 0, err
		}
		for _, sub := range list {
			if _, ok := s.subs[sub.Endpoint]; !ok {
				s.subs[sub.Endpoint] = sub
				added++
			}
		}
	}
	return added, s.saveLocked()
}

func GenerateKeys() (public, private string, err error) {
	private, public, err = webpush.GenerateVAPIDKeys()
	return public, private, err
}

func New(public, private, subject, path string) (*Service, error) {
	if public == "" || private == "" {
		return nil, errors.New("brak kluczy VAPID")
	}
	s := &Service{public: public, private: private, subject: subject, path: path, subs: map[string]Subscription{}}
	s.send = s.webpushSend
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Service) PublicKey() string {
	return s.public
}

func (s *Service) load() error {
	if s.path == "" {
		return nil
	}
	data, err := os.ReadFile(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var list []Subscription
	if err := json.Unmarshal(data, &list); err != nil {
		return fmt.Errorf("%s: %w", s.path, err)
	}
	for _, sub := range list {
		s.subs[sub.Endpoint] = sub
	}
	return nil
}

func (s *Service) saveLocked() error {
	if s.path == "" && s.mirror == nil {
		return nil
	}
	list := make([]Subscription, 0, len(s.subs))
	for _, sub := range s.subs {
		list = append(list, sub)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Endpoint < list[j].Endpoint })
	data, err := json.Marshal(list)
	if err != nil {
		return err
	}
	if s.mirror != nil {
		_ = s.mirror.Put(mirrorKey, data)
	}
	if s.path == "" {
		return nil
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func validEndpoint(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || len(raw) > 1024 {
		return false
	}
	host := strings.ToLower(u.Hostname())
	for _, allowed := range allowedHosts {
		if host == strings.TrimPrefix(allowed, ".") || (strings.HasPrefix(allowed, ".") && strings.HasSuffix(host, allowed)) {
			return true
		}
	}
	return false
}

func (s *Service) Subscribe(sub Subscription) error {
	if !validEndpoint(sub.Endpoint) || sub.Keys.P256dh == "" || sub.Keys.Auth == "" ||
		len(sub.Keys.P256dh) > 200 || len(sub.Keys.Auth) > 100 {
		return ErrInvalid
	}
	seen := map[string]bool{}
	plans := make([]string, 0, len(sub.Plans))
	for _, id := range sub.Plans {
		if id != "" && !seen[id] {
			seen[id] = true
			plans = append(plans, id)
		}
	}
	if len(plans) > maxPlansPerSub {
		return fmt.Errorf("%w: najwyżej %d grup", ErrInvalid, maxPlansPerSub)
	}
	sort.Strings(plans)
	sub.Plans = plans
	sub.Updated = time.Now().UTC().Format(time.RFC3339)

	s.mu.Lock()
	defer s.mu.Unlock()
	if len(plans) == 0 {
		delete(s.subs, sub.Endpoint)
		return s.saveLocked()
	}
	if _, ok := s.subs[sub.Endpoint]; !ok && len(s.subs) >= maxSubs {
		return fmt.Errorf("%w: limit subskrypcji", ErrInvalid)
	}
	s.subs[sub.Endpoint] = sub
	return s.saveLocked()
}

func (s *Service) Has(endpoint string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.subs[endpoint]
	return ok
}

func (s *Service) Unsubscribe(endpoint string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.subs[endpoint]; !ok {
		return nil
	}
	delete(s.subs, endpoint)
	return s.saveLocked()
}

func (s *Service) Watched() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	seen := map[string]bool{}
	for _, sub := range s.subs {
		for _, id := range sub.Plans {
			seen[id] = true
		}
	}
	out := make([]string, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func (s *Service) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.subs)
}

func (s *Service) Notify(planID string, p Payload) int {
	s.mu.Lock()
	var targets []Subscription
	for _, sub := range s.subs {
		if slices.Contains(sub.Plans, planID) {
			targets = append(targets, sub)
		}
	}
	s.mu.Unlock()
	if len(targets) == 0 {
		return 0
	}

	data, err := json.Marshal(p)
	if err != nil {
		return 0
	}
	sent := 0
	var gone []string
	for _, sub := range targets {
		status, err := s.send(data, sub)
		switch {
		case status == http.StatusNotFound || status == http.StatusGone:
			gone = append(gone, sub.Endpoint)
		case err != nil || status >= 300:
			log.Printf("push %s: status %d: %v", planID, status, err)
		default:
			sent++
		}
	}
	if len(gone) > 0 {
		s.mu.Lock()
		for _, endpoint := range gone {
			delete(s.subs, endpoint)
		}
		if err := s.saveLocked(); err != nil {
			log.Printf("push: zapis subskrypcji: %v", err)
		}
		s.mu.Unlock()
	}
	log.Printf("push %s: wysłano %d z %d, wygasło %d", planID, sent, len(targets), len(gone))
	return sent
}

func (s *Service) webpushSend(payload []byte, sub Subscription) (int, error) {
	resp, err := webpush.SendNotification(payload, &webpush.Subscription{
		Endpoint: sub.Endpoint,
		Keys:     webpush.Keys{P256dh: sub.Keys.P256dh, Auth: sub.Keys.Auth},
	}, &webpush.Options{
		Subscriber:      s.subject,
		VAPIDPublicKey:  s.public,
		VAPIDPrivateKey: s.private,
		TTL:             sendTTL,
		Urgency:         webpush.UrgencyNormal,
		Topic:           "",
	})
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	return resp.StatusCode, nil
}
