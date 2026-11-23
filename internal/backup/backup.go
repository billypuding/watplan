package backup

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"time"

	"github.com/billypuding/watplan/internal/durable"
)

type Uploader interface {
	PutObject(key string, value []byte, contentType, cacheControl string) error
}

type Snapshot struct {
	Created string            `json:"created"`
	Keys    int               `json:"keys"`
	Data    map[string][]byte `json:"data"`
}

func Key(at time.Time) string {
	return "backups/" + at.UTC().Format("2006-01-02") + ".json.gz"
}

func Encode(src durable.Mirror, at time.Time) ([]byte, int, error) {
	all, err := src.All("")
	if err != nil {
		return nil, 0, fmt.Errorf("odczyt bazy: %w", err)
	}
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if err := json.NewEncoder(zw).Encode(Snapshot{Created: at.UTC().Format(time.RFC3339), Keys: len(all), Data: all}); err != nil {
		return nil, 0, err
	}
	if err := zw.Close(); err != nil {
		return nil, 0, err
	}
	return buf.Bytes(), len(all), nil
}

func Decode(data []byte) (Snapshot, error) {
	var s Snapshot
	zr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return s, err
	}
	defer zr.Close()
	err = json.NewDecoder(zr).Decode(&s)
	return s, err
}

func Run(src durable.Mirror, dst Uploader, at time.Time) (string, int, int, error) {
	data, keys, err := Encode(src, at)
	if err != nil {
		return "", 0, 0, err
	}
	if keys == 0 {
		return "", 0, 0, fmt.Errorf("baza jest pusta, nie nadpisuję kopii")
	}
	key := Key(at)
	if err := dst.PutObject(key, data, "application/gzip", ""); err != nil {
		return "", 0, 0, err
	}
	return key, keys, len(data), nil
}
