package storage

import (
	"github.com/iotaledger/hive.go/ierrors"
	"github.com/iotaledger/hive.go/lo"
	"github.com/iotaledger/iota-core/pkg/storage/database"
	iotago "github.com/iotaledger/iota.go/v4"
)

func (s *Storage) setIsPruning(value bool) {
	s.statusLock.Lock()
	s.isPruning = value
	s.statusLock.Unlock()
}

func (s *Storage) IsPruning() bool {
	s.statusLock.RLock()
	defer s.statusLock.RUnlock()

	return s.isPruning
}

func (s *Storage) LastPrunedEpoch() (index iotago.EpochIndex, hasPruned bool) {
	s.pruningLock.RLock()
	defer s.pruningLock.RUnlock()

	return s.lastPrunedEpoch.Index()
}

func (s *Storage) TryPrune() error {
	s.pruningLock.Lock()
	defer s.pruningLock.Unlock()

	// TODO: This should be called whenever a slot is accepted/finalized.
	// It should adhere to the default pruningDelay, whereas the others might not need to.

	// No need to prune.
	// if epoch < s.optsPruningDelay {
	// 	return database.ErrNoPruningNeeded
	// }

	return nil
}

// PruneByEpochIndex prunes the database until the given epoch. It returns an error if the epoch is too old or too new.
// It is to be called by the user e.g. via the WebAPI.
func (s *Storage) PruneByEpochIndex(epoch iotago.EpochIndex) error {
	s.pruningLock.Lock()
	defer s.pruningLock.Unlock()

	// Make sure epoch is not too recent or not yet finalized.
	latestPrunableEpoch := s.latestPrunableEpoch()
	if epoch > latestPrunableEpoch {
		return ierrors.Errorf("epoch %d is too new, latest prunable epoch is %d", epoch, latestPrunableEpoch)
	}

	// Make sure epoch is not already pruned.
	start, canPrune := s.getPruningStart(epoch)
	if !canPrune {
		return ierrors.Errorf("epoch %d is too old, last pruned epoch is %d", epoch, lo.Return1(s.lastPrunedEpoch.Index()))
	}

	if err := s.pruneUntilEpoch(start, epoch, 0); err != nil {
		return ierrors.Wrapf(err, "failed to prune from epoch %d to %d", start, epoch)
	}

	return nil
}

func (s *Storage) PruneByDepth(depth iotago.EpochIndex) (firstPruned, lastPruned iotago.EpochIndex, err error) {
	s.pruningLock.Lock()
	defer s.pruningLock.Unlock()

	if depth == 0 {
		return 0, 0, database.ErrNoPruningNeeded
	}

	latestPrunableEpoch := s.latestPrunableEpoch()
	if depth > latestPrunableEpoch {
		return 0, 0, ierrors.Errorf("depth %d is too big, latest prunable epoch is %d", depth, latestPrunableEpoch)
	}

	// We need to do (depth-1) because latestPrunableEpoch is already making sure that we keep at least one full epoch.
	end := latestPrunableEpoch - (depth - 1)

	// Make sure epoch is not already pruned.
	start, canPrune := s.getPruningStart(end)
	if !canPrune {
		return 0, 0, ierrors.Wrapf(database.ErrEpochPruned, "depth %d is too big, want to prune until %d but pruned epoch is already %d", depth, end, lo.Return1(s.lastPrunedEpoch.Index()))
	}

	if err := s.pruneUntilEpoch(start, end, depth); err != nil {
		return 0, 0, ierrors.Wrapf(err, "failed to prune from epoch %d to %d", start, end)
	}

	return start, end, nil
}

func (s *Storage) PruneBySize(targetSizeBytes ...int64) error {
	if !s.optPruningSizeEnabled && len(targetSizeBytes) == 0 {
		// pruning by size deactivated
		return database.ErrNoPruningNeeded
	}

	s.pruningLock.Lock()
	defer s.pruningLock.Unlock()

	s.setIsPruning(true)
	defer s.setIsPruning(false)

	targetDatabaseSizeBytes := s.optPruningSizeMaxTargetSizeBytes
	if len(targetSizeBytes) > 0 {
		targetDatabaseSizeBytes = targetSizeBytes[0]
	}

	currentSize := s.Size()
	// No need to prune. The database is already smaller than the target size.
	if targetDatabaseSizeBytes < 0 || currentSize < targetDatabaseSizeBytes {
		return database.ErrNoPruningNeeded
	}

	latestFinalizedSlot := s.Settings().LatestFinalizedSlot()
	latestEpoch := s.Settings().APIProvider().APIForSlot(latestFinalizedSlot).TimeProvider().EpochFromSlot(latestFinalizedSlot)
	bytesToPrune := currentSize - int64(float64(targetDatabaseSizeBytes)*s.optPruningSizeThresholdPercentage)
	targetEpoch, err := s.epochToPrunedBySize(bytesToPrune, latestEpoch)
	if err != nil {
		s.errorHandler(err)
		return err
	}

	s.pruneUntilEpoch(0, targetEpoch, 0)

	return nil
	// Note: what if permanent is too big -> log error?
}

func (s *Storage) getPruningStart(epoch iotago.EpochIndex) (iotago.EpochIndex, bool) {
	lastPrunedEpoch, hasPruned := s.lastPrunedEpoch.Index()
	if hasPruned && epoch <= lastPrunedEpoch {
		return 0, false
	}

	return s.lastPrunedEpoch.NextIndex(), true
}

func (s *Storage) epochToPrunedBySize(targetSize int64, latestFinalizedEpoch iotago.EpochIndex) (iotago.EpochIndex, error) {
	// 	lastPrunedEpoch := lo.Return1(p.prunableSlotStore.LastPrunedEpoch())
	// 	if latestFinalizedEpoch < p.defaultPruningDelay {
	// 		return 0, database.ErrNoPruningNeeded
	// 	}
	//
	// 	var sum int64
	// 	for i := lastPrunedEpoch + 1; i <= latestFinalizedEpoch-p.defaultPruningDelay; i++ {
	// 		bucketSize, err := p.prunableSlotStore.BucketSize(i)
	// 		if err != nil {
	// 			return 0, ierrors.Wrapf(err, "failed to get bucket size in EpochToPrunedBasedOnSize")
	// 		}
	// 		// add 10% for semiPermanentDB size estimation, it would be too heavy to estimate semiPermanentDB.
	// 		// we can tune this number later
	// 		sum += int64(float64(bucketSize) * 1.1)
	//
	// 		if sum >= targetSize {
	// 			return i + p.defaultPruningDelay, nil
	// 		}
	// 	}
	//
	// 	if sum >= targetSize {
	// 		return latestFinalizedEpoch, nil
	// 	}
	//
	// 	// TODO: do we return error here, or prune as much as we could
	return 0, database.ErrNotEnoughHistory
}

func (s *Storage) latestPrunableEpoch() iotago.EpochIndex {
	latestFinalizedSlot := s.Settings().LatestFinalizedSlot()
	currentFinalizedEpoch := s.Settings().APIProvider().APIForSlot(latestFinalizedSlot).TimeProvider().EpochFromSlot(latestFinalizedSlot)

	// We always want at least 1 full epoch of history. Thus, the latest prunable epoch is the current finalized epoch - 2.
	if currentFinalizedEpoch < 2 {
		return 0
	}

	return currentFinalizedEpoch - 2
}

// PruneUntilEpoch prunes the database until the given epoch.
// The caller needs to make sure that the start and target epoch take into account the specified pruning delay.
func (s *Storage) pruneUntilEpoch(start iotago.EpochIndex, target iotago.EpochIndex, pruningDelay iotago.EpochIndex) error {
	s.setIsPruning(true)
	defer s.setIsPruning(false)

	for currentIndex := start; currentIndex <= target; currentIndex++ {
		if err := s.prunable.Prune(currentIndex, pruningDelay); err != nil {
			return ierrors.Wrapf(err, "failed to prune epoch in prunable %d", currentIndex)
		}

		if err := s.permanent.PruneUTXOLedger(currentIndex); err != nil {
			return ierrors.Wrapf(err, "failed to prune epoch in permanent %d", currentIndex)
		}
	}

	s.lastPrunedEpoch.MarkEvicted(target)

	return nil
}
