package slotnotarization

import (
	"sync"
	"time"

	"github.com/pkg/errors"

	"github.com/iotaledger/hive.go/runtime/event"
	"github.com/iotaledger/hive.go/runtime/module"
	"github.com/iotaledger/hive.go/runtime/options"
	"github.com/iotaledger/hive.go/runtime/workerpool"
	"github.com/iotaledger/hive.go/serializer/v2/serix"
	"github.com/iotaledger/iota-core/pkg/model"
	"github.com/iotaledger/iota-core/pkg/protocol/engine"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/attestation"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/blocks"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/notarization"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/therealledger"
	"github.com/iotaledger/iota-core/pkg/storage"
	iotago "github.com/iotaledger/iota.go/v4"
)

const (
	defaultMinSlotCommittableAge = 6
)

// Manager is the component that manages the slot commitments.
type Manager struct {
	events        *notarization.Events
	slotMutations *SlotMutations

	workers      *workerpool.Group
	errorHandler func(error)

	attestation attestation.Attestation
	ledger      therealledger.Ledger

	storage         *storage.Storage
	commitmentMutex sync.RWMutex

	acceptedTimeFunc          func() time.Time
	slotTimeProviderFunc      func() *iotago.SlotTimeProvider
	optsMinCommittableSlotAge iotago.SlotIndex

	module.Module
}

func NewProvider(opts ...options.Option[Manager]) module.Provider[*engine.Engine, notarization.Notarization] {
	return module.Provide(func(e *engine.Engine) notarization.Notarization {
		return options.Apply(&Manager{
			events:                    notarization.NewEvents(),
			workers:                   e.Workers.CreateGroup("NotarizationManager"),
			errorHandler:              e.ErrorHandler("notarization"),
			optsMinCommittableSlotAge: defaultMinSlotCommittableAge,
		}, opts,
			func(m *Manager) {
				m.slotTimeProviderFunc = func() *iotago.SlotTimeProvider {
					return e.API().SlotTimeProvider()
				}

				e.HookConstructed(func() {
					m.storage = e.Storage
					m.acceptedTimeFunc = e.Clock.RatifiedAccepted().Time

					m.ledger = e.Ledger
					m.attestation = e.Attestation

					wpBlocks := m.workers.CreatePool("Blocks", 1)           // Using just 1 worker to avoid contention
					wpCommitments := m.workers.CreatePool("Commitments", 1) // Using just 1 worker to avoid contention

					e.Events.BlockGadget.BlockRatifiedAccepted.Hook(func(block *blocks.Block) {
						if err := m.notarizeRatifiedAcceptedBlock(block); err != nil {
							m.errorHandler(errors.Wrapf(err, "failed to add accepted block %s to slot", block.ID()))
						}
					}, event.WithWorkerPool(wpBlocks))

					// Slots are committed whenever RatifiedATT advances, start committing only when bootstrapped.
					e.Events.Clock.RatifiedAcceptedTimeUpdated.Hook(m.tryCommitUntil, event.WithWorkerPool(wpCommitments))

					e.Events.Notarization.LinkTo(m.events)

					m.TriggerInitialized()
				})

				e.Storage.Settings().HookInitialized(func() {
					m.slotMutations = NewSlotMutations(e.SybilProtection.Accounts(), e.Storage.Settings().LatestCommitment().Index())
				})
			},
			(*Manager).TriggerConstructed,
			(*Manager).TriggerInitialized,
		)
	})
}

func (m *Manager) Shutdown() {
	m.TriggerStopped()
	m.workers.Shutdown()
}

// tryCommitUntil tries to create slot commitments until the new provided acceptance time.
func (m *Manager) tryCommitUntil(acceptanceTime time.Time) {
	m.commitmentMutex.Lock()
	defer m.commitmentMutex.Unlock()

	if index := m.slotTimeProviderFunc().IndexFromTime(acceptanceTime); index > m.storage.Settings().LatestCommitment().Index() {
		m.tryCommitSlotUntil(index)
	}
}

// IsBootstrapped returns if the Manager finished committing all pending slots up to the current acceptance time.
func (m *Manager) IsBootstrapped() bool {
	// If acceptance time is in slot 10, then the latest committable index is 3 (with optsMinCommittableSlotAge=6), because there are 6 full slots between slot 10 and slot 3.
	// All slots smaller than 4 are committable, so in order to check if slot 3 is committed it's necessary to do m.optsMinCommittableSlotAge-1,
	// otherwise we'd expect slot 4 to be committed in order to be fully committed, which is impossible.
	return m.storage.Settings().LatestCommitment().Index() >= m.slotTimeProviderFunc().IndexFromTime(m.acceptedTimeFunc())-m.optsMinCommittableSlotAge-1
}

func (m *Manager) notarizeRatifiedAcceptedBlock(block *blocks.Block) (err error) {
	if err = m.slotMutations.AddRatifiedAcceptedBlock(block); err != nil {
		return errors.Wrap(err, "failed to add accepted block to slot mutations")
	}

	m.attestation.AddAttestationFromBlock(block)

	return
}

// MinCommittableSlotAge returns the minimum age of a slot to be committable.
func (m *Manager) MinCommittableSlotAge() iotago.SlotIndex {
	return m.optsMinCommittableSlotAge
}

func (m *Manager) tryCommitSlotUntil(acceptedBlockIndex iotago.SlotIndex) {
	for i := m.storage.Settings().LatestCommitment().Index() + 1; i <= acceptedBlockIndex; i++ {
		if m.WasStopped() {
			break
		}

		if !m.isCommittable(i, acceptedBlockIndex) {
			return
		}

		if !m.createCommitment(i) {
			return
		}
	}
}

func (m *Manager) isCommittable(index, acceptedBlockIndex iotago.SlotIndex) bool {
	if acceptedBlockIndex < m.optsMinCommittableSlotAge {
		return false
	}

	return index < acceptedBlockIndex-m.optsMinCommittableSlotAge
}

func (m *Manager) createCommitment(index iotago.SlotIndex) (success bool) {
	latestCommitment := m.storage.Settings().LatestCommitment()
	if index != latestCommitment.Index()+1 {
		m.errorHandler(errors.Errorf("cannot create commitment for slot %d, latest commitment is for slot %d", index, latestCommitment.Index()))
		return false
	}

	// set createIfMissing to true to make sure that this is never nil. Will get evicted later on anyway.
	ratifiedAcceptedBlocks := m.slotMutations.RatifiedAcceptedBlocks(index, true)

	cumulativeWeight, attestations, err := m.attestation.Commit(index)
	if err != nil {
		m.errorHandler(errors.Wrap(err, "failed to commit attestations"))
		return false
	}

	stateRoot, mutationRoot, err := m.ledger.CommitSlot(index)
	if err != nil {
		m.errorHandler(errors.Wrap(err, "failed to commit ledger"))
		return false
	}

	newCommitment := iotago.NewCommitment(
		index,
		latestCommitment.ID(),
		iotago.NewRoots(
			iotago.Identifier(ratifiedAcceptedBlocks.Root()),
			mutationRoot,
			iotago.Identifier(attestations.Root()),
			stateRoot,
			iotago.Identifier(m.slotMutations.weights.Root()),
		).ID(),
		cumulativeWeight,
	)

	newModelCommitment, err := model.CommitmentFromCommitment(newCommitment, m.storage.Settings().API(), serix.WithValidation())
	if err != nil {
		return false
	}

	if err = m.storage.Settings().SetLatestCommitment(newModelCommitment); err != nil {
		m.errorHandler(errors.Wrap(err, "failed to set latest commitment"))
		return false
	}

	if err = m.storage.Commitments().Store(newModelCommitment); err != nil {
		m.errorHandler(errors.Wrap(err, "failed to store latest commitment"))
		return false
	}

	m.events.SlotCommitted.Trigger(&notarization.SlotCommittedDetails{
		Commitment:             newModelCommitment,
		RatifiedAcceptedBlocks: ratifiedAcceptedBlocks,
		ActiveValidatorsCount:  0,
	})

	if err = m.slotMutations.Evict(index); err != nil {
		m.errorHandler(errors.Wrapf(err, "failed to evict slotMutations at index: %d", index))
	}

	return true
}

func (m *Manager) PerformLocked(perform func(m notarization.Notarization)) {
	m.commitmentMutex.Lock()
	defer m.commitmentMutex.Unlock()
	perform(m)
}

var _ notarization.Notarization = new(Manager)
