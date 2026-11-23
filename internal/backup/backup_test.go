package backup

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/billypuding/watplan/internal/durable"
)

type fakeUploader struct {
	key, contentType string
	data             []byte
	err              error
}

func (f *fakeUploader) PutObject(key string, value []byte, contentType, _ string) error {
	if f.err != nil {
		return f.err
	}
	f.key, f.data, f.contentType = key, value, contentType
	return nil
}

func TestRunRoundTrip(t *testing.T) {
	src := durable.NewMemory()
	src.Put("plan/WMT26AX3S1", []byte(`{"events":[]}`))
	src.Put("push/abc", []byte{0, 1, 2})
	src.Put("meta/indexes", []byte(`{"saved":3}`))
	dst := &fakeUploader{}
	at := time.Date(2026, 11, 23, 23, 30, 0, 0, time.FixedZone("CET", 3600))
	key, keys, size, err := Run(src, dst, at)
	if err != nil || key != "backups/2026-11-23.json.gz" || keys != 3 || size != len(dst.data) || dst.contentType != "application/gzip" {
		t.Fatalf("%q %d %d %v %q", key, keys, size, err, dst.contentType)
	}
	snap, err := Decode(dst.data)
	if err != nil || snap.Keys != 3 || string(snap.Data["meta/indexes"]) != `{"saved":3}` || len(snap.Data["push/abc"]) != 3 {
		t.Fatalf("po odczycie: %+v %v", snap, err)
	}
	if !strings.HasPrefix(snap.Created, "2026-11-23T22:30:00") {
		t.Fatalf("created %q", snap.Created)
	}
}

func TestRunRefusesEmptyDatabase(t *testing.T) {
	dst := &fakeUploader{}
	if _, _, _, err := Run(durable.NewMemory(), dst, time.Now()); err == nil || dst.data != nil {
		t.Fatalf("pusta baza: %v, wysłano %d B", err, len(dst.data))
	}
}

func TestRunReportsUploadError(t *testing.T) {
	src := durable.NewMemory()
	src.Put("plan/A", []byte("{}"))
	if _, _, _, err := Run(src, &fakeUploader{err: errors.New("403")}, time.Now()); err == nil {
		t.Fatal("brak błędu wysyłki")
	}
}
