package sybilprotection

import (
	"github.com/iotaledger/hive.go/runtime/event"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/blocks"
	iotago "github.com/iotaledger/iota.go/v4"
)

type Events struct {
	BlockProcessed                *event.Event1[*blocks.Block]
	OnlineCommitteeAccountAdded   *event.Event1[iotago.AccountID]
	OnlineCommitteeAccountRemoved *event.Event1[iotago.AccountID]

	event.Group[Events, *Events]
}

// NewEvents contains the constructor of the Events object (it is generated by a generic factory).
var NewEvents = event.CreateGroupConstructor(func() (newEvents *Events) {
	return &Events{
		BlockProcessed:                event.New1[*blocks.Block](),
		OnlineCommitteeAccountAdded:   event.New1[iotago.AccountID](),
		OnlineCommitteeAccountRemoved: event.New1[iotago.AccountID](),
	}
})