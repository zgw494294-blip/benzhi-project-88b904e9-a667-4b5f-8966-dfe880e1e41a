package coatwindow

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestServiceWorkflowPreservesOrderAndDefensiveCopies(t *testing.T) {
	clock := time.Date(2026, time.August, 17, 8, 0, 0, 0, time.UTC)
	ledger := newTestLedger(t)
	service := NewServiceWithOptions(ledger, ServiceOptions{
		Now:   func() time.Time { return clock },
		NewID: func() (string, error) { return "batch-1", nil },
	})
	opened, err := service.OpenBatch("Epoxy coat", 100, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if opened.ClosedAt != nil || opened.Outcome != nil || opened.CloseNote != nil {
		t.Fatal("open batch exposed close state")
	}
	if _, err := service.Apply(opened.ID, "one", "bow", 30); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Apply(opened.ID, "two", "stern", 20); err != nil {
		t.Fatal(err)
	}
	view, err := service.Get(opened.ID)
	if err != nil {
		t.Fatal(err)
	}
	view.Applications[0].AreaLabel = "changed outside service"
	view.Applications = append(view.Applications, Application{ID: "outside"})
	again, err := service.Get(opened.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Applications) != 2 || again.Applications[0].AreaLabel != "bow" || again.Applications[1].ID != "two" {
		t.Fatalf("application collection was not defensive: %#v", again.Applications)
	}
}

func TestServiceRejectsOverspendWithoutPartialChangeAndClosesImmutably(t *testing.T) {
	clock := time.Date(2026, time.August, 17, 9, 0, 0, 0, time.UTC)
	ledger := newTestLedger(t)
	service := NewServiceWithOptions(ledger, ServiceOptions{
		Now:   func() time.Time { return clock },
		NewID: func() (string, error) { return "batch-2", nil },
	})
	opened, err := service.OpenBatch("Polyurethane", 100, 30*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Apply(opened.ID, "coat", "deck", 80); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Apply(opened.ID, "too-much", "deck", 21); !errors.Is(err, ErrInsufficientVolume) {
		t.Fatalf("overspend error = %v", err)
	}
	beforeClose, err := service.Get(opened.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(beforeClose.Applications) != 1 || beforeClose.RemainingVolumeML != 20 {
		t.Fatalf("overspend changed state: %#v", beforeClose)
	}
	note := "discarded remainder"
	closed, err := service.Close(opened.ID, &note)
	if err != nil {
		t.Fatal(err)
	}
	if closed.Outcome == nil || *closed.Outcome != OutcomeDiscardedWithRemainder || closed.CloseNote == nil || *closed.CloseNote != note {
		t.Fatalf("unexpected close result: %#v", closed)
	}
	if _, err := service.Close(opened.ID, nil); !errors.Is(err, ErrBatchClosed) {
		t.Fatalf("second close error = %v", err)
	}
	if _, err := service.Apply(opened.ID, "late", "deck", 1); !errors.Is(err, ErrBatchClosed) {
		t.Fatalf("late application error = %v", err)
	}
}

func TestServiceDerivesExpiredAndFullyAppliedOutcomes(t *testing.T) {
	clock := time.Date(2026, time.August, 17, 10, 0, 0, 0, time.UTC)
	ledger := newTestLedger(t)
	ids := []string{"expired", "full"}
	service := NewServiceWithOptions(ledger, ServiceOptions{
		Now: func() time.Time { return clock },
		NewID: func() (string, error) {
			id := ids[0]
			ids = ids[1:]
			return id, nil
		},
	})
	expired, err := service.OpenBatch("Lacquer", 100, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(2 * time.Minute)
	closedExpired, err := service.Close(expired.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if closedExpired.Outcome == nil || *closedExpired.Outcome != OutcomeExpiredWithRemainder {
		t.Fatalf("expired outcome = %v", closedExpired.Outcome)
	}
	clock = time.Date(2026, time.August, 17, 11, 0, 0, 0, time.UTC)
	full, err := service.OpenBatch("Lacquer", 40, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Apply(full.ID, "all", "trim", 40); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(2 * time.Minute)
	closedFull, err := service.Close(full.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if closedFull.Outcome == nil || *closedFull.Outcome != OutcomeFullyApplied {
		t.Fatalf("fully applied outcome = %v", closedFull.Outcome)
	}
}

func TestLedgerFailureLeavesStateAndTemporaryFileClean(t *testing.T) {
	tests := []struct {
		name   string
		option func() LedgerOptions
		expect string
	}{
		{name: "encode", option: func() LedgerOptions {
			return LedgerOptions{Encode: func(any) ([]byte, error) { return nil, errors.New("encode failed") }}
		}, expect: "encode failed"},
		{name: "sync", option: func() LedgerOptions {
			return LedgerOptions{Sync: func(*os.File) error { return errors.New("sync failed") }}
		}, expect: "sync failed"},
		{name: "rename", option: func() LedgerOptions {
			return LedgerOptions{Rename: func(string, string) error { return errors.New("rename failed") }}
		}, expect: "rename failed"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			path := filepath.Join(directory, "ledger.json")
			ledger, err := NewLedgerWithOptions(path, test.option())
			if err != nil {
				t.Fatal(err)
			}
			batch := newBatch("batch", "Coat", 10, time.Now(), time.Hour)
			if err := ledger.Add(batch); err == nil || err.Error() != test.expect {
				t.Fatalf("save error = %v", err)
			}
			if _, err := ledger.Get("batch"); !errors.Is(err, ErrBatchNotFound) {
				t.Fatalf("failed save changed visible state: %v", err)
			}
			matches, err := filepath.Glob(filepath.Join(directory, ".ledger.json.tmp-*"))
			if err != nil {
				t.Fatal(err)
			}
			if len(matches) != 0 {
				t.Fatalf("temporary files remain: %v", matches)
			}
		})
	}
}

func newTestLedger(t *testing.T) *Ledger {
	t.Helper()
	ledger, err := NewLedger(filepath.Join(t.TempDir(), "ledger.json"))
	if err != nil {
		t.Fatal(err)
	}
	return ledger
}
