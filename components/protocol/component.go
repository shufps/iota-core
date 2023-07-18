package protocol

import (
	"context"
	"time"

	"go.uber.org/dig"

	"github.com/iotaledger/hive.go/app"
	"github.com/iotaledger/hive.go/autopeering/peer"
	"github.com/iotaledger/hive.go/ierrors"
	hivedb "github.com/iotaledger/hive.go/kvstore/database"
	"github.com/iotaledger/hive.go/runtime/workerpool"
	"github.com/iotaledger/iota-core/pkg/core/account"
	"github.com/iotaledger/iota-core/pkg/daemon"
	"github.com/iotaledger/iota-core/pkg/model"
	"github.com/iotaledger/iota-core/pkg/network"
	"github.com/iotaledger/iota-core/pkg/network/p2p"
	"github.com/iotaledger/iota-core/pkg/protocol"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/attestation/slotattestation"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/blocks"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/filter"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/filter/blockfilter"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/notarization"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/notarization/slotnotarization"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/upgrade/signalingupgradeorchestrator"
	"github.com/iotaledger/iota-core/pkg/protocol/sybilprotection/sybilprotectionv1"
	"github.com/iotaledger/iota-core/pkg/storage"
	"github.com/iotaledger/iota-core/pkg/storage/database"
	"github.com/iotaledger/iota-core/pkg/storage/prunable"
	iotago "github.com/iotaledger/iota.go/v4"
)

func init() {
	Component = &app.Component{
		Name:             "Protocol",
		DepsFunc:         func(cDeps dependencies) { deps = cDeps },
		Params:           params,
		InitConfigParams: initConfigParams,
		Provide:          provide,
		Configure:        configure,
		Run:              run,
	}
}

var (
	Component *app.Component
	deps      dependencies
)

type dependencies struct {
	dig.In

	Peer     *peer.Local
	Protocol *protocol.Protocol
}

func initConfigParams(c *dig.Container) error {

	type cfgResult struct {
		dig.Out
		DatabaseEngine hivedb.Engine `name:"databaseEngine"`
	}

	if err := c.Provide(func() cfgResult {
		dbEngine, err := hivedb.EngineFromStringAllowed(ParamsDatabase.Engine, database.AllowedEnginesDefault)
		if err != nil {
			Component.LogPanic(err)
		}

		return cfgResult{
			DatabaseEngine: dbEngine,
		}
	}); err != nil {
		Component.LogPanic(err)
	}

	return nil
}

func provide(c *dig.Container) error {

	type protocolDeps struct {
		dig.In

		DatabaseEngine hivedb.Engine `name:"databaseEngine"`
		P2PManager     *p2p.Manager
	}

	return c.Provide(func(deps protocolDeps) *protocol.Protocol {
		return protocol.New(
			workerpool.NewGroup("Protocol"),
			deps.P2PManager,
			protocol.WithBaseDirectory(ParamsDatabase.Path),
			protocol.WithStorageOptions(
				storage.WithDBEngine(deps.DatabaseEngine),
				storage.WithPruningDelay(iotago.SlotIndex(ParamsDatabase.PruningThreshold)),
				storage.WithPrunableManagerOptions(
					prunable.WithGranularity(ParamsDatabase.DBGranularity),
					prunable.WithMaxOpenDBs(ParamsDatabase.MaxOpenDBs),
				),
			),
			protocol.WithSnapshotPath(ParamsProtocol.Snapshot.Path),
			protocol.WithSybilProtectionProvider(
				sybilprotectionv1.NewProvider(),
			),
			protocol.WithNotarizationProvider(
				slotnotarization.NewProvider(),
			),
			protocol.WithAttestationProvider(
				slotattestation.NewProvider(slotattestation.DefaultAttestationCommitmentOffset),
			),
			protocol.WithFilterProvider(
				blockfilter.NewProvider(
					blockfilter.WithMaxAllowedWallClockDrift(ParamsProtocol.Filter.MaxAllowedClockDrift),
					blockfilter.WithSignatureValidation(true),
				),
			),
			// TODO: here we should pass the protocol parameters from the config.
			protocol.WithUpgradeOrchestratorProvider(signalingupgradeorchestrator.NewProvider()),
		)
	})
}

func configure() error {
	deps.Protocol.Events.Error.Hook(func(err error) {
		Component.LogErrorf("Error in Protocol: %s", err)
	})

	deps.Protocol.Events.Network.Error.Hook(func(err error, id network.PeerID) {
		Component.LogErrorf("NetworkError: %s Source: %s", err.Error(), id)
	})

	// TODO: check whether we hooked to all events

	deps.Protocol.Events.Network.BlockReceived.Hook(func(block *model.Block, source network.PeerID) {
		Component.LogInfof("BlockReceived: %s", block.ID())
	})

	deps.Protocol.Events.Engine.Filter.BlockFiltered.Hook(func(event *filter.BlockFilteredEvent) {
		Component.LogInfof("BlockFiltered: %s - %s", event.Block.ID(), event.Reason.Error())
	})

	deps.Protocol.Events.Engine.BlockDAG.BlockSolid.Hook(func(block *blocks.Block) {
		Component.LogInfof("BlockSolid: %s", block.ID())
	})

	deps.Protocol.Events.Engine.Booker.BlockBooked.Hook(func(block *blocks.Block) {
		Component.LogInfof("BlockBooked: %s", block.ID())
	})

	deps.Protocol.Events.Engine.BlockGadget.BlockPreAccepted.Hook(func(block *blocks.Block) {
		Component.LogInfof("BlockPreAccepted: %s", block.ID())
	})

	deps.Protocol.Events.Engine.BlockGadget.BlockAccepted.Hook(func(block *blocks.Block) {
		Component.LogInfof("BlockAccepted: %s", block.ID())
	})

	deps.Protocol.Events.Engine.BlockGadget.BlockPreConfirmed.Hook(func(block *blocks.Block) {
		Component.LogInfof("BlockPreConfirmed: %s", block.ID())
	})

	deps.Protocol.Events.Engine.Clock.AcceptedTimeUpdated.Hook(func(time time.Time) {
		Component.LogInfof("AcceptedTimeUpdated: Slot %d @ %s", deps.Protocol.LatestAPI().TimeProvider().SlotFromTime(time), time)
	})

	deps.Protocol.Events.Engine.Clock.ConfirmedTimeUpdated.Hook(func(time time.Time) {
		Component.LogInfof("ConfirmedTimeUpdated: Slot %d @ %s", deps.Protocol.LatestAPI().TimeProvider().SlotFromTime(time), time)
	})

	deps.Protocol.Events.Engine.Notarization.SlotCommitted.Hook(func(details *notarization.SlotCommittedDetails) {
		Component.LogInfof("SlotCommitted: %s - %d", details.Commitment.ID(), details.Commitment.Index())
	})

	deps.Protocol.Events.Engine.SlotGadget.SlotFinalized.Hook(func(index iotago.SlotIndex) {
		Component.LogInfof("SlotFinalized: %d", index)
	})

	deps.Protocol.Events.ChainManager.RequestCommitment.Hook(func(id iotago.CommitmentID) {
		Component.LogInfof("RequestCommitment: %s", id)
	})

	deps.Protocol.Events.Network.SlotCommitmentRequestReceived.Hook(func(commitmentID iotago.CommitmentID, id network.PeerID) {
		Component.LogInfof("SlotCommitmentRequestReceived: %s", commitmentID)
	})

	deps.Protocol.Events.Network.SlotCommitmentReceived.Hook(func(commitment *model.Commitment, id network.PeerID) {
		Component.LogInfof("SlotCommitmentReceived: %s", commitment.ID())
	})

	deps.Protocol.Events.Engine.SybilProtection.CommitteeSelected.Hook(func(committee *account.Accounts, epoch iotago.EpochIndex) {
		Component.LogInfof("CommitteeSelected: Epoch %d - %s (reused: %t)", epoch, committee.IDs(), committee.IsReused())
	})

	return nil
}

func run() error {
	return Component.Daemon().BackgroundWorker(Component.Name, func(ctx context.Context) {
		if err := deps.Protocol.Run(ctx); err != nil {
			if !ierrors.Is(err, context.Canceled) {
				Component.LogErrorfAndExit("Error running the Protocol: %s", err.Error())
			}
		}
		Component.LogInfo("Gracefully shutting down the Protocol...")
	}, daemon.PriorityProtocol)
}
