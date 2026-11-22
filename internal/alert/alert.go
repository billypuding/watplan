package alert

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"
)

type Notifier struct {
	url    string
	client *http.Client
	mu     sync.Mutex
	state  map[string]string
	sent   map[string]time.Time
	every  time.Duration
	post   func(text string) error
	queue  chan string
}

func New(webhook string) *Notifier {
	n := &Notifier{
		url:    webhook,
		client: &http.Client{Timeout: 10 * time.Second},
		state:  map[string]string{},
		sent:   map[string]time.Time{},
		every:  6 * time.Hour,
		queue:  make(chan string, 64),
	}
	n.post = n.webhookPost
	go n.run()
	return n
}

func (n *Notifier) run() {
	for text := range n.queue {
		if err := n.post(text); err != nil {
			log.Printf("alert: %v", err)
		}
	}
}

func NewFunc(fn func(text string) error) *Notifier {
	n := New("")
	n.post = fn
	return n
}

func (n *Notifier) Problem(key, text string) {
	if n == nil {
		return
	}
	n.mu.Lock()
	prev, had := n.state[key]
	last := n.sent[key]
	repeat := had && prev == text && time.Since(last) < n.every
	n.state[key] = text
	if !repeat {
		n.sent[key] = time.Now()
	}
	n.mu.Unlock()
	if repeat {
		return
	}
	n.send("⚠️ " + text)
}

func (n *Notifier) Raise(key, text string) {
	if n == nil {
		return
	}
	n.mu.Lock()
	_, had := n.state[key]
	last := n.sent[key]
	repeat := had && time.Since(last) < n.every
	n.state[key] = text
	if !repeat {
		n.sent[key] = time.Now()
	}
	n.mu.Unlock()
	if repeat {
		return
	}
	n.send("⚠️ " + text)
}

func (n *Notifier) Resolved(key, text string) {
	if n == nil {
		return
	}
	n.mu.Lock()
	_, had := n.state[key]
	delete(n.state, key)
	delete(n.sent, key)
	n.mu.Unlock()
	if had {
		n.send("✅ " + text)
	}
}

func (n *Notifier) send(text string) {
	select {
	case n.queue <- text:
	default:
		log.Printf("alert: kolejka pełna, pomijam %q", text)
	}
}

func (n *Notifier) webhookPost(text string) error {
	body, _ := json.Marshal(map[string]string{"content": text, "text": text})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := n.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return &statusErr{resp.StatusCode}
	}
	return nil
}

type statusErr struct{ code int }

func (e *statusErr) Error() string { return "webhook odpowiedział " + http.StatusText(e.code) }
