package coatwindow

import (
	"errors"
	"fmt"
	"time"
)

var (
	ErrBatchNotFound        = errors.New("batch not found")
	ErrBatchClosed          = errors.New("batch is already closed")
	ErrBatchExpired         = errors.New("batch pot life has expired")
	ErrDuplicateApplication = errors.New("application id already exists")
	ErrInsufficientVolume   = errors.New("application exceeds remaining volume")
)

type validationError struct {
	message string
}

func (e *validationError) Error() string {
	return e.message
}

func invalidInput(message string) error {
	return &validationError{message: message}
}

type Outcome string

const (
	OutcomeFullyApplied           Outcome = "fully-applied"
	OutcomeExpiredWithRemainder   Outcome = "expired-with-remainder"
	OutcomeDiscardedWithRemainder Outcome = "discarded-with-remainder"
)

type Application struct {
	ID         string    `json:"id"`
	AreaLabel  string    `json:"areaLabel"`
	QuantityML int64     `json:"quantityMl"`
	AppliedAt  time.Time `json:"appliedAt"`
}

type BatchView struct {
	ID                string        `json:"id"`
	MaterialName      string        `json:"materialName"`
	StartingVolumeML  int64         `json:"startingVolumeMl"`
	MixedAt           time.Time     `json:"mixedAt"`
	ExpiresAt         time.Time     `json:"expiresAt"`
	Applications      []Application `json:"applications"`
	RemainingVolumeML int64         `json:"remainingVolumeMl"`
	ClosedAt          *time.Time    `json:"closedAt,omitempty"`
	Outcome           *Outcome      `json:"outcome,omitempty"`
	CloseNote         *string       `json:"closeNote,omitempty"`
}

type batch struct {
	id               string
	materialName     string
	startingVolumeML int64
	mixedAt          time.Time
	expiresAt        time.Time
	applications     []Application
	closedAt         *time.Time
	outcome          *Outcome
	closeNote        *string
}

func newBatch(id, materialName string, startingVolumeML int64, mixedAt time.Time, potLife time.Duration) batch {
	mixedAt = mixedAt.UTC()
	return batch{
		id:               id,
		materialName:     materialName,
		startingVolumeML: startingVolumeML,
		mixedAt:          mixedAt,
		expiresAt:        mixedAt.Add(potLife),
		applications:     make([]Application, 0),
	}
}

func (b batch) clone() batch {
	clone := b
	clone.applications = make([]Application, len(b.applications))
	copy(clone.applications, b.applications)
	if b.closedAt != nil {
		closedAt := *b.closedAt
		clone.closedAt = &closedAt
	}
	if b.outcome != nil {
		outcome := *b.outcome
		clone.outcome = &outcome
	}
	if b.closeNote != nil {
		note := *b.closeNote
		clone.closeNote = &note
	}
	return clone
}

func (b batch) remainingVolume() int64 {
	used := int64(0)
	for _, application := range b.applications {
		used += application.QuantityML
	}
	return b.startingVolumeML - used
}

func (b batch) applicationIDs() map[string]struct{} {
	ids := make(map[string]struct{}, len(b.applications))
	for _, application := range b.applications {
		ids[application.ID] = struct{}{}
	}
	return ids
}

func (b *batch) addApplication(application Application, now time.Time) error {
	if b.closedAt != nil {
		return ErrBatchClosed
	}
	if !now.Before(b.expiresAt) {
		return ErrBatchExpired
	}
	if application.ID == "" {
		return invalidInput("application id is required")
	}
	if application.AreaLabel == "" {
		return invalidInput("area label is required")
	}
	if application.QuantityML <= 0 {
		return invalidInput("quantityMl must be positive")
	}
	if _, exists := b.applicationIDs()[application.ID]; exists {
		return ErrDuplicateApplication
	}
	if application.QuantityML > b.remainingVolume() {
		return ErrInsufficientVolume
	}
	application.AppliedAt = now.UTC()
	b.applications = append(b.applications, application)
	return nil
}

func (b *batch) close(now time.Time, note *string) error {
	if b.closedAt != nil {
		return ErrBatchClosed
	}
	closedAt := now.UTC()
	b.closedAt = &closedAt
	remaining := b.remainingVolume()
	var outcome Outcome
	switch {
	case remaining == 0:
		outcome = OutcomeFullyApplied
	case !now.Before(b.expiresAt):
		outcome = OutcomeExpiredWithRemainder
	default:
		outcome = OutcomeDiscardedWithRemainder
	}
	b.outcome = &outcome
	if note != nil {
		copyNote := *note
		b.closeNote = &copyNote
	}
	return nil
}

func (b batch) view() BatchView {
	view := BatchView{
		ID:                b.id,
		MaterialName:      b.materialName,
		StartingVolumeML:  b.startingVolumeML,
		MixedAt:           b.mixedAt,
		ExpiresAt:         b.expiresAt,
		Applications:      make([]Application, len(b.applications)),
		RemainingVolumeML: b.remainingVolume(),
	}
	copy(view.Applications, b.applications)
	if b.closedAt != nil {
		closedAt := *b.closedAt
		view.ClosedAt = &closedAt
	}
	if b.outcome != nil {
		outcome := *b.outcome
		view.Outcome = &outcome
	}
	if b.closeNote != nil {
		note := *b.closeNote
		view.CloseNote = &note
	}
	return view
}

func validateBatch(b batch) error {
	if b.id == "" {
		return errors.New("batch id is required")
	}
	if b.materialName == "" {
		return errors.New("material name is required")
	}
	if b.startingVolumeML <= 0 {
		return errors.New("starting volume must be positive")
	}
	if !b.expiresAt.After(b.mixedAt) {
		return errors.New("expiry must be after mixing")
	}
	ids := make(map[string]struct{}, len(b.applications))
	used := int64(0)
	for _, application := range b.applications {
		if application.ID == "" || application.AreaLabel == "" || application.QuantityML <= 0 {
			return errors.New("invalid application")
		}
		if _, exists := ids[application.ID]; exists {
			return fmt.Errorf("duplicate application id %q", application.ID)
		}
		ids[application.ID] = struct{}{}
		if application.QuantityML > b.startingVolumeML-used {
			return errors.New("applications exceed starting volume")
		}
		used += application.QuantityML
	}
	if used > b.startingVolumeML {
		return errors.New("applications exceed starting volume")
	}
	if b.closedAt == nil && (b.outcome != nil || b.closeNote != nil) {
		return errors.New("open batch contains close state")
	}
	if b.closedAt != nil && b.outcome == nil {
		return errors.New("closed batch has no outcome")
	}
	return nil
}
