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
	path     string
	mu       sync.RWMutex
	commitMu *sync.Mutex
	batches  map[string]batch
	encode   func(any) ([]byte, error)
	sync     func(*os.File) error
	rename   func(string, string) error
}

var ledgerCommitLocks sync.Map

func commitLockFor(path string) *sync.Mutex {
	key, err := filepath.Abs(path)
	if err != nil {
		key = filepath.Clean(path)
	}
	lock, _ := ledgerCommitLocks.LoadOrStore(key, &sync.Mutex{})
	return lock.(*sync.Mutex)
}

func NewLedger(path string) (*Ledger, error) {
	return NewLedgerWithOptions(path, LedgerOptions{})
}

func NewLedgerWithOptions(path string, options LedgerOptions) (*Ledger, error) {
	if path == "" {
		return nil, errors.New("ledger path is required")
	}
	ledger := &Ledger{
		path:     path,
		commitMu: commitLockFor(path),
		batches:  make(map[string]batch),
		encode:   options.Encode,
		sync:     options.Sync,
		rename:   options.Rename,
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
	batches, err := l.readBatches()
	if err != nil {
		return err
	}
	l.batches = batches
	return nil
}

func (l *Ledger) readBatches() (map[string]batch, error) {
	data, err := os.ReadFile(l.path)
	if errors.Is(err, os.ErrNotExist) {
		return make(map[string]batch), nil
	}
	if err != nil {
		return nil, err
	}
	var file ledgerRecord
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, err
	}
	batches := make(map[string]batch, len(file.Batches))
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
			return nil, errors.New("ledger key does not match batch id")
		}
		if err := validateBatch(b); err != nil {
			return nil, err
		}
		batches[id] = b
	}
	return batches, nil
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
	committed, err := l.save(next, b.id, true)
	if err != nil {
		return err
	}
	l.batches = committed
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
	committed, err := l.save(next, id, false)
	if err != nil {
		return batch{}, err
	}
	l.batches = committed
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

func (l *Ledger) save(batches map[string]batch, changedID string, adding bool) (map[string]batch, error) {
	directory := filepath.Dir(l.path)
	temporary, err := os.CreateTemp(directory, "."+filepath.Base(l.path)+".tmp-*")
	if err != nil {
		return nil, err
	}
	temporaryPath := temporary.Name()
	preparedPath := temporaryPath + ".prepared"
	defer os.Remove(temporaryPath)
	defer os.Remove(preparedPath)
	if err := temporary.Close(); err != nil {
		return nil, err
	}
	if err := l.rename(temporaryPath, preparedPath); err != nil {
		return nil, err
	}

	l.commitMu.Lock()
	defer l.commitMu.Unlock()
	committed, err := l.readBatches()
	if err != nil {
		return nil, err
	}
	if adding {
		if _, exists := committed[changedID]; exists {
			return nil, errors.New("batch id already exists")
		}
	} else if _, exists := committed[changedID]; !exists {
		return nil, ErrBatchNotFound
	}
	committed[changedID] = batches[changedID].clone()

	data, err := l.encodeLedger(committed)
	if err != nil {
		return nil, err
	}
	prepared, err := os.OpenFile(preparedPath, os.O_WRONLY|os.O_TRUNC, 0)
	if err != nil {
		return nil, err
	}
	if _, err := prepared.Write(data); err != nil {
		_ = prepared.Close()
		return nil, err
	}
	if err := l.sync(prepared); err != nil {
		_ = prepared.Close()
		return nil, err
	}
	if err := prepared.Close(); err != nil {
		return nil, err
	}
	if err := os.Rename(preparedPath, l.path); err != nil {
		return nil, err
	}
	return committed, nil
}

func (l *Ledger) encodeLedger(batches map[string]batch) ([]byte, error) {
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
	return l.encode(file)
}
