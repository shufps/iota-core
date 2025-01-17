package eviction_test

import (
	"testing"

	hivedb "github.com/iotaledger/hive.go/kvstore/database"
	"github.com/iotaledger/hive.go/kvstore/mapdb"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/eviction"
	"github.com/iotaledger/iota-core/pkg/storage/database"
	"github.com/iotaledger/iota-core/pkg/storage/permanent"
	"github.com/iotaledger/iota-core/pkg/storage/prunable"
	iotago "github.com/iotaledger/iota.go/v4"
	"github.com/iotaledger/iota.go/v4/tpkg"
)

func TestState_RootBlocks(t *testing.T) {
	errorHandler := func(err error) {
		t.Error(err)
	}

	TestAPISmallMCA := iotago.V3API(iotago.NewV3SnapshotProtocolParameters(
		iotago.WithStorageOptions(0, 0, 0, 0, 0, 0),               // zero storage score
		iotago.WithWorkScoreOptions(0, 1, 0, 0, 0, 0, 0, 0, 0, 0), // all blocks workscore = 1
		iotago.WithLivenessOptions(5, 9, 1, 3, 4),
	))

	prunableStorage := prunable.New(database.Config{
		Engine:    hivedb.EngineMapDB,
		Directory: t.TempDir(),
	}, iotago.SingleVersionProvider(tpkg.ZeroCostTestAPI), errorHandler)

	newSettings := permanent.NewSettings(mapdb.NewMapDB())
	newSettings.StoreProtocolParametersForStartEpoch(TestAPISmallMCA.ProtocolParameters(), 0)

	ts := NewTestFramework(t, prunableStorage, eviction.NewState(newSettings, prunableStorage.RootBlocks))

	ts.CreateAndAddRootBlock("Genesis", 0, iotago.NewEmptyCommitment(TestAPISmallMCA).MustID())
	ts.RequireActiveRootBlocks("Genesis")
	ts.RequireLastEvictedSlot(0)

	ts.Instance.Initialize(0)

	ts.CreateAndAddRootBlock("Root1.0", 1, iotago.NewEmptyCommitment(tpkg.ZeroCostTestAPI).MustID())
	ts.CreateAndAddRootBlock("Root1.1", 1, iotago.NewEmptyCommitment(tpkg.ZeroCostTestAPI).MustID())
	ts.CreateAndAddRootBlock("Root2.0", 2, iotago.NewEmptyCommitment(tpkg.ZeroCostTestAPI).MustID())
	ts.CreateAndAddRootBlock("Root3.0", 3, iotago.NewEmptyCommitment(tpkg.ZeroCostTestAPI).MustID())
	ts.CreateAndAddRootBlock("Root4.0", 4, iotago.NewEmptyCommitment(tpkg.ZeroCostTestAPI).MustID())
	ts.CreateAndAddRootBlock("Root4.1", 4, iotago.NewEmptyCommitment(tpkg.ZeroCostTestAPI).MustID())
	ts.CreateAndAddRootBlock("Root5.0", 5, iotago.NewEmptyCommitment(tpkg.ZeroCostTestAPI).MustID())

	ts.RequireActiveRootBlocks("Genesis")
	ts.RequireLastEvictedSlot(0)
	ts.RequireStorageRootBlocks("Genesis", "Root1.0", "Root1.1", "Root2.0", "Root3.0", "Root4.0", "Root4.1", "Root5.0")

	ts.Instance.AdvanceActiveWindowToIndex(0)
	ts.RequireActiveRootBlocks("Genesis")
	ts.RequireLastEvictedSlot(0)
	ts.RequireStorageRootBlocks("Genesis", "Root1.0", "Root1.1", "Root2.0", "Root3.0", "Root4.0", "Root4.1", "Root5.0")

	ts.Instance.AdvanceActiveWindowToIndex(1)
	ts.RequireActiveRootBlocks("Genesis", "Root1.0", "Root1.1")
	ts.RequireLastEvictedSlot(1)
	ts.RequireStorageRootBlocks("Genesis", "Root1.0", "Root1.1", "Root2.0", "Root3.0", "Root4.0", "Root4.1", "Root5.0")

	ts.Instance.AdvanceActiveWindowToIndex(2)
	ts.RequireActiveRootBlocks("Genesis", "Root1.0", "Root1.1", "Root2.0")
	ts.RequireLastEvictedSlot(2)
	ts.RequireStorageRootBlocks("Genesis", "Root1.0", "Root1.1", "Root2.0", "Root3.0", "Root4.0", "Root4.1", "Root5.0")

	ts.Instance.AdvanceActiveWindowToIndex(3)
	// Genesis is evicted because the rootBlockEviction delay is 3: we keep the root blocks of the last 3 slots
	// starting from the last evicted one: 3, 2, 1.
	ts.RequireActiveRootBlocks("Root1.0", "Root1.1", "Root2.0", "Root3.0")
	// Now 0 should be expected to have been evicted.
	ts.RequireLastEvictedSlot(3)
	ts.RequireStorageRootBlocks("Genesis", "Root1.0", "Root1.1", "Root2.0", "Root3.0", "Root4.0", "Root4.1", "Root5.0")

	ts.Instance.AdvanceActiveWindowToIndex(4)
	ts.RequireActiveRootBlocks("Root2.0", "Root3.0", "Root4.0", "Root4.1")
	ts.RequireLastEvictedSlot(4)
	ts.RequireStorageRootBlocks("Root1.0", "Root1.1", "Root2.0", "Root3.0", "Root4.0", "Root4.1", "Root5.0")
}
