package mempoolv1

import (
	"github.com/iotaledger/hive.go/ds/reactive"
	"github.com/iotaledger/hive.go/runtime/promise"
	iotago "github.com/iotaledger/iota.go/v4"
)

// inclusionFlags represents important flags and events that relate to the inclusion of an entity in the distributed ledger.
type inclusionFlags struct {
	// accepted gets triggered when the entity gets marked as accepted.
	accepted reactive.Variable[bool]

	// committed gets set when the entity gets marked as committed.
	committed reactive.Variable[iotago.SlotIndex]

	// rejected gets triggered when the entity gets marked as rejected.
	rejected *promise.Event

	// orphaned gets set when the entity gets marked as orphaned.
	orphaned reactive.Variable[iotago.SlotIndex]
}

// newInclusionFlags creates a new inclusionFlags instance.
func newInclusionFlags() *inclusionFlags {
	return &inclusionFlags{
		accepted:  reactive.NewVariable[bool](),
		committed: reactive.NewVariable[iotago.SlotIndex](),
		rejected:  promise.NewEvent(),
		// Make sure the oldest orphaned index doesn't get overridden by newer TX spending the orphaned conflit further.
		orphaned: reactive.NewVariable[iotago.SlotIndex](func(currentValue, newValue iotago.SlotIndex) iotago.SlotIndex {
			if currentValue != 0 {
				return currentValue
			}

			return newValue
		}),
	}
}

func (s *inclusionFlags) IsPending() bool {
	return !s.IsAccepted() && !s.IsRejected()
}

// IsAccepted returns true if the entity was accepted.
func (s *inclusionFlags) IsAccepted() bool {
	return s.accepted.Get()
}

// OnAccepted registers a callback that gets triggered when the entity gets accepted.
func (s *inclusionFlags) OnAccepted(callback func()) {
	s.accepted.OnUpdate(func(wasAccepted, isAccepted bool) {
		if isAccepted && !wasAccepted {
			callback()
		}
	})
}

// OnPending registers a callback that gets triggered when the entity gets pending.
func (s *inclusionFlags) OnPending(callback func()) {
	s.accepted.OnUpdate(func(wasAccepted, isAccepted bool) {
		if !isAccepted && wasAccepted {
			callback()
		}
	})
}

// IsRejected returns true if the entity was rejected.
func (s *inclusionFlags) IsRejected() bool {
	return s.rejected.WasTriggered()
}

// OnRejected registers a callback that gets triggered when the entity gets rejected.
func (s *inclusionFlags) OnRejected(callback func()) {
	s.rejected.OnTrigger(callback)
}

// IsCommitted returns true if the entity was committed.
func (s *inclusionFlags) IsCommitted() (slot iotago.SlotIndex, isCommitted bool) {
	return s.committed.Get(), s.committed.Get() != 0
}

// OnCommitted registers a callback that gets triggered when the entity gets committed.
func (s *inclusionFlags) OnCommitted(callback func(slot iotago.SlotIndex)) {
	s.committed.OnUpdate(func(_, newValue iotago.SlotIndex) {
		callback(newValue)
	})
}

// IsOrphaned returns true if the entity was orphaned.
func (s *inclusionFlags) IsOrphaned() (slot iotago.SlotIndex, isOrphaned bool) {
	return s.orphaned.Get(), s.orphaned.Get() != 0
}

// OnOrphaned registers a callback that gets triggered when the entity gets orphaned.
func (s *inclusionFlags) OnOrphaned(callback func(slot iotago.SlotIndex)) {
	s.orphaned.OnUpdate(func(_, newValue iotago.SlotIndex) {
		callback(newValue)
	})
}

// setAccepted marks the entity as accepted.
func (s *inclusionFlags) setAccepted() {
	s.accepted.Set(true)
}

// setPending marks the entity as pending.
func (s *inclusionFlags) setPending() {
	s.accepted.Set(false)
}

// setRejected marks the entity as rejected.
func (s *inclusionFlags) setRejected() {
	s.rejected.Trigger()
}

// setCommitted marks the entity as committed.
func (s *inclusionFlags) setCommitted(slot iotago.SlotIndex) {
	s.committed.Set(slot)
}

// setOrphaned marks the entity as orphaned.
func (s *inclusionFlags) setOrphaned(slot iotago.SlotIndex) {
	s.orphaned.Set(slot)
}
