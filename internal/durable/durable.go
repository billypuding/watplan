package durable

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/url"
	"strings"
	"sync"
	"time"

	_ "github.com/tursodatabase/libsql-client-go/libsql"

	"github.com/billypuding/watplan/internal/alert"
)

type Mirror interface {
	Put(key string, value []byte) error
	All(prefix string) (map[string][]byte, error)
}

type Turso struct {
	db *sql.DB
}

func tursoURL(rawURL, token string) (string, error) {
	if rawURL == "" {
		return "", errors.New("brak adresu bazy")
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("zły adres bazy: %w", err)
	}
	if token != "" {
		q := u.Query()
		q.Set("authToken", token)
		u.RawQuery = q.Encode()
	}
	return u.String(), nil
}

func OpenTurso(rawURL, token string) (*Turso, error) {
	dsn, err := tursoURL(rawURL, token)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("libsql", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(4)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS kv (k TEXT PRIMARY KEY, v BLOB NOT NULL, updated INTEGER NOT NULL)`); err != nil {
		db.Close()
		return nil, fmt.Errorf("tworzenie tabeli: %w", err)
	}
	return &Turso{db: db}, nil
}

func (t *Turso) Put(key string, value []byte) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_, err := t.db.ExecContext(ctx,
		`INSERT INTO kv (k, v, updated) VALUES (?, ?, ?) ON CONFLICT(k) DO UPDATE SET v = excluded.v, updated = excluded.updated`,
		key, value, time.Now().Unix())
	return err
}

func (t *Turso) All(prefix string) (map[string][]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	rows, err := t.db.QueryContext(ctx, `SELECT k, v FROM kv WHERE substr(k, 1, ?) = ?`, len(prefix), prefix)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]byte{}
	for rows.Next() {
		var k string
		var v []byte
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

func (t *Turso) Close() error {
	return t.db.Close()
}

type Queue struct {
	target     Mirror
	batchDelay time.Duration
	retryDelay time.Duration
	mu         sync.Mutex
	wait       map[string][]byte
	wake       chan struct{}
	failMu     sync.Mutex
	failed     string
	saved      int
	alert      *alert.Notifier
}

func (q *Queue) SetAlert(n *alert.Notifier) {
	q.alert = n
}

func NewQueue(target Mirror) *Queue {
	return newQueue(target, 500*time.Millisecond, 5*time.Second)
}

func newQueue(target Mirror, batchDelay, retryDelay time.Duration) *Queue {
	q := &Queue{target: target, batchDelay: batchDelay, retryDelay: retryDelay, wait: map[string][]byte{}, wake: make(chan struct{}, 1)}
	go q.run()
	return q
}

func (q *Queue) Put(key string, value []byte) error {
	q.mu.Lock()
	q.wait[key] = append([]byte(nil), value...)
	q.mu.Unlock()
	select {
	case q.wake <- struct{}{}:
	default:
	}
	return nil
}

func (q *Queue) All(prefix string) (map[string][]byte, error) {
	return q.target.All(prefix)
}

func (q *Queue) Status() (string, int) {
	q.failMu.Lock()
	defer q.failMu.Unlock()
	return q.failed, q.saved
}

func (q *Queue) run() {
	for range q.wake {
		time.Sleep(q.batchDelay)
		q.mu.Lock()
		batch := q.wait
		q.wait = map[string][]byte{}
		q.mu.Unlock()
		for key, value := range batch {
			err := q.target.Put(key, value)
			q.failMu.Lock()
			if err != nil {
				q.failed = time.Now().Format("2006-01-02 15:04:05") + ": " + err.Error()
			} else {
				q.failed = ""
				q.saved++
			}
			q.failMu.Unlock()
			if err == nil {
				q.alert.Resolved("db", "Zapis do bazy znów działa.")
			}
			if err != nil {
				q.alert.Raise("db", "Zapis do bazy Turso nie działa: "+err.Error())
				log.Printf("baza: zapis %s: %v", key, err)
				q.mu.Lock()
				if _, newer := q.wait[key]; !newer {
					q.wait[key] = value
				}
				q.mu.Unlock()
				time.Sleep(q.retryDelay)
			}
		}
		q.mu.Lock()
		pending := len(q.wait) > 0
		q.mu.Unlock()
		if pending {
			select {
			case q.wake <- struct{}{}:
			default:
			}
		}
	}
}

func Keyed(prefix, id string) string {
	return prefix + "/" + id
}

func TrimKey(prefix, key string) string {
	return strings.TrimPrefix(key, prefix+"/")
}

type Memory struct {
	mu   sync.Mutex
	data map[string][]byte
}

func NewMemory() *Memory {
	return &Memory{data: map[string][]byte{}}
}

func (m *Memory) Put(key string, value []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[key] = append([]byte(nil), value...)
	return nil
}

func (m *Memory) All(prefix string) (map[string][]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[string][]byte{}
	for k, v := range m.data {
		if strings.HasPrefix(k, prefix) {
			out[k] = append([]byte(nil), v...)
		}
	}
	return out, nil
}
