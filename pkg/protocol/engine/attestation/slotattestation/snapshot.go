package slotattestation

import (
	"io"

	"github.com/pkg/errors"

	"github.com/iotaledger/hive.go/lo"
	"github.com/iotaledger/hive.go/serializer/v2/stream"
	iotago "github.com/iotaledger/iota.go/v4"
)

func (m *Manager) Import(reader io.ReadSeeker) error {
	m.commitmentMutex.Lock()
	defer m.commitmentMutex.Unlock()

	// Read slot count.
	count, err := stream.Read[iotago.SlotIndex](reader)
	if err != nil {
		return errors.Wrap(err, "failed to read slot")
	}

	for i := 0; i < int(count); i++ {
		// Read slot index.
		slotIndex, err := stream.Read[iotago.SlotIndex](reader)
		if err != nil {
			return errors.Wrap(err, "failed to read slot")
		}

		// Read attestations.
		var attestations []*iotago.Attestation
		if err = stream.ReadCollection(reader, func(i int) error {
			importedAttestation := new(iotago.Attestation)
			if err = stream.ReadSerializable(reader, importedAttestation); err != nil {
				return errors.Wrapf(err, "failed to read attestation %d", i)
			}

			attestations = append(attestations, importedAttestation)

			return nil
		}); err != nil {
			return errors.Wrapf(err, "failed to import attestations for slot %d", slotIndex)
		}

		cutoffIndex, isValid := m.computeAttestationCommitmentOffset(m.lastCommittedSlot)
		if !isValid {
			return nil
		}

		if slotIndex >= cutoffIndex {
			for _, a := range attestations {
				m.applyToPendingAttestations(a, cutoffIndex)
			}
		} else {
			// We should never be able to import attestations for a slot that is older than the attestation commitment offset.
			panic("commitment not aligned with attestation")
		}
	}

	m.TriggerInitialized()

	return nil
}

func (m *Manager) Export(writer io.WriteSeeker, targetSlot iotago.SlotIndex) error {
	m.commitmentMutex.RLock()
	defer m.commitmentMutex.RUnlock()

	if targetSlot > m.lastCommittedSlot {
		return errors.Errorf("slot %d is newer than last committed slot %d", targetSlot, m.lastCommittedSlot)
	}

	attestationSlotIndex, isValid := m.computeAttestationCommitmentOffset(m.lastCommittedSlot)
	if !isValid {
		if err := stream.Write(writer, uint64(0)); err != nil {
			return errors.Wrap(err, "failed to write slot count")
		}

		return nil
	}

	// Write slot count.
	start := lo.Max(targetSlot-m.attestationCommitmentOffset, 0)
	if err := stream.Write(writer, uint64(targetSlot-start+1)); err != nil {
		return errors.Wrap(err, "failed to write slot count")
	}

	for i := start; i <= targetSlot; i++ {
		var attestations []*iotago.Attestation
		if i < attestationSlotIndex {
			// Need to get attestations from storage.
			attestationsStorage, err := m.adsMapStorage(i)
			if err != nil {
				return errors.Wrapf(err, "failed to get attestations of slot %d", i)
			}
			err = attestationsStorage.Stream(func(key iotago.AccountID, value *iotago.Attestation) bool {
				attestations = append(attestations, value)
				return true
			})
			if err != nil {
				return errors.Wrapf(err, "failed to stream attestations of slot %d", i)
			}
		} else {
			// Need to get attestations from tracker.
			attestations = m.determineAttestationsFromWindow(i)
		}

		// Write slot index.
		if err := stream.Write(writer, uint64(i)); err != nil {
			return errors.Wrapf(err, "failed to write slot %d", i)
		}

		// Write attestations.
		if err := stream.WriteCollection(writer, func() (uint64, error) {
			for _, a := range attestations {
				if writeErr := stream.WriteSerializable(writer, a); writeErr != nil {
					return 0, errors.Wrapf(writeErr, "failed to write attestation %v", a)
				}
			}

			return uint64(len(attestations)), nil
		}); err != nil {
			return errors.Wrapf(err, "failed to write attestations of slot %d", i)
		}
	}

	return nil
}