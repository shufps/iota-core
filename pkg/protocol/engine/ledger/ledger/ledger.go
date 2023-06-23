package ledger

import (
	cryptoed25519 "crypto/ed25519"
	"fmt"
	"io"

	"github.com/pkg/errors"
	"golang.org/x/xerrors"

	"github.com/iotaledger/hive.go/crypto/ed25519"
	"github.com/iotaledger/hive.go/ds/advancedset"
	"github.com/iotaledger/hive.go/kvstore"
	"github.com/iotaledger/hive.go/lo"
	"github.com/iotaledger/hive.go/runtime/event"
	"github.com/iotaledger/hive.go/runtime/module"
	"github.com/iotaledger/iota-core/pkg/core/promise"
	"github.com/iotaledger/iota-core/pkg/core/vote"
	"github.com/iotaledger/iota-core/pkg/model"
	"github.com/iotaledger/iota-core/pkg/protocol/engine"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/accounts"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/accounts/accountsledger"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/accounts/mana"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/blocks"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/ledger"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/mempool"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/mempool/conflictdag"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/mempool/conflictdag/conflictdagv1"
	mempoolv1 "github.com/iotaledger/iota-core/pkg/protocol/engine/mempool/v1"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/sybilprotection"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/utxoledger"
	"github.com/iotaledger/iota-core/pkg/storage/prunable"
	iotago "github.com/iotaledger/iota.go/v4"
)

var ErrUnexpectedUnderlyingType = errors.New("unexpected underlying type provided by the interface")

type Ledger struct {
	events *ledger.Events

	apiProvider func() iotago.API

	utxoLedger       *utxoledger.Manager
	accountsLedger   *accountsledger.Manager
	manaManager      *mana.Manager
	commitmentLoader func(iotago.SlotIndex) (*model.Commitment, error)

	sybilProtection sybilprotection.SybilProtection

	memPool            mempool.MemPool[ledger.BlockVoteRank]
	conflictDAG        conflictdag.ConflictDAG[iotago.TransactionID, iotago.OutputID, ledger.BlockVoteRank]
	errorHandler       func(error)
	protocolParameters *iotago.ProtocolParameters

	manaDecayProvider *iotago.ManaDecayProvider

	module.Module
}

func NewProvider() module.Provider[*engine.Engine, ledger.Ledger] {
	return module.Provide(func(e *engine.Engine) ledger.Ledger {
		l := New(

			e.Storage.Ledger(),
			e.Storage.Accounts(),
			e.Storage.Commitments().Load,
			e.BlockCache.Block,
			e.Storage.AccountDiffs,
			e.API,
			e.SybilProtection,
			e.ErrorHandler("ledger"),
		)

		// TODO: when should ledgerState be pruned?

		e.HookConstructed(func() {
			e.Events.Ledger.LinkTo(l.events)
			e.Events.ConflictDAG.LinkTo(l.conflictDAG.Events())

			l.memPool = mempoolv1.New(l.executeStardustVM, l.resolveState, e.Workers.CreateGroup("MemPool"), l.conflictDAG, mempoolv1.WithForkAllTransactions[ledger.BlockVoteRank](true))
			e.EvictionState.Events.SlotEvicted.Hook(l.memPool.Evict)

			wpAccounts := e.Workers.CreateGroup("Accounts").CreatePool("trackBurnt", 1)
			e.Events.BlockGadget.BlockAccepted.Hook(func(block *blocks.Block) {
				l.accountsLedger.TrackBlock(block)
				l.BlockAccepted(block)
				l.events.BlockProcessed.Trigger(block)
			}, event.WithWorkerPool(wpAccounts))
			e.Events.BlockGadget.BlockPreAccepted.Hook(l.blockPreAccepted)
		})
		e.HookInitialized(func() {
			l.protocolParameters = e.Storage.Settings().ProtocolParameters()
			l.manaDecayProvider = l.protocolParameters.ManaDecayProvider()
			l.manaManager = mana.NewManager(l.manaDecayProvider, l.resolveAccountOutput)
			l.TriggerConstructed()

			l.accountsLedger.SetMaxCommittableAge(l.protocolParameters.MaxCommittableAge)
			l.accountsLedger.SetLatestCommittedSlot(e.Storage.Settings().LatestCommitment().Index())

			l.TriggerInitialized()
		})

		return l
	})
}

func New(
	utxoStore kvstore.KVStore,
	accountsStore kvstore.KVStore,
	commitmentLoader func(iotago.SlotIndex) (*model.Commitment, error),
	blocksFunc func(id iotago.BlockID) (*blocks.Block, bool),
	slotDiffFunc func(iotago.SlotIndex) *prunable.AccountDiffs,
	apiProvider func() iotago.API,
	sybilProtection sybilprotection.SybilProtection,
	errorHandler func(error),
) *Ledger {
	return &Ledger{
		events:           ledger.NewEvents(),
		apiProvider:      apiProvider,
		accountsLedger:   accountsledger.New(blocksFunc, slotDiffFunc, accountsStore, apiProvider()),
		utxoLedger:       utxoledger.New(utxoStore, apiProvider),
		commitmentLoader: commitmentLoader,
		sybilProtection:  sybilProtection,
		errorHandler:     errorHandler,
		conflictDAG:      conflictdagv1.New[iotago.TransactionID, iotago.OutputID, ledger.BlockVoteRank](sybilProtection.OnlineCommittee().Size),
	}
}

func (l *Ledger) OnTransactionAttached(handler func(transaction mempool.TransactionMetadata), opts ...event.Option) {
	l.memPool.OnTransactionAttached(handler, opts...)
}

func (l *Ledger) AttachTransaction(block *blocks.Block) (transactionMetadata mempool.TransactionMetadata, containsTransaction bool) {
	switch payload := block.Block().Payload.(type) {
	case mempool.Transaction:
		transactionMetadata, err := l.memPool.AttachTransaction(payload, block.ID())
		if err != nil {
			l.errorHandler(err)

			return nil, true
		}

		return transactionMetadata, true
	default:

		return nil, false
	}
}

func (l *Ledger) CommitSlot(index iotago.SlotIndex) (stateRoot iotago.Identifier, mutationRoot iotago.Identifier, accountRoot iotago.Identifier, err error) {
	ledgerIndex, err := l.utxoLedger.ReadLedgerIndex()
	if err != nil {
		return iotago.Identifier{}, iotago.Identifier{}, iotago.Identifier{}, err
	}

	if index != ledgerIndex+1 {
		panic(fmt.Errorf("there is a gap in the ledgerstate %d vs %d", ledgerIndex, index))
	}

	stateDiff := l.memPool.StateDiff(index)

	// collect outputs and allotments from the "uncompacted" stateDiff
	// outputs need to be processed in the "uncompacted" version of the state diff, as we need to be able to store
	// and retrieve intermediate outputs to show to the user
	spends, outputs, accountDiffs, err := l.processStateDiffTransactions(stateDiff)
	if err != nil {
		return iotago.Identifier{}, iotago.Identifier{}, iotago.Identifier{}, xerrors.Errorf("failed to process state diff transactions in slot %d: %w", index, err)
	}

	// Now we process the collected account changes, for that we consume the "compacted" state diff to get the overall
	// account changes at UTXO level without needing to worry about multiple spends of the same account in the same slot,
	// we only care about the initial account output to be consumed and the final account output to be created.
	// output side
	createdAccounts, consumedAccounts, destroyedAccounts, err := l.getCreatedAndConsumedAccountOutputs(stateDiff)
	if err != nil {
		return iotago.Identifier{}, iotago.Identifier{}, iotago.Identifier{}, xerrors.Errorf("failed to process outputs consumed and created in slot %d: %w", index, err)
	}

	l.prepareAccountDiffs(accountDiffs, index, consumedAccounts, createdAccounts)

	// Commit the changes
	// Update the mana manager's cache
	l.manaManager.ApplyDiff(index, destroyedAccounts, createdAccounts)

	// Update the UTXO ledger
	if err = l.utxoLedger.ApplyDiff(index, outputs, spends); err != nil {
		return iotago.Identifier{}, iotago.Identifier{}, iotago.Identifier{}, xerrors.Errorf("failed to apply diff to UTXO ledger for index %d: %w", index, err)
	}

	// Update the Accounts ledger
	if err = l.accountsLedger.ApplyDiff(index, accountDiffs, destroyedAccounts); err != nil {
		return iotago.Identifier{}, iotago.Identifier{}, iotago.Identifier{}, xerrors.Errorf("failed to apply diff to Accounts ledger for index %d: %w", index, err)
	}

	// Mark each transaction as committed so the mempool can evict it
	stateDiff.ExecutedTransactions().ForEach(func(_ iotago.TransactionID, tx mempool.TransactionMetadata) bool {
		tx.Commit()
		return true
	})

	return l.utxoLedger.StateTreeRoot(), iotago.Identifier(stateDiff.Mutations().Root()), l.accountsLedger.AccountsTreeRoot(), nil
}

func (l *Ledger) AddAccount(output *utxoledger.Output) error {
	return l.accountsLedger.AddAccount(output)
}

func (l *Ledger) AddUnspentOutput(unspentOutput *utxoledger.Output) error {
	return l.utxoLedger.AddUnspentOutput(unspentOutput)
}

func (l *Ledger) BlockAccepted(block *blocks.Block) {
	switch block.Block().Payload.(type) {
	case *iotago.Transaction:
		l.memPool.MarkAttachmentIncluded(block.ID())

	default:
		return
	}
}

func (l *Ledger) Account(accountID iotago.AccountID, optTargetIndex ...iotago.SlotIndex) (accountData *accounts.AccountData, exists bool, err error) {
	return l.accountsLedger.Account(accountID, optTargetIndex...)
}

func (l *Ledger) Output(stateRef iotago.IndexedUTXOReferencer) (*utxoledger.Output, error) {
	stateWithMetadata, err := l.memPool.StateMetadata(stateRef)
	if err != nil {
		return nil, err
	}

	switch castState := stateWithMetadata.State().(type) {
	case *utxoledger.Output:
		return castState, nil
	case *ExecutionOutput:
		txWithMetadata, exists := l.memPool.TransactionMetadata(stateRef.Ref().TransactionID())
		if !exists {
			return nil, err
		}

		earliestAttachment := txWithMetadata.EarliestIncludedAttachment()

		tx, ok := txWithMetadata.Transaction().(*iotago.Transaction)
		if !ok {
			return nil, ErrUnexpectedUnderlyingType
		}

		return utxoledger.CreateOutput(l.utxoLedger.API(), stateWithMetadata.State().OutputID(), earliestAttachment, earliestAttachment.Index(), tx.Essence.CreationTime, stateWithMetadata.State().Output()), nil
	default:
		panic("unexpected State type")
	}
}

func (l *Ledger) IsOutputUnspent(outputID iotago.OutputID) (bool, error) {
	return l.utxoLedger.IsOutputIDUnspentWithoutLocking(outputID)
}

func (l *Ledger) Spent(outputID iotago.OutputID) (*utxoledger.Spent, error) {
	return l.utxoLedger.ReadSpentForOutputIDWithoutLocking(outputID)
}

func (l *Ledger) StateDiffs(index iotago.SlotIndex) (*utxoledger.SlotDiff, error) {
	return l.utxoLedger.SlotDiffWithoutLocking(index)
}

func (l *Ledger) TransactionMetadata(transactionID iotago.TransactionID) (mempool.TransactionMetadata, bool) {
	return l.memPool.TransactionMetadata(transactionID)
}

func (l *Ledger) TransactionMetadataByAttachment(blockID iotago.BlockID) (mempool.TransactionMetadata, bool) {
	return l.memPool.TransactionMetadataByAttachment(blockID)
}

func (l *Ledger) ConflictDAG() conflictdag.ConflictDAG[iotago.TransactionID, iotago.OutputID, ledger.BlockVoteRank] {
	return l.conflictDAG
}

func (l *Ledger) Import(reader io.ReadSeeker) error {
	if err := l.utxoLedger.Import(reader); err != nil {
		return errors.Wrap(err, "failed to import utxoLedger")
	}

	if err := l.accountsLedger.Import(reader); err != nil {
		return errors.Wrap(err, "failed to import accountsLedger")
	}

	return nil
}

func (l *Ledger) Export(writer io.WriteSeeker, targetIndex iotago.SlotIndex) error {
	if err := l.utxoLedger.Export(writer, targetIndex); err != nil {
		return errors.Wrap(err, "failed to export utxoLedger")
	}

	if err := l.accountsLedger.Export(writer, targetIndex); err != nil {
		return errors.Wrap(err, "failed to export accountsLedger")
	}

	return nil
}

func (l *Ledger) Shutdown() {
	l.TriggerStopped()
	l.conflictDAG.Shutdown()
}

// Process the collected account changes. The consumedAccounts and createdAccounts maps only contain outputs with a
// BIC feature, so allotments made to account without a BIC feature are not tracked here, and they are burned as a result.
// There are 3 possible cases:
// 1. The account was only consumed but not created in this slot, therefore, it is marked as destroyed, and its latest
// state is stored as diff to allow a rollback.
// 2. The account was consumed and created in the same slot, the account was transitioned, and we have to store the
// changes in the diff.
// 3. The account was only created in this slot, in this case we need to track the output's values as the diff.
func (l *Ledger) prepareAccountDiffs(accountDiffs map[iotago.AccountID]*prunable.AccountDiff, index iotago.SlotIndex, consumedAccounts map[iotago.AccountID]*utxoledger.Output, createdAccounts map[iotago.AccountID]*utxoledger.Output) {
	for consumedAccountID, consumedOutput := range consumedAccounts {
		// We might have had an allotment on this account, and the diff already exists
		accountDiff, exists := accountDiffs[consumedAccountID]
		if !exists {
			accountDiff = prunable.NewAccountDiff()
			accountDiffs[consumedAccountID] = accountDiff
		}

		// Obtain account state at the current latest committed slot, which is index-1
		accountData, exists, err := l.accountsLedger.Account(consumedAccountID, index-1)
		if err != nil {
			panic(fmt.Errorf("error loading account %s in slot %d: %w", consumedAccountID, index-1, err))
		}
		if !exists {
			panic(fmt.Errorf("could not find destroyed account %s in slot %d", consumedAccountID, index-1))
		}

		// case 1. the account was destroyed, the diff will be created inside accountLedger on account deletion
		// case 2. the account was transitioned, fill in the diff with the delta information
		// Change and PreviousUpdatedTime are either 0 if we did not have an allotment for this account, or we already
		// have some values from the allotment, so no need to set them explicitly.
		createdOutput, accountTransitioned := createdAccounts[consumedAccountID]
		if !accountTransitioned {
			continue
		}
		accountDiff.NewOutputID = createdOutput.OutputID()
		accountDiff.PreviousOutputID = consumedOutput.OutputID()

		oldPubKeysSet := accountData.PubKeys
		newPubKeysSet := advancedset.New[ed25519.PublicKey]()
		for _, pubKey := range createdOutput.Output().FeatureSet().BlockIssuer().BlockIssuerKeys {
			newPubKeysSet.Add(ed25519.PublicKey(pubKey))
		}

		// Add public keys that are not in the old set
		accountDiff.PubKeysAdded = newPubKeysSet.Filter(func(key ed25519.PublicKey) bool {
			return !oldPubKeysSet.Has(key)
		}).Slice()

		// Remove the keys that are not in the new set
		accountDiff.PubKeysRemoved = oldPubKeysSet.Filter(func(key ed25519.PublicKey) bool {
			return !newPubKeysSet.Has(key)
		}).Slice()
	}

	// case 3. the account was created, fill in the diff with the information of the created output.
	for createdAccountID, createdOutput := range createdAccounts {
		// If it is also consumed we are in case 2 that was handled above.
		if _, exists := consumedAccounts[createdAccountID]; exists {
			continue
		}

		// We might have had an allotment on this account, and the diff already exists
		accountDiff, exists := accountDiffs[createdAccountID]
		if !exists {
			accountDiff = prunable.NewAccountDiff()
			accountDiffs[createdAccountID] = accountDiff
		}
		// Change and PreviousUpdatedTime are either 0 if we did not have an allotment for this account, or we already
		// have some values from the allotment, so no need to set them explicitly.
		accountDiff.NewOutputID = createdOutput.OutputID()
		accountDiff.PreviousOutputID = iotago.OutputID{}
		accountDiff.PubKeysAdded = lo.Map(createdOutput.Output().FeatureSet().BlockIssuer().BlockIssuerKeys, func(pk cryptoed25519.PublicKey) ed25519.PublicKey { return ed25519.PublicKey(pk) })
	}
}

func (l *Ledger) getCreatedAndConsumedAccountOutputs(stateDiff mempool.StateDiff) (createdAccounts map[iotago.AccountID]*utxoledger.Output, consumedAccounts map[iotago.AccountID]*utxoledger.Output, destroyedAccounts *advancedset.AdvancedSet[iotago.AccountID], err error) {
	createdAccounts = make(map[iotago.AccountID]*utxoledger.Output)
	consumedAccounts = make(map[iotago.AccountID]*utxoledger.Output)
	destroyedAccounts = advancedset.New[iotago.AccountID]()

	stateDiff.CreatedStates().ForEachKey(func(outputID iotago.OutputID) bool {
		createdOutput, errOutput := l.Output(outputID.UTXOInput())
		if errOutput != nil {
			err = xerrors.Errorf("failed to retrieve output %s: %w", outputID, errOutput)
			return false
		}

		if createdOutput.OutputType() == iotago.OutputAccount {
			createdAccount, _ := createdOutput.Output().(*iotago.AccountOutput)

			// Skip if the account doesn't have a BIC feature.
			if createdAccount.FeatureSet().BlockIssuer() == nil {
				return true
			}

			accountID := createdAccount.AccountID
			if accountID.Empty() {
				accountID = iotago.AccountIDFromOutputID(outputID)
			}

			createdAccounts[accountID] = createdOutput
		}

		return true
	})

	if err != nil {
		return nil, nil, nil, xerrors.Errorf("error while processing created states: %w", err)
	}

	// input side
	stateDiff.DestroyedStates().ForEachKey(func(outputID iotago.OutputID) bool {
		spentOutput, errOutput := l.Output(outputID.UTXOInput())
		if errOutput != nil {
			err = xerrors.Errorf("failed to retrieve output %s: %w", outputID, errOutput)
			return false
		}

		if spentOutput.OutputType() == iotago.OutputAccount {
			consumedAccount, _ := spentOutput.Output().(*iotago.AccountOutput)
			// Skip if the account doesn't have a BIC feature.
			if consumedAccount.FeatureSet().BlockIssuer() == nil {
				return true
			}
			consumedAccounts[consumedAccount.AccountID] = spentOutput

			// if we have consumed accounts that are not created in the same slot, we need to track them as destroyed
			if _, exists := createdAccounts[consumedAccount.AccountID]; !exists {
				destroyedAccounts.Add(consumedAccount.AccountID)
			}
		}

		return true
	})

	if err != nil {
		return nil, nil, nil, xerrors.Errorf("error while processing created states: %w", err)
	}

	return createdAccounts, consumedAccounts, destroyedAccounts, nil
}

func (l *Ledger) processStateDiffTransactions(stateDiff mempool.StateDiff) (spends utxoledger.Spents, outputs utxoledger.Outputs, accountDiffs map[iotago.AccountID]*prunable.AccountDiff, err error) {
	accountDiffs = make(map[iotago.AccountID]*prunable.AccountDiff)

	stateDiff.ExecutedTransactions().ForEach(func(txID iotago.TransactionID, txWithMeta mempool.TransactionMetadata) bool {
		tx, ok := txWithMeta.Transaction().(*iotago.Transaction)
		if !ok {
			err = ErrUnexpectedUnderlyingType
			return false
		}
		txCreationTime := tx.Essence.CreationTime

		inputRefs, errInput := tx.Inputs()
		if errInput != nil {
			err = xerrors.Errorf("failed to retrieve inputs of %s: %w", txID, errInput)
			return false
		}

		// process outputs
		{
			// input side
			for _, inputRef := range inputRefs {
				inputState, outputErr := l.Output(inputRef)
				if outputErr != nil {
					err = xerrors.Errorf("failed to retrieve outputs of %s: %w", txID, errInput)
					return false
				}

				spend := utxoledger.NewSpent(inputState, txWithMeta.ID(), stateDiff.Index())
				spends = append(spends, spend)
			}

			// output side
			txWithMeta.Outputs().Range(func(stateMetadata mempool.StateMetadata) {
				output := utxoledger.CreateOutput(l.utxoLedger.API(), stateMetadata.State().OutputID(), txWithMeta.EarliestIncludedAttachment(), stateDiff.Index(), txCreationTime, stateMetadata.State().Output())
				outputs = append(outputs, output)
			})
		}

		// process allotments
		{
			for _, allotment := range tx.Essence.Allotments {
				accountDiff, exists := accountDiffs[allotment.AccountID]
				if !exists {
					// allotments won't change the outputID of the Account, so the diff defaults to empty new and previous outputIDs
					accountDiff = prunable.NewAccountDiff()
					accountDiffs[allotment.AccountID] = accountDiff
				}
				accountData, exists, accountErr := l.accountsLedger.Account(allotment.AccountID)
				if accountErr != nil {
					panic(fmt.Errorf("error loading account %s in slot %d: %w", allotment.AccountID, stateDiff.Index()-1, accountErr))
				}
				// if the account does not exist in our AccountsLedger it means it doesn't have a BIC feature, so
				// we burn this allotment.
				if !exists {
					continue
				}

				accountDiff.Change += int64(allotment.Value)
				accountDiff.PreviousUpdatedTime = accountData.Credits.UpdateTime

				// we are not transitioning the allotted account, so the new and previous outputIDs are the same
				accountDiff.NewOutputID = accountData.OutputID
				accountDiff.PreviousOutputID = accountData.OutputID
			}
		}

		return true
	})

	return spends, outputs, accountDiffs, nil
}

func (l *Ledger) resolveAccountOutput(accountID iotago.AccountID, slotIndex iotago.SlotIndex) (*utxoledger.Output, error) {
	accountMetadata, _, err := l.accountsLedger.Account(accountID, slotIndex)
	if err != nil {
		return nil, xerrors.Errorf("could not get account information for account %s in slot %d: %w", accountID, slotIndex, err)
	}

	l.utxoLedger.ReadLockLedger()
	defer l.utxoLedger.ReadUnlockLedger()

	isUnspent, err := l.utxoLedger.IsOutputIDUnspentWithoutLocking(accountMetadata.OutputID)
	if err != nil {
		return nil, xerrors.Errorf("error while checking account output %s is unspent: %w", accountMetadata.OutputID, err)
	}
	if !isUnspent {
		return nil, xerrors.Errorf("unspent account output %s not found: %w", accountMetadata.OutputID, mempool.ErrStateNotFound)
	}

	accountOutput, err := l.utxoLedger.ReadOutputByOutputIDWithoutLocking(accountMetadata.OutputID)
	if err != nil {
		return nil, xerrors.Errorf("error while retrieving account output %s: %w", accountMetadata.OutputID, err)
	}

	return accountOutput, nil
}

func (l *Ledger) resolveState(stateRef iotago.IndexedUTXOReferencer) *promise.Promise[mempool.State] {
	p := promise.New[mempool.State]()

	l.utxoLedger.ReadLockLedger()
	defer l.utxoLedger.ReadUnlockLedger()

	isUnspent, err := l.utxoLedger.IsOutputIDUnspentWithoutLocking(stateRef.Ref())
	if err != nil {
		p.Reject(xerrors.Errorf("error while retrieving output %s: %w", stateRef.Ref(), err))
	}

	if !isUnspent {
		p.Reject(xerrors.Errorf("unspent output %s not found: %w", stateRef.Ref(), mempool.ErrStateNotFound))
	}

	// possible to cast `stateRef` to more specialized interfaces here, e.g. for DustOutput
	output, err := l.utxoLedger.ReadOutputByOutputID(stateRef.Ref())

	if err != nil {
		p.Reject(xerrors.Errorf("output %s not found: %w", stateRef.Ref(), mempool.ErrStateNotFound))
	} else {
		p.Resolve(output)
	}

	return p
}

func (l *Ledger) blockPreAccepted(block *blocks.Block) {
	voteRank := ledger.NewBlockVoteRank(block.ID(), block.Block().IssuingTime)

	seat, exists := l.sybilProtection.Committee(block.ID().Index()).GetSeat(block.Block().IssuerID)
	if !exists {
		return
	}

	if err := l.conflictDAG.CastVotes(vote.NewVote(seat, voteRank), block.ConflictIDs()); err != nil {
		// TODO: here we need to check what kind of error and potentially mark the block as invalid.
		//  Do we track witness weight of invalid blocks?
		l.errorHandler(errors.Wrapf(err, "failed to cast votes for block %s", block.ID()))
	}
}