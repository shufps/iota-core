package presets

import (
	"time"

	"golang.org/x/crypto/blake2b"

	"github.com/iotaledger/hive.go/crypto/ed25519"
	"github.com/iotaledger/hive.go/lo"
	"github.com/iotaledger/hive.go/runtime/options"
	"github.com/iotaledger/iota-core/pkg/protocol"
	"github.com/iotaledger/iota-core/pkg/protocol/snapshotcreator"
	"github.com/iotaledger/iota-core/pkg/testsuite"
	iotago "github.com/iotaledger/iota.go/v4"
	"github.com/iotaledger/iota.go/v4/hexutil"
)

var Base = []options.Option[snapshotcreator.Options]{
	snapshotcreator.WithDatabaseVersion(protocol.DatabaseVersion),
	snapshotcreator.WithFilePath("snapshot.bin"),
	snapshotcreator.WithProtocolParameters(
		iotago.NewV3ProtocolParameters(
			iotago.WithNetworkOptions("default", "rms"),
			iotago.WithSupplyOptions(10_000_000_000, 100, 1, 10, 100, 100, 100),
			iotago.WithTimeProviderOptions(1696841745, 10, 13),
			iotago.WithLivenessOptions(30, 30, 7, 14, 30),
			// increase/decrease threshold = fraction * slotDurationInSeconds * schedulerRate
			iotago.WithCongestionControlOptions(500, 500, 500, 800000, 500000, 100000, 1000, 100),
			iotago.WithWorkScoreOptions(25, 1, 100, 50, 10, 10, 50, 1, 10, 250),
		),
	),
	snapshotcreator.WithRootBlocks(map[iotago.BlockID]iotago.CommitmentID{
		iotago.EmptyBlockID: iotago.NewEmptyCommitment(3).MustID(),
	}),
}

var Docker = []options.Option[snapshotcreator.Options]{
	snapshotcreator.WithFilePath("docker-network.snapshot"),
	snapshotcreator.WithAccounts(
		snapshotcreator.AccountDetails{ // validator-1
			AccountID:            blake2b.Sum256(lo.PanicOnErr(hexutil.DecodeHex("0x293dc170d9a59474e6d81cfba7f7d924c09b25d7166bcfba606e53114d0a758b"))),
			Address:              iotago.Ed25519AddressFromPubKey(lo.PanicOnErr(hexutil.DecodeHex("0x293dc170d9a59474e6d81cfba7f7d924c09b25d7166bcfba606e53114d0a758b"))),
			Amount:               testsuite.MinValidatorAccountAmount,
			IssuerKey:            iotago.Ed25519PublicKeyBlockIssuerKeyFromPublicKey(ed25519.PublicKey(lo.PanicOnErr(hexutil.DecodeHex("0x293dc170d9a59474e6d81cfba7f7d924c09b25d7166bcfba606e53114d0a758b")))),
			ExpirySlot:           iotago.MaxSlotIndex,
			BlockIssuanceCredits: iotago.MaxBlockIssuanceCredits / 2,
			StakingEpochEnd:      iotago.MaxEpochIndex,
			FixedCost:            1,
			StakedAmount:         testsuite.MinValidatorAccountAmount,
			Mana:                 iotago.Mana(testsuite.MinValidatorAccountAmount),
		},
		snapshotcreator.AccountDetails{ // validator-2
			AccountID:            blake2b.Sum256(lo.PanicOnErr(hexutil.DecodeHex("0x05c1de274451db8de8182d64c6ee0dca3ae0c9077e0b4330c976976171d79064"))),
			Address:              iotago.Ed25519AddressFromPubKey(lo.PanicOnErr(hexutil.DecodeHex("0x05c1de274451db8de8182d64c6ee0dca3ae0c9077e0b4330c976976171d79064"))),
			Amount:               testsuite.MinValidatorAccountAmount,
			IssuerKey:            iotago.Ed25519PublicKeyBlockIssuerKeyFromPublicKey(ed25519.PublicKey(lo.PanicOnErr(hexutil.DecodeHex("0x05c1de274451db8de8182d64c6ee0dca3ae0c9077e0b4330c976976171d79064")))),
			ExpirySlot:           iotago.MaxSlotIndex,
			BlockIssuanceCredits: iotago.MaxBlockIssuanceCredits / 2,
			StakingEpochEnd:      iotago.MaxEpochIndex,
			FixedCost:            1,
			StakedAmount:         testsuite.MinValidatorAccountAmount,
			Mana:                 iotago.Mana(testsuite.MinValidatorAccountAmount),
		},
		snapshotcreator.AccountDetails{ // validator-3
			AccountID:            blake2b.Sum256(lo.PanicOnErr(hexutil.DecodeHex("0x1e4b21eb51dcddf65c20db1065e1f1514658b23a3ddbf48d30c0efc926a9a648"))),
			Address:              iotago.Ed25519AddressFromPubKey(lo.PanicOnErr(hexutil.DecodeHex("0x1e4b21eb51dcddf65c20db1065e1f1514658b23a3ddbf48d30c0efc926a9a648"))),
			Amount:               testsuite.MinValidatorAccountAmount,
			IssuerKey:            iotago.Ed25519PublicKeyBlockIssuerKeyFromPublicKey(ed25519.PublicKey(lo.PanicOnErr(hexutil.DecodeHex("0x1e4b21eb51dcddf65c20db1065e1f1514658b23a3ddbf48d30c0efc926a9a648")))),
			ExpirySlot:           iotago.MaxSlotIndex,
			BlockIssuanceCredits: iotago.MaxBlockIssuanceCredits / 2,
			StakingEpochEnd:      iotago.MaxEpochIndex,
			FixedCost:            1,
			StakedAmount:         testsuite.MinValidatorAccountAmount,
			Mana:                 iotago.Mana(testsuite.MinValidatorAccountAmount),
		},
		snapshotcreator.AccountDetails{ // inx-blockissuer
			AccountID:            blake2b.Sum256(lo.PanicOnErr(hexutil.DecodeHex("0xa54fafa44a88e4a6a37796526ea884f613a24d84337871226eb6360f022d8b39"))),
			Address:              iotago.Ed25519AddressFromPubKey(lo.PanicOnErr(hexutil.DecodeHex("0xa54fafa44a88e4a6a37796526ea884f613a24d84337871226eb6360f022d8b39"))),
			Amount:               testsuite.MinIssuerAccountAmount,
			IssuerKey:            iotago.Ed25519PublicKeyBlockIssuerKeyFromPublicKey(ed25519.PublicKey(lo.PanicOnErr(hexutil.DecodeHex("0xa54fafa44a88e4a6a37796526ea884f613a24d84337871226eb6360f022d8b39")))),
			ExpirySlot:           iotago.MaxSlotIndex,
			BlockIssuanceCredits: iotago.MaxBlockIssuanceCredits / 2,
			Mana:                 iotago.Mana(testsuite.MinIssuerAccountAmount),
		},
	),
	snapshotcreator.WithProtocolParameters(
		iotago.NewV3ProtocolParameters(
			iotago.WithNetworkOptions("docker", "rms"),
			iotago.WithSupplyOptions(10_000_000_000, 1, 1, 10, 100, 100, 100),
			iotago.WithTimeProviderOptions(time.Now().Unix(), 10, 13),
			iotago.WithLivenessOptions(30, 30, 7, 14, 30),
			// increase/decrease threshold = fraction * slotDurationInSeconds * schedulerRate
			iotago.WithCongestionControlOptions(500, 500, 500, 800000, 500000, 100000, 1000, 100),
			iotago.WithWorkScoreOptions(25, 1, 100, 50, 10, 10, 50, 1, 10, 250),
		),
	),
}

// Feature is a preset for the feature network, genesis time ~20th of July 2023.
var Feature = []options.Option[snapshotcreator.Options]{
	snapshotcreator.WithFilePath("docker-network.snapshot"),
	snapshotcreator.WithAccounts(
		snapshotcreator.AccountDetails{
			AccountID:            blake2b.Sum256(lo.PanicOnErr(hexutil.DecodeHex("0x01fb6b9db5d96240aef00bc950d1c67a6494513f6d7cf784e57b4972b96ab2fe"))),
			Address:              iotago.Ed25519AddressFromPubKey(lo.PanicOnErr(hexutil.DecodeHex("0x01fb6b9db5d96240aef00bc950d1c67a6494513f6d7cf784e57b4972b96ab2fe"))),
			Amount:               testsuite.MinValidatorAccountAmount,
			IssuerKey:            iotago.Ed25519PublicKeyBlockIssuerKeyFromPublicKey(ed25519.PublicKey(lo.PanicOnErr(hexutil.DecodeHex("0x01fb6b9db5d96240aef00bc950d1c67a6494513f6d7cf784e57b4972b96ab2fe")))),
			ExpirySlot:           iotago.MaxSlotIndex,
			BlockIssuanceCredits: iotago.MaxBlockIssuanceCredits / 2,
			StakingEpochEnd:      iotago.MaxEpochIndex,
			FixedCost:            1,
			StakedAmount:         testsuite.MinValidatorAccountAmount,
			Mana:                 iotago.Mana(testsuite.MinValidatorAccountAmount),
		},
		snapshotcreator.AccountDetails{
			AccountID:            blake2b.Sum256(lo.PanicOnErr(hexutil.DecodeHex("0x83e7f71a440afd48981a8b4684ddae24434b7182ce5c47cfb56ac528525fd4b6"))),
			Address:              iotago.Ed25519AddressFromPubKey(lo.PanicOnErr(hexutil.DecodeHex("0x83e7f71a440afd48981a8b4684ddae24434b7182ce5c47cfb56ac528525fd4b6"))),
			Amount:               testsuite.MinValidatorAccountAmount,
			IssuerKey:            iotago.Ed25519PublicKeyBlockIssuerKeyFromPublicKey(ed25519.PublicKey(lo.PanicOnErr(hexutil.DecodeHex("0x83e7f71a440afd48981a8b4684ddae24434b7182ce5c47cfb56ac528525fd4b6")))),
			ExpirySlot:           iotago.MaxSlotIndex,
			BlockIssuanceCredits: iotago.MaxBlockIssuanceCredits / 2,
			StakingEpochEnd:      iotago.MaxEpochIndex,
			FixedCost:            1,
			StakedAmount:         testsuite.MinValidatorAccountAmount,
			Mana:                 iotago.Mana(testsuite.MinValidatorAccountAmount),
		},
		snapshotcreator.AccountDetails{
			AccountID:            blake2b.Sum256(lo.PanicOnErr(hexutil.DecodeHex("0xac628986b2ef52a1679f2289fcd7b4198476976dea4c30ae34ff04ae52e14805"))),
			Address:              iotago.Ed25519AddressFromPubKey(lo.PanicOnErr(hexutil.DecodeHex("0xac628986b2ef52a1679f2289fcd7b4198476976dea4c30ae34ff04ae52e14805"))),
			Amount:               testsuite.MinValidatorAccountAmount,
			IssuerKey:            iotago.Ed25519PublicKeyBlockIssuerKeyFromPublicKey(ed25519.PublicKey(lo.PanicOnErr(hexutil.DecodeHex("0xac628986b2ef52a1679f2289fcd7b4198476976dea4c30ae34ff04ae52e14805")))),
			ExpirySlot:           iotago.MaxSlotIndex,
			BlockIssuanceCredits: iotago.MaxBlockIssuanceCredits / 2,
			StakingEpochEnd:      iotago.MaxEpochIndex,
			FixedCost:            1,
			StakedAmount:         testsuite.MinValidatorAccountAmount,
			Mana:                 iotago.Mana(testsuite.MinValidatorAccountAmount),
		},
	),
	snapshotcreator.WithProtocolParameters(
		iotago.NewV3ProtocolParameters(
			iotago.WithNetworkOptions("feature", "rms"),
			iotago.WithSupplyOptions(10_000_000_000, 100, 1, 10, 100, 100, 100),
			iotago.WithTimeProviderOptions(1697631694, 10, 13),
			iotago.WithLivenessOptions(30, 30, 10, 20, 30),
			// increase/decrease threshold = fraction * slotDurationInSeconds * schedulerRate
			iotago.WithCongestionControlOptions(500, 500, 500, 800000, 500000, 100000, 1000, 100),
			iotago.WithWorkScoreOptions(25, 1, 100, 50, 10, 10, 50, 1, 10, 250),
		),
	),
}
