package coatwindow

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type batchRecord struct {
	ID               string        `json:"id"`
	MaterialName     string        `json:"materialName"`
	StartingVolumeML int64         `json:"startingVolumeMl"`
	MixedAt          time.Time     `json:"mixedAt"`
	ExpiresAt        time.Time     `json:"expiresAt"`
	Applications     []Application `json:"applications"`
	ClosedAt         *time.Time    `json:"closedAt,omitempty"`
	Outcome          *Outcome      `json:"outcome,omitempty"`
	CloseNote        *string       `json:"closeNote,omitempty"`
}

type ledgerRecord struct {
	Batches map[string]batchRecord `json:"batches"`
}

type LedgerOptions struct {
	Encode func(any) ([]byte, error)
	Sync   func(*os.File) error
	Rename func(string, string) error
}

type Ledger struct {
	path    string
	mu      sync.RWMutex
	batches map[string]batch
	encode  func(any) ([]byte, error)
	sync    func(*os.File) error
	rename  func(string, string) error
}

func NewLedger(path string) (*Ledger, error) {
	return NewLedgerWithOptions(path, LedgerOptions{})
}

func NewLedgerWithOptions(path string, options LedgerOptions) (*Ledger, error) {
	if path == "" {
		return nil, errors.New("ledger path is required")
	}
	ledger := &Ledger{
		path:    path,
		batches: make(map[string]batch),
		encode:  options.Encode,
		sync:    options.Sync,
		rename:  options.Rename,
	}
	if ledger.encode == nil {
		ledger.encode = func(value any) ([]byte, error) {
			return json.MarshalIndent(value, "", "  ")
		}
	}
	if ledger.sync == nil {
		ledger.sync = func(file *os.File) error { return file.Sync() }
	}
	if ledger.rename == nil {
		ledger.rename = os.Rename
	}
	if err := ledger.load(); err != nil {
		return nil, err
	}
	return ledger, nil
}

func (l *Ledger) load() error {
	data, err := os.ReadFile(l.path)
	if errors.Is(err, os.ErrNotExist) {
		if _, statErr := os.Stat(filepath.Dir(l.path)); statErr != nil {
			return statErr
		}
		return nil
	}
	if err != nil {
		return err
	}
	var file ledgerRecord
	if err := json.Unmarshal(data, &file); err != nil {
		return err
	}
	for id, record := range file.Batches {
		b := batch{
			id:               record.ID,
			materialName:     record.MaterialName,
			startingVolumeML: record.StartingVolumeML,
			mixedAt:          record.MixedAt,
			expiresAt:        record.ExpiresAt,
			applications:     make([]Application, len(record.Applications)),
			closedAt:         record.ClosedAt,
			outcome:          record.Outcome,
			closeNote:        record.CloseNote,
		}
		copy(b.applications, record.Applications)
		if b.id != id {
			return errors.New("ledger key does not match batch id")
		}
		if err := validateBatch(b); err != nil {
			return err
		}
		l.batches[id] = b
	}
	return nil
}

func (l *Ledger) Add(b batch) error {
	if err := validateBatch(b); err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, exists := l.batches[b.id]; exists {
		return errors.New("batch id already exists")
	}
	next := l.copyBatches()
	next[b.id] = b.clone()
	if err := l.save(next); err != nil {
		return err
	}
	l.batches = next
	return nil
}

func (l *Ledger) Update(id string, update func(*batch) error) (batch, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	current, exists := l.batches[id]
	if !exists {
		return batch{}, ErrBatchNotFound
	}
	candidate := current.clone()
	if err := update(&candidate); err != nil {
		return batch{}, err
	}
	if err := validateBatch(candidate); err != nil {
		return batch{}, err
	}
	next := l.copyBatches()
	next[id] = candidate
	if err := l.save(next); err != nil {
		return batch{}, err
	}
	l.batches = next
	return candidate.clone(), nil
}

func (l *Ledger) Get(id string) (batch, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	b, exists := l.batches[id]
	if !exists {
		return batch{}, ErrBatchNotFound
	}
	return b.clone(), nil
}

func (l *Ledger) copyBatches() map[string]batch {
	next := make(map[string]batch, len(l.batches))
	for id, b := range l.batches {
		next[id] = b.clone()
	}
	return next
}

func (l *Ledger) save(batches map[string]batch) error {
	file := ledgerRecord{Batches: make(map[string]batchRecord, len(batches))}
	for id, b := range batches {
		file.Batches[id] = batchRecord{
			ID:               b.id,
			MaterialName:     b.materialName,
			StartingVolumeML: b.startingVolumeML,
			MixedAt:          b.mixedAt,
			ExpiresAt:        b.expiresAt,
			Applications:     make([]Application, len(b.applications)),
			ClosedAt:         b.closedAt,
			Outcome:          b.outcome,
			CloseNote:        b.closeNote,
		}
		copy(file.Batches[id].Applications, b.applications)
	}
	data, err := l.encode(file)
	if err != nil {
		return err
	}
	directory := filepath.Dir(l.path)
	temporary, err := os.CreateTemp(directory, "."+filepath.Base(l.path)+".tmp-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := l.sync(temporary); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := l.rename(temporaryPath, l.path); err != nil {
		return err
	}
	return nil
}
