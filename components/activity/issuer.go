package activity

import (
	"github.com/iotaledger/hive.go/lo"
	"github.com/iotaledger/iota-core/pkg/model"
	iotago "github.com/iotaledger/iota.go/v4"
	"github.com/iotaledger/iota.go/v4/builder"
)

func issueActivityBlock() {
	issuerKey := lo.PanicOnErr(deps.Peer.Database().LocalPrivateKey())
	pubKey := issuerKey.Public()
	addr := iotago.Ed25519AddressFromPubKey(pubKey[:])

	block, err := builder.NewBlockBuilder().
		StrongParents(deps.Protocol.TipManager.Tips(ParamsActivity.ParentsCount)).
		SlotCommitment(deps.Protocol.MainEngineInstance().Storage.Settings.LatestCommitment()).
		Payload(&iotago.TaggedData{
			Tag: []byte("ACTIVITY"),
		}).
		Sign(&addr, issuerKey[:]).
		Build()
	if err != nil {
		Component.LogWarnf("error building block: %s", err.Error())
		return
	}

	modelBlock, err := model.BlockFromBlock(block, deps.Protocol.API())
	if err != nil {
		Component.LogWarnf("error creating model.Block from block: %s", err.Error())
		return
	}

	err = deps.Protocol.ProcessBlock(modelBlock, deps.Peer.ID())
	if err != nil {
		Component.LogWarnf("Error processing block in Protocol: %s", err.Error())
		return
	}

	Component.LogInfof("Issued activity block: %s - commitment %s %d", modelBlock.ID(), modelBlock.Block().SlotCommitment.MustID(), modelBlock.Block().SlotCommitment.Index)
}