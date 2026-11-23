package durable

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/billypuding/watplan/internal/alert"
)

type flakyMirror struct {
	mu     sync.Mutex
	down   bool
	data   map[string][]string
	tries  int
	onPut  func(key string)
	prefix string
}

func newFlaky() *flakyMirror {
	return &flakyMirror{data: map[string][]string{}}
}

func (m *flakyMirror) setDown(down bool) {
	m.mu.Lock()
	m.down = down
	m.mu.Unlock()
}

func (m *flakyMirror) Put(key string, value []byte) error {
	m.mu.Lock()
	m.tries++
	down, hook := m.down, m.onPut
	if !down {
		m.data[key] = append(m.data[key], string(value))
	}
	m.mu.Unlock()
	if hook != nil {
		hook(key)
	}
	if down {
		return errors.New("baza niedostępna")
	}
	return nil
}

func (m *flakyMirror) All(prefix string) (map[string][]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prefix = prefix
	return map[string][]byte{"plan/A": []byte("x")}, nil
}

func (m *flakyMirror) writes(key string) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.data[key]...)
}

func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("nie doczekałem się: %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

type alerts struct {
	mu   sync.Mutex
	sent []string
}

func (a *alerts) post(text string) error {
	a.mu.Lock()
	a.sent = append(a.sent, text)
	a.mu.Unlock()
	return nil
}

func (a *alerts) list() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.sent...)
}

func TestQueueSurvivesLongOutageAndAlertsOnce(t *testing.T) {
	m := newFlaky()
	m.setDown(true)
	got := &alerts{}
	q := newQueue(m, time.Millisecond, 2*time.Millisecond)
	q.SetAlert(alert.NewFunc(got.post))

	q.Put("plan/A", []byte("v1"))
	waitUntil(t, "kilka nieudanych prób", func() bool {
		m.mu.Lock()
		defer m.mu.Unlock()
		return m.tries >= 5
	})
	failed, saved := q.Status()
	if !strings.Contains(failed, "baza niedostępna") || saved != 0 {
		t.Fatalf("status w trakcie awarii: %q, zapisanych %d", failed, saved)
	}
	if n := len(m.writes("plan/A")); n != 0 {
		t.Fatalf("w czasie awarii zapisano %d wersji", n)
	}

	m.setDown(false)
	waitUntil(t, "zapis po powrocie bazy", func() bool { return len(m.writes("plan/A")) == 1 })
	waitUntil(t, "czysty status", func() bool { f, s := q.Status(); return f == "" && s == 1 })
	if w := m.writes("plan/A"); w[0] != "v1" {
		t.Fatalf("zapisano %v", w)
	}

	waitUntil(t, "alert o naprawie", func() bool { return len(got.list()) >= 2 })
	sent := got.list()
	problems := 0
	for _, s := range sent {
		if strings.HasPrefix(s, "⚠️") {
			problems++
		}
	}
	if problems != 1 || !strings.HasPrefix(sent[len(sent)-1], "✅") {
		t.Fatalf("alerty %q: chcę jeden problem i jedno potwierdzenie mimo wielu prób", sent)
	}
}

func TestQueueRetryDoesNotOverwriteNewerValue(t *testing.T) {
	m := newFlaky()
	m.setDown(true)
	var q *Queue
	var once sync.Once
	m.onPut = func(key string) {
		once.Do(func() { q.Put(key, []byte("nowsza")) })
	}
	q = newQueue(m, time.Millisecond, 20*time.Millisecond)
	q.Put("plan/A", []byte("starsza"))
	waitUntil(t, "pierwsza nieudana próba", func() bool {
		m.mu.Lock()
		defer m.mu.Unlock()
		return m.tries >= 1
	})
	m.setDown(false)
	waitUntil(t, "zapis", func() bool { return len(m.writes("plan/A")) > 0 })
	time.Sleep(50 * time.Millisecond)
	if w := m.writes("plan/A"); w[len(w)-1] != "nowsza" {
		t.Fatalf("ostatnia zapisana wersja %q (wszystkie %v), chcę nowszą", w[len(w)-1], w)
	}
}

func TestQueueCopiesValueAndDelegatesAll(t *testing.T) {
	m := newFlaky()
	q := newQueue(m, 10*time.Millisecond, time.Millisecond)
	value := []byte("abc")
	q.Put("plan/B", value)
	value[0] = 'X'
	waitUntil(t, "zapis", func() bool { return len(m.writes("plan/B")) == 1 })
	if w := m.writes("plan/B")[0]; w != "abc" {
		t.Fatalf("kolejka nie skopiowała wartości: %q", w)
	}
	all, err := q.All("plan/")
	if err != nil || string(all["plan/A"]) != "x" || m.prefix != "plan/" {
		t.Fatalf("All: %v %v %q", all, err, m.prefix)
	}
}

func TestQueueWritesManyKeys(t *testing.T) {
	m := newFlaky()
	q := newQueue(m, time.Millisecond, time.Millisecond)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			q.Put(Keyed("plan", string(rune('A'+i%26))+string(rune('a'+i/26))), []byte("v"))
		}(i)
	}
	wg.Wait()
	waitUntil(t, "50 kluczy", func() bool {
		m.mu.Lock()
		defer m.mu.Unlock()
		return len(m.data) == 50
	})
	if _, saved := q.Status(); saved < 50 {
		t.Fatalf("zapisanych %d", saved)
	}
}

func TestMemoryMirror(t *testing.T) {
	mem := NewMemory()
	value := []byte("abc")
	mem.Put("plan/A", value)
	mem.Put("meta/usage", []byte("u"))
	value[0] = 'X'
	all, _ := mem.All("plan/")
	if len(all) != 1 || string(all["plan/A"]) != "abc" {
		t.Fatalf("All(plan/) = %v", all)
	}
	all["plan/A"][0] = 'Y'
	again, _ := mem.All("")
	if string(again["plan/A"]) != "abc" || len(again) != 2 {
		t.Fatalf("Memory nie kopiuje danych: %v", again)
	}
}

func TestKeys(t *testing.T) {
	key := Keyed("plan", "WMT26AX3S1")
	if key != "plan/WMT26AX3S1" || TrimKey("plan", key) != "WMT26AX3S1" || TrimKey("plan", "inne") != "inne" {
		t.Fatalf("%q", key)
	}
}

func TestTursoURL(t *testing.T) {
	got, err := tursoURL("libsql://watplan-x.turso.io", "sekret/+=")
	if err != nil || got != "libsql://watplan-x.turso.io?authToken=sekret%2F%2B%3D" {
		t.Fatalf("%q %v", got, err)
	}
	got, err = tursoURL("https://db.example.com?tls=1", "")
	if err != nil || got != "https://db.example.com?tls=1" {
		t.Fatalf("bez tokenu: %q %v", got, err)
	}
	if _, err := tursoURL("", "t"); err == nil {
		t.Fatal("pusty adres bez błędu")
	}
	if _, err := tursoURL("http://[::1", "t"); err == nil {
		t.Fatal("zły adres bez błędu")
	}
}

func TestOpenTursoFailsFastWhenUnreachable(t *testing.T) {
	if _, err := OpenTurso("", ""); err == nil {
		t.Fatal("pusty adres bez błędu")
	}
	start := time.Now()
	_, err := OpenTurso("http://127.0.0.1:1", "t")
	if err == nil || !strings.Contains(err.Error(), "tworzenie tabeli") {
		t.Fatalf("nieosiągalna baza: %v", err)
	}
	if time.Since(start) > 16*time.Second {
		t.Fatalf("OpenTurso czekał %v", time.Since(start))
	}
}

func TestQueueAlertName(t *testing.T) {
	m := newFlaky()
	m.setDown(true)
	got := &alerts{}
	q := newQueue(m, time.Millisecond, 2*time.Millisecond)
	q.SetAlert(alert.NewFunc(got.post))
	q.SetAlertName("cdn", "Wysyłanie planów do CDN (R2)")
	q.Put("plans/A.json", []byte("v"))
	waitUntil(t, "alert", func() bool { return len(got.list()) >= 1 })
	if s := got.list()[0]; !strings.Contains(s, "Wysyłanie planów do CDN (R2) nie działa") || strings.Contains(s, "Turso") {
		t.Fatalf("alert %q", s)
	}
	m.setDown(false)
	waitUntil(t, "naprawa", func() bool {
		l := got.list()
		return len(l) >= 2 && strings.Contains(l[len(l)-1], "CDN (R2) znów działa")
	})
}
