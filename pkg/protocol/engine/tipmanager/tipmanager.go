package tipmanager

import (
	"github.com/iotaledger/hive.go/runtime/module"
	"github.com/iotaledger/iota-core/pkg/model"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/blocks"
	iotago "github.com/iotaledger/iota.go/v4"
)

// TipManager is a component that manages the tips of the Tangle.
type TipManager interface {
	// AddBlock adds a block to the TipManager.
	AddBlock(block *blocks.Block)

	// SelectTips selects the tips that should be used for the next block.
	SelectTips(count int) (references model.ParentReferences)

	// StrongTipSet returns the strong tip set of the TipManager.
	StrongTipSet() []*blocks.Block

	// WeakTipSet returns the weak tip set of the TipManager.
	WeakTipSet() []*blocks.Block

	// Evict evicts a block from the TipManager.
	Evict(slotIndex iotago.SlotIndex)

	// Events returns the events of the TipManager.
	Events() *Events

	// Interface embeds the required methods of the module.Interface.
	module.Interface
}