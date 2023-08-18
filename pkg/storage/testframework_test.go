package storage_test

import (
	"encoding/binary"
	"fmt"
	"math/rand"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/iotaledger/hive.go/ds/types"
	"github.com/iotaledger/hive.go/kvstore"
	"github.com/iotaledger/hive.go/lo"
	"github.com/iotaledger/hive.go/runtime/options"
	"github.com/iotaledger/iota-core/pkg/core/account"
	"github.com/iotaledger/iota-core/pkg/model"
	"github.com/iotaledger/iota-core/pkg/storage"
	"github.com/iotaledger/iota-core/pkg/storage/database"
	"github.com/iotaledger/iota-core/pkg/storage/prunable/epochstore"
	iotago "github.com/iotaledger/iota.go/v4"
	"github.com/iotaledger/iota.go/v4/api"
	"github.com/iotaledger/iota.go/v4/tpkg"
)

const (
	B  int64 = 1
	KB       = 1024 * B
	MB       = 1024 * KB
	GB       = 1024 * MB
)

type TestFramework struct {
	t               *testing.T
	Instance        *storage.Storage
	apiProvider     api.Provider
	baseDir         string
	baseDirPrunable string

	uniqueKeyCounter uint64
}

func NewTestFramework(t *testing.T, base string, storageOpts ...options.Option[storage.Storage]) *TestFramework {
	errorHandler := func(err error) {
		t.Log(err)
	}

	baseDir := base
	if baseDir == "" {
		baseDir = t.TempDir()
	}

	instance := storage.New(baseDir, 0, errorHandler, storageOpts...)
	require.NoError(t, instance.Settings().StoreProtocolParametersForStartEpoch(iotago.NewV3ProtocolParameters(), 0))

	return &TestFramework{
		t:               t,
		Instance:        instance,
		apiProvider:     instance.Settings().APIProvider(),
		baseDir:         baseDir,
		baseDirPrunable: filepath.Join(baseDir, "prunable"),
	}
}

func (t *TestFramework) Shutdown() {
	t.Instance.Shutdown()
}

func (t *TestFramework) BaseDir() string {
	return t.baseDir
}

func (t *TestFramework) SetLatestFinalizedEpoch(epoch iotago.EpochIndex) {
	endSlot := t.Instance.Settings().APIProvider().CurrentAPI().TimeProvider().EpochEnd(epoch)
	t.Instance.Settings().SetLatestFinalizedSlot(endSlot)
}

func (t *TestFramework) GeneratePrunableData(epoch iotago.EpochIndex, size int64) {
	initialStorageSize := t.Instance.PrunableDatabaseSize()

	apiForEpoch := t.apiProvider.APIForEpoch(epoch)
	endSlot := apiForEpoch.TimeProvider().EpochEnd(epoch)

	var createdBytes int64
	for createdBytes < size {
		block := tpkg.RandProtocolBlock(&iotago.BasicBlock{
			StrongParents: tpkg.SortedRandBlockIDs(1 + rand.Intn(iotago.BlockMaxParents)),
			Payload:       &iotago.TaggedData{Data: make([]byte, 8192)},
			BurnedMana:    1000,
		}, apiForEpoch, 0)

		modelBlock, err := model.BlockFromBlock(block, apiForEpoch)
		require.NoError(t.t, err)

		blockStorageForSlot, err := t.Instance.Blocks(endSlot)
		require.NoError(t.t, err)
		err = blockStorageForSlot.Store(modelBlock)
		require.NoError(t.t, err)

		createdBytes += int64(len(modelBlock.Data()))
		createdBytes += iotago.SlotIdentifierLength
	}

	t.Instance.Flush()
	t.AssertPrunableSizeGreater(initialStorageSize + size)

	fmt.Printf("> created %d MB of bucket prunable data\n\tPermanent: %dMB\n\tPrunable: %dMB\n", createdBytes/MB, t.Instance.PermanentDatabaseSize()/MB, t.Instance.PrunableDatabaseSize()/MB)
}

func (t *TestFramework) GenerateSemiPermanentData(epoch iotago.EpochIndex) {
	rewardsKV, err := t.Instance.RewardsForEpoch(epoch)
	require.NoError(t.t, err)

	poolStatsStore := t.Instance.PoolStats()
	decidedUpgradeSignalsStore := t.Instance.DecidedUpgradeSignals()
	committeeStore := t.Instance.Committee()

	var createdBytes int64

	for i := 0; i < 200; i++ {
		createdBytes += t.storeRandomData(rewardsKV, 32)
	}

	poolStatsModel := &model.PoolsStats{
		TotalStake:          1,
		TotalValidatorStake: 2,
		ProfitMargin:        3,
	}
	err = poolStatsStore.Store(epoch, poolStatsModel)
	require.NoError(t.t, err)
	createdBytes += int64(len(lo.PanicOnErr(poolStatsModel.Bytes()))) + 8 // for epoch key

	versionAndHash := model.VersionAndHash{
		Version: 1,
		Hash:    iotago.Identifier{2},
	}
	err = decidedUpgradeSignalsStore.Store(epoch, versionAndHash)
	require.NoError(t.t, err)
	createdBytes += int64(len(lo.PanicOnErr(versionAndHash.Bytes()))) + 8 // for epoch key

	accounts := account.NewAccounts()
	accounts.Set(tpkg.RandAccountID(), &account.Pool{})
	err = committeeStore.Store(epoch, accounts)
	require.NoError(t.t, err)
	createdBytes += int64(len(lo.PanicOnErr(accounts.Bytes()))) + 8 // for epoch key

	t.Instance.Flush()
}

func (t *TestFramework) GeneratePermanentData(size int64) {
	initialStorageSize := t.Instance.PermanentDatabaseSize()

	// Use as dummy to generate some data.
	kv := t.Instance.Ledger()

	var createdBytes int64
	for createdBytes < size {
		createdBytes += t.storeRandomData(kv, 8192)
	}

	t.Instance.Flush()

	t.AssertPermanentSizeGreater(initialStorageSize + size)
	fmt.Printf("> created %d MB of permanent data\n\tPermanent: %dMB\n\tPrunable: %dMB\n", createdBytes/MB, t.Instance.PermanentDatabaseSize()/MB, t.Instance.PrunableDatabaseSize()/MB)
}

func (t *TestFramework) storeRandomData(kv kvstore.KVStore, size int64) int64 {
	err := kv.Set(uint64ToBytes(t.uniqueKeyCounter), tpkg.RandBytes(int(size)))
	require.NoError(t.t, err)

	t.uniqueKeyCounter++

	return size + 8 // for key
}

func uint64ToBytes[V ~uint64](v V) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, uint64(v))
	return b
}

func (t *TestFramework) AssertStorageSizeGreater(expected int64) {
	require.GreaterOrEqual(t.t, t.Instance.Size(), expected)
}

func (t *TestFramework) AssertPrunableSizeGreater(expected int64) {
	require.GreaterOrEqual(t.t, t.Instance.PrunableDatabaseSize(), expected)
}

func (t *TestFramework) AssertPermanentSizeGreater(expected int64) {
	require.GreaterOrEqual(t.t, t.Instance.PermanentDatabaseSize(), expected)
}

func (t *TestFramework) AssertStorageSizeBelow(expected int64) {
	require.LessOrEqual(t.t, t.Instance.Size(), expected)
}

func (t *TestFramework) AssertPrunedUntil(
	expectedPrunable *types.Tuple[int, bool],
	expectedDecidedUpgrades *types.Tuple[int, bool],
	expectedPoolStats *types.Tuple[int, bool],
	expectedCommittee *types.Tuple[int, bool],
	expectedRewards *types.Tuple[int, bool]) {

	t.assertPrunedState(expectedPrunable, t.Instance.LastPrunedEpoch, "prunable")
	t.assertPrunedState(expectedPoolStats, t.Instance.PoolStats().LastPrunedEpoch, "pool stats")
	t.assertPrunedState(expectedDecidedUpgrades, t.Instance.DecidedUpgradeSignals().LastPrunedEpoch, "decided upgrades")
	t.assertPrunedState(expectedCommittee, t.Instance.Committee().LastPrunedEpoch, "committee")
	t.assertPrunedState(expectedRewards, t.Instance.Rewards().LastPrunedEpoch, "rewards")

	// Check that things are actually pruned and the correct error is returned.
	if expectedPrunable.B {
		for epoch := iotago.EpochIndex(0); epoch <= iotago.EpochIndex(expectedPrunable.A); epoch++ {
			t.assertPrunableSlotStoragesPruned(epoch)
		}
	}

	if expectedDecidedUpgrades.B {
		for epoch := iotago.EpochIndex(0); epoch <= iotago.EpochIndex(expectedDecidedUpgrades.A); epoch++ {
			assertPrunableEpochStoragesPruned(t, t.Instance.DecidedUpgradeSignals(), epoch)
		}
	}

	if expectedPoolStats.B {
		for epoch := iotago.EpochIndex(0); epoch <= iotago.EpochIndex(expectedPoolStats.A); epoch++ {
			assertPrunableEpochStoragesPruned(t, t.Instance.PoolStats(), epoch)
		}
	}

	if expectedCommittee.B {
		for epoch := iotago.EpochIndex(0); epoch <= iotago.EpochIndex(expectedCommittee.A); epoch++ {
			assertPrunableEpochStoragesPruned(t, t.Instance.Committee(), epoch)
		}
	}

	if expectedRewards.B {
		for epoch := iotago.EpochIndex(0); epoch <= iotago.EpochIndex(expectedRewards.A); epoch++ {
			_, err := t.Instance.RewardsForEpoch(epoch)
			require.ErrorIsf(t.t, err, database.ErrEpochPruned, "expected epoch %d to be pruned when calling RewardsForEpoch", epoch)
		}
	}

	// TODO:  -> for permanent storage we need to make sure that everything from the ledger is deleted for the given epoch/slots
}

func assertPrunableEpochStoragesPruned[V any](t *TestFramework, store *epochstore.Store[V], epoch iotago.EpochIndex) {
	// Check that all store returns the expected error when trying to access the data.
	_, err := store.Load(epoch)
	require.ErrorIsf(t.t, err, database.ErrEpochPruned, "expected epoch %d to be pruned when calling Load", epoch)

	var empty V
	err = store.Store(epoch, empty)
	require.ErrorIsf(t.t, err, database.ErrEpochPruned, "expected epoch %d to be pruned when calling Store", epoch)

	// Check that the epoch actually has been deleted.
	var seenEpochs []iotago.EpochIndex
	err = store.Stream(func(epoch iotago.EpochIndex, _ V) error {
		seenEpochs = append(seenEpochs, epoch)
		return nil
	})
	require.NotContainsf(t.t, seenEpochs, epoch, "expected epoch %d to be pruned when calling Stream", epoch)

	seenEpochs = nil
	err = store.StreamBytes(func(bytes []byte, bytes2 []byte) error {
		epochFromBytes, _, err := iotago.EpochIndexFromBytes(bytes)
		require.NoError(t.t, err)
		seenEpochs = append(seenEpochs, epochFromBytes)
		return nil
	})
	require.NotContainsf(t.t, seenEpochs, epoch, "expected epoch %d to be pruned when calling StreamBytes", epoch)
}

func (t *TestFramework) assertPrunableSlotStoragesPruned(epoch iotago.EpochIndex) {
	// Check that the folder for the epoch is deleted.
	require.NoDirExistsf(t.t, filepath.Join(t.baseDirPrunable, fmt.Sprintf("%d", epoch)), "expected folder for epoch %d to be deleted from disk", epoch)

	// Check that all storages return the expected error when trying to access the data.
	endSlot := t.apiProvider.APIForEpoch(epoch).TimeProvider().EpochEnd(epoch)

	_, err := t.Instance.Blocks(endSlot)
	require.ErrorIsf(t.t, err, database.ErrEpochPruned, "expected epoch %d to be pruned", epoch)

	_, err = t.Instance.RootBlocks(endSlot)
	require.ErrorIsf(t.t, err, database.ErrEpochPruned, "expected epoch %d to be pruned", epoch)

	_, err = t.Instance.Attestations(endSlot)
	require.ErrorIsf(t.t, err, database.ErrEpochPruned, "expected epoch %d to be pruned", epoch)

	_, err = t.Instance.AccountDiffs(endSlot)
	require.ErrorIsf(t.t, err, database.ErrEpochPruned, "expected epoch %d to be pruned", epoch)

	_, err = t.Instance.PerformanceFactors(endSlot)
	require.ErrorIsf(t.t, err, database.ErrEpochPruned, "expected epoch %d to be pruned", epoch)

	_, err = t.Instance.UpgradeSignals(endSlot)
	require.ErrorIsf(t.t, err, database.ErrEpochPruned, "expected epoch %d to be pruned", epoch)

	_, err = t.Instance.Roots(endSlot)
	require.ErrorIsf(t.t, err, database.ErrEpochPruned, "expected epoch %d to be pruned", epoch)

	_, err = t.Instance.Retainer(endSlot)
	require.ErrorIsf(t.t, err, database.ErrEpochPruned, "expected epoch %d to be pruned", epoch)
}

func (t *TestFramework) assertPrunedState(expected *types.Tuple[int, bool], prunedStateFunc func() (iotago.EpochIndex, bool), name string) {
	lastPruned, hasPruned := prunedStateFunc()
	require.EqualValuesf(t.t, expected.A, lastPruned, "%s: expected %d, got %d", name, expected.A, lastPruned)
	require.EqualValuesf(t.t, expected.B, hasPruned, "%s: expected %v, got %v", name, expected.B, hasPruned)
}
