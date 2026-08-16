package coatwindow

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
	"time"
)

type ServiceOptions struct {
	Now   func() time.Time
	NewID func() (string, error)
}

type Service struct {
	ledger *Ledger
	now    func() time.Time
	newID  func() (string, error)
}

func NewService(ledger *Ledger) *Service {
	return NewServiceWithOptions(ledger, ServiceOptions{})
}

func NewServiceWithOptions(ledger *Ledger, options ServiceOptions) *Service {
	if options.Now == nil {
		options.Now = func() time.Time { return time.Now().UTC() }
	}
	if options.NewID == nil {
		options.NewID = randomID
	}
	return &Service{ledger: ledger, now: options.Now, newID: options.NewID}
}

func (s *Service) OpenBatch(materialName string, startingVolumeML int64, potLife time.Duration) (BatchView, error) {
	materialName = strings.TrimSpace(materialName)
	if materialName == "" {
		return BatchView{}, invalidInput("material name is required")
	}
	if startingVolumeML <= 0 {
		return BatchView{}, invalidInput("starting volume must be positive")
	}
	if potLife <= 0 {
		return BatchView{}, invalidInput("pot life must be positive")
	}
	id, err := s.newID()
	if err != nil {
		return BatchView{}, err
	}
	mixedAt := s.now().UTC()
	b := newBatch(id, materialName, startingVolumeML, mixedAt, potLife)
	if err := s.ledger.Add(b); err != nil {
		return BatchView{}, err
	}
	return b.view(), nil
}

func (s *Service) Apply(batchID string, applicationID string, areaLabel string, quantityML int64) (BatchView, error) {
	updated, err := s.ledger.Update(batchID, func(b *batch) error {
		return b.addApplication(Application{
			ID:         strings.TrimSpace(applicationID),
			AreaLabel:  strings.TrimSpace(areaLabel),
			QuantityML: quantityML,
		}, s.now())
	})
	if err != nil {
		return BatchView{}, err
	}
	return updated.view(), nil
}

func (s *Service) Close(batchID string, note *string) (BatchView, error) {
	updated, err := s.ledger.Update(batchID, func(b *batch) error {
		return b.close(s.now(), note)
	})
	if err != nil {
		return BatchView{}, err
	}
	return updated.view(), nil
}

func (s *Service) Get(batchID string) (BatchView, error) {
	b, err := s.ledger.Get(batchID)
	if err != nil {
		return BatchView{}, err
	}
	return b.view(), nil
}

func randomID() (string, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return "batch-" + hex.EncodeToString(bytes[:]), nil
}
