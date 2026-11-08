package observation

import (
	"context"
	"errors"
	"fmt"
	"os"
	"ovpn/internal/app/dto"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestReadOnlyMissingStateAndExclusiveOwner(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "absent")
	if _, err := Open(dir, false); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("missing history was not empty")
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("read created storage")
	}
	unlock, err := Lock(dir)
	if err != nil {
		t.Fatal(err)
	}
	if release, err := Lock(dir); err == nil {
		release()
		t.Fatal("second owner acquired lock")
	}
	unlock()
	release, err := Lock(dir)
	if err != nil {
		t.Fatal(err)
	}
	release()
}

func TestAttemptReservationSurvivesRestartAndConcurrentCalls(t *testing.T) {
	dir := t.TempDir()
	st, err := Open(dir, true)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	now := time.Now()
	var accepted atomic.Int64
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			ok, err := st.Reserve(ctx, "native/target", now, 3)
			if err != nil {
				t.Error(err)
			}
			if ok {
				accepted.Add(1)
			}
		})
	}
	wg.Wait()
	if accepted.Load() != 3 {
		t.Fatalf("charged %d attempts", accepted.Load())
	}
	_ = st.Close()
	st, err = Open(dir, true)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if ok, err := st.Reserve(ctx, "native/target", now, 3); err != nil || ok {
		t.Fatal("restart reset budget")
	}
	if ok, err := st.Reserve(ctx, "native/target", now.Add(24*time.Hour), 3); err != nil || !ok {
		t.Fatal("next UTC day did not open allowance")
	}
}

func TestHistoryRetentionAndLatestProjection(t *testing.T) {
	ctx := context.Background()
	st, err := Open(t.TempDir(), true)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	tx, err := st.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for i := range HistoryLimit + 10 {
		if _, err := tx.Exec(`INSERT INTO samples(sample_key,measured_at,payload) VALUES(?,?,?)`, "old", time.Now().UTC().Format(time.RFC3339), `{"outcome":"failed"}`); err != nil {
			t.Fatal(i, err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := st.Add(ctx, "new", dto.Observation{Outcome: "passed", MeasuredAt: time.Now().UTC().Format(time.RFC3339)}); err != nil {
		t.Fatal(err)
	}
	count, err := st.Count(ctx)
	if err != nil || count != HistoryLimit {
		t.Fatal("history cap not enforced", count, err)
	}
	latest, err := st.Samples(ctx, true)
	if err != nil || len(latest) != 2 || latest[0].Outcome != "passed" {
		t.Fatal("latest per key projection incorrect")
	}
	history, err := st.Samples(ctx, false)
	if err != nil || len(history) != 50 {
		t.Fatal("unbounded history response")
	}
}

func TestExpiredHistoryIsHiddenWithoutStartingAWriter(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st, err := Open(dir, true)
	if err != nil {
		t.Fatal(err)
	}
	_, err = st.db.Exec(`INSERT INTO samples(sample_key,measured_at,payload) VALUES(?,?,?)`, "expired", time.Now().UTC().Add(-8*24*time.Hour).Format(time.RFC3339), `{"outcome":"passed"}`)
	if err != nil {
		t.Fatal(err)
	}
	_ = st.Close()
	st, err = Open(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	for _, latest := range []bool{true, false} {
		items, err := st.Samples(ctx, latest)
		if err != nil || len(items) != 0 {
			t.Fatal("expired evidence remained visible", err)
		}
	}
	if count, err := st.Count(ctx); err != nil || count != 0 {
		t.Fatal("expired evidence remained counted", err)
	}
}

func TestJobAndIncidentStorageIsBoundedWithoutSamples(t *testing.T) {
	ctx := context.Background()
	st, err := Open(t.TempDir(), true)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	for _, table := range []string{"jobs", "incidents"} {
		for i := range 105 {
			if err := st.Save(ctx, table, fmt.Sprint(i), dto.ObservationJob{State: "completed"}); err != nil {
				t.Fatal(err)
			}
		}
		var count int
		if err := st.db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&count); err != nil || count != 100 {
			t.Fatal("unbounded metadata storage", count, err)
		}
	}
}
