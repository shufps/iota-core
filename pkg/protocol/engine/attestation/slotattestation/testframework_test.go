package slotattestation_test

import (
	"crypto/ed25519"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/iotaledger/hive.go/ads"
	"github.com/iotaledger/hive.go/core/account"
	"github.com/iotaledger/hive.go/ds/shrinkingmap"
	"github.com/iotaledger/hive.go/kvstore"
	"github.com/iotaledger/hive.go/kvstore/mapdb"
	"github.com/iotaledger/hive.go/lo"
	"github.com/iotaledger/iota-core/pkg/model"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/attestation/slotattestation"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/blocks"
	iotago "github.com/iotaledger/iota.go/v4"
	"github.com/iotaledger/iota.go/v4/builder"
)

type issuer struct {
	accountID iotago.AccountID
	priv      ed25519.PrivateKey
}

type TestFramework struct {
	test     *testing.T
	Instance *slotattestation.Manager

	bucketedStorage *shrinkingmap.ShrinkingMap[iotago.SlotIndex, kvstore.KVStore]

	api                 iotago.API
	slotTimeProvider    *iotago.SlotTimeProvider
	attestationsByAlias *shrinkingmap.ShrinkingMap[string, *iotago.Attestation]
	issuerByAlias       *shrinkingmap.ShrinkingMap[string, *issuer]

	uniqueCounter atomic.Int64
	mutex         sync.RWMutex
}

func NewTestFramework(test *testing.T) *TestFramework {
	protocolParameters := iotago.ProtocolParameters{
		Version:     3,
		NetworkName: test.Name(),
		Bech32HRP:   "rms",
		MinPoWScore: 0,
		RentStructure: iotago.RentStructure{
			VByteCost:    100,
			VBFactorData: 1,
			VBFactorKey:  10,
		},
		TokenSupply:           1_000_0000,
		GenesisUnixTimestamp:  uint32(time.Now().Truncate(10*time.Second).Unix() - 10*100), // start 100 slots in the past at an even number.
		SlotDurationInSeconds: 10,
	}

	api := iotago.LatestAPI(&protocolParameters)
	iotago.SwapInternalAPI(api)

	t := &TestFramework{
		test:                test,
		api:                 api,
		slotTimeProvider:    api.SlotTimeProvider(),
		bucketedStorage:     shrinkingmap.New[iotago.SlotIndex, kvstore.KVStore](),
		attestationsByAlias: shrinkingmap.New[string, *iotago.Attestation](),
		issuerByAlias:       shrinkingmap.New[string, *issuer](),
	}

	bucketedStorage := func(index iotago.SlotIndex) kvstore.KVStore {
		return lo.Return1(t.bucketedStorage.GetOrCreate(index, func() kvstore.KVStore {
			return mapdb.NewMapDB()
		}))
	}

	committeeFunc := func() *account.Accounts[iotago.AccountID, *iotago.AccountID] {
		committee := account.NewAccounts[iotago.AccountID](mapdb.NewMapDB())
		t.issuerByAlias.ForEach(func(alias string, issuer *issuer) bool {
			committee.Set(issuer.accountID, 1)
			return true
		})
		return committee
	}

	t.Instance = slotattestation.NewManager(2, bucketedStorage, committeeFunc)

	return t
}

func (t *TestFramework) issuer(alias string) *issuer {
	return lo.Return1(t.issuerByAlias.GetOrCreate(alias, func() *issuer {
		pub, priv, err := ed25519.GenerateKey(nil)
		require.NoError(t.test, err)

		accountID := iotago.AccountID(*iotago.Ed25519AddressFromPubKey(pub))
		accountID.RegisterAlias(alias)

		return &issuer{
			accountID: accountID,
			priv:      priv,
		}
	}))
}

func (t *TestFramework) AddFutureAttestation(issuerAlias string, attestationAlias string, blockSlot iotago.SlotIndex, attestedSlot iotago.SlotIndex) {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	issuer := t.issuer(issuerAlias)
	issuingTime := t.slotTimeProvider.StartTime(blockSlot).Add(time.Duration(t.uniqueCounter.Add(1)))

	block, err := builder.NewBlockBuilder().
		IssuingTime(issuingTime).
		SlotCommitment(iotago.NewCommitment(attestedSlot, iotago.CommitmentID{}, iotago.Identifier{}, 0)).
		Sign(issuer.accountID, issuer.priv).
		Build()
	require.NoError(t.test, err)

	block.MustID(t.slotTimeProvider).RegisterAlias(attestationAlias)
	att := iotago.NewAttestation(block)
	t.attestationsByAlias.Set(attestationAlias, att)

	modelBlock, err := model.BlockFromBlock(block, t.api)
	require.NoError(t.test, err)

	t.Instance.AddAttestationFromBlock(blocks.NewBlock(modelBlock))
}

func (t *TestFramework) blockIDFromAttestation(att *iotago.Attestation) iotago.BlockID {
	return lo.PanicOnErr(att.BlockID(t.slotTimeProvider))
}

func (t *TestFramework) attestation(alias string) *iotago.Attestation {
	attestation, exists := t.attestationsByAlias.Get(alias)
	require.Truef(t.test, exists, "attestation with alias '%s' does not exist", alias)

	return attestation
}

func (t *TestFramework) AssertCommit(slot iotago.SlotIndex, expectedCW uint64, expectedAttestationsAliases map[string]string, optExpectedGetError ...bool) {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	cw, root, err := t.Instance.Commit(slot)
	require.NoError(t.test, err)

	require.EqualValues(t.test, expectedCW, cw)

	expectedTree := *ads.NewMap[iotago.AccountID, iotago.Attestation, *iotago.AccountID, *iotago.Attestation](mapdb.NewMapDB())
	expectedAttestations := make([]*iotago.Attestation, 0)
	for issuerAlias, attestationAlias := range expectedAttestationsAliases {
		expectedTree.Set(t.issuer(issuerAlias).accountID, t.attestation(attestationAlias))
		expectedAttestations = append(expectedAttestations, t.attestation(attestationAlias))
	}

	// Retrieve attestations from storage and compare them with the expected ones.
	tree, err := t.Instance.Get(slot)

	attestationFromTree := make([]*iotago.Attestation, 0)
	attestationBlockIDsFromTree := make([]iotago.BlockID, 0)
	if len(optExpectedGetError) == 1 && optExpectedGetError[0] {
		require.ErrorContains(t.test, err, "is smaller than attestation")
		return
	} else {
		require.NoError(t.test, err)
	}

	require.NoError(t.test, tree.Stream(func(key iotago.AccountID, value *iotago.Attestation) bool {
		attestationFromTree = append(attestationFromTree, value)
		attestationBlockIDsFromTree = append(attestationBlockIDsFromTree, t.blockIDFromAttestation(value))
		return true
	}))

	require.ElementsMatchf(t.test, expectedAttestations, attestationFromTree, "attestations from tree do not match expected ones: expected: %v, got: %v", lo.Values(expectedAttestationsAliases), attestationBlockIDsFromTree)

	require.Equal(t.test, expectedTree.Root(), root)
}