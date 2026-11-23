package backup

import (
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/billypuding/watplan/internal/cdn"
	"github.com/billypuding/watplan/internal/durable"
)

func TestBackupFromTursoToS3(t *testing.T) {
	dbURL, s3URL := os.Getenv("PLANWAT_TEST_DB_URL"), os.Getenv("PLANWAT_TEST_S3_URL")
	if dbURL == "" || s3URL == "" {
		t.Skip("potrzebne PLANWAT_TEST_DB_URL i PLANWAT_TEST_S3_URL")
	}
	db, err := durable.OpenTurso(dbURL, "")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	run := fmt.Sprintf("bk%d", time.Now().UnixNano())
	db.Put("plan/"+run, []byte(`{"events":[]}`))
	db.Put("push/"+run, []byte("sub"))

	name := "watplan-backup-" + run
	bucket, err := cdn.New(s3URL, name, os.Getenv("PLANWAT_TEST_S3_KEY"), os.Getenv("PLANWAT_TEST_S3_SECRET"), "us-east-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := bucket.CreateBucket(); err != nil {
		t.Fatal(err)
	}
	key, keys, _, err := Run(db, bucket, time.Now())
	if err != nil || keys < 2 {
		t.Fatalf("Run: %v, %d kluczy", err, keys)
	}
	data, status, err := bucket.Get(key)
	if err != nil || status != http.StatusOK {
		t.Fatalf("pobranie kopii: %d %v", status, err)
	}
	snap, err := Decode(data)
	if err != nil || string(snap.Data["push/"+run]) != "sub" || string(snap.Data["plan/"+run]) != `{"events":[]}` {
		t.Fatalf("kopia bez danych testu: %v", err)
	}
}
