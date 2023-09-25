package utxoledger

import (
	"github.com/iotaledger/hive.go/kvstore"
	"github.com/iotaledger/hive.go/serializer/v2/marshalutil"
	iotago "github.com/iotaledger/iota.go/v4"
)

// OutputIDConsumer is a function that consumes an output ID.
// Returning false from this function indicates to abort the iteration.
type OutputIDConsumer func(outputID iotago.OutputID) bool

// OutputConsumer is a function that consumes an output.
// Returning false from this function indicates to abort the iteration.
type OutputConsumer func(output *Output) bool

type LookupKey []byte

func lookupKeyUnspentOutput(outputID iotago.OutputID) LookupKey {
	ms := marshalutil.New(iotago.OutputIDLength + 1)
	ms.WriteByte(StoreKeyPrefixOutputUnspent) // 1 byte
	ms.WriteBytes(outputID[:])                // iotago.OutputIDLength bytes

	return ms.Bytes()
}

func (o *Output) UnspentLookupKey() LookupKey {
	return lookupKeyUnspentOutput(o.outputID)
}

func outputIDFromDatabaseKey(key LookupKey) (iotago.OutputID, error) {
	ms := marshalutil.New([]byte(key))

	// prefix
	if _, err := ms.ReadByte(); err != nil {
		return iotago.OutputID{}, err
	}

	return ParseOutputID(ms)
}

func markAsUnspent(output *Output, mutations kvstore.BatchedMutations) error {
	return mutations.Set(output.UnspentLookupKey(), []byte{})
}

func markAsSpent(output *Output, mutations kvstore.BatchedMutations) error {
	return deleteOutputLookups(output, mutations)
}

func deleteOutputLookups(output *Output, mutations kvstore.BatchedMutations) error {
	return mutations.Delete(output.UnspentLookupKey())
}

func (m *Manager) IsOutputIDUnspentWithoutLocking(outputID iotago.OutputID) (bool, error) {
	return m.store.Has(lookupKeyUnspentOutput(outputID))
}

func (m *Manager) IsOutputUnspentWithoutLocking(output *Output) (bool, error) {
	return m.store.Has(output.UnspentLookupKey())
}

func storeSpentAndMarkOutputAsSpent(spent *Spent, mutations kvstore.BatchedMutations) error {
	if err := storeSpent(spent, mutations); err != nil {
		return err
	}

	return markAsSpent(spent.output, mutations)
}

func deleteSpentAndMarkOutputAsUnspent(spent *Spent, mutations kvstore.BatchedMutations) error {
	if err := deleteSpent(spent, mutations); err != nil {
		return err
	}

	return markAsUnspent(spent.output, mutations)
}
