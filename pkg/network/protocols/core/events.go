package core

import (
	p2ppeer "github.com/libp2p/go-libp2p/core/peer"

	"github.com/iotaledger/hive.go/runtime/event"
	"github.com/iotaledger/iota-core/pkg/model"
	iotago "github.com/iotaledger/iota.go/v4"
	"github.com/iotaledger/iota.go/v4/merklehasher"
)

type Events struct {
	BlockReceived                 *event.Event2[*model.Block, p2ppeer.ID]
	BlockRequestReceived          *event.Event2[iotago.BlockID, p2ppeer.ID]
	SlotCommitmentReceived        *event.Event2[*model.Commitment, p2ppeer.ID]
	SlotCommitmentRequestReceived *event.Event2[iotago.CommitmentID, p2ppeer.ID]
	AttestationsReceived          *event.Event4[*model.Commitment, []*iotago.Attestation, *merklehasher.Proof[iotago.Identifier], p2ppeer.ID]
	AttestationsRequestReceived   *event.Event2[iotago.CommitmentID, p2ppeer.ID]
	WarpSyncRequestReceived       *event.Event2[iotago.CommitmentID, p2ppeer.ID]
	WarpSyncResponseReceived      *event.Event4[iotago.CommitmentID, iotago.BlockIDs, *merklehasher.Proof[iotago.Identifier], p2ppeer.ID]
	Error                         *event.Event2[error, p2ppeer.ID]

	event.Group[Events, *Events]
}

// NewEvents contains the constructor of the Events object (it is generated by a generic factory).
var NewEvents = event.CreateGroupConstructor(func() (newEvents *Events) {
	return &Events{
		BlockReceived:                 event.New2[*model.Block, p2ppeer.ID](),
		BlockRequestReceived:          event.New2[iotago.BlockID, p2ppeer.ID](),
		SlotCommitmentReceived:        event.New2[*model.Commitment, p2ppeer.ID](),
		SlotCommitmentRequestReceived: event.New2[iotago.CommitmentID, p2ppeer.ID](),
		AttestationsReceived:          event.New4[*model.Commitment, []*iotago.Attestation, *merklehasher.Proof[iotago.Identifier], p2ppeer.ID](),
		AttestationsRequestReceived:   event.New2[iotago.CommitmentID, p2ppeer.ID](),
		WarpSyncRequestReceived:       event.New2[iotago.CommitmentID, p2ppeer.ID](),
		WarpSyncResponseReceived:      event.New4[iotago.CommitmentID, iotago.BlockIDs, *merklehasher.Proof[iotago.Identifier], p2ppeer.ID](),
		Error:                         event.New2[error, p2ppeer.ID](),
	}
})
