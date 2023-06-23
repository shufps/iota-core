package testsuite

import (
	"github.com/google/go-cmp/cmp"
	"github.com/pkg/errors"

	"github.com/iotaledger/iota-core/pkg/protocol/engine/accounts"
	"github.com/iotaledger/iota-core/pkg/storage/prunable"
	"github.com/iotaledger/iota-core/pkg/testsuite/mock"
	iotago "github.com/iotaledger/iota.go/v4"
)

func (t *TestSuite) AssertAccountData(accountData *accounts.AccountData, node *mock.Node) {
	t.Eventually(func() error {
		actualAccountData, exists, err := node.Protocol.MainEngineInstance().Ledger.Account(accountData.ID, node.Protocol.SyncManager.LatestCommittedSlot())
		if err != nil {
			return errors.Wrap(err, "AssertAccountData: failed to load account data")
		}
		if !exists {
			return errors.Errorf("AssertAccountData: %s: account %s does not exist with latest committed slot %d", node.Name, accountData.ID, node.Protocol.SyncManager.LatestCommittedSlot())
		}

		if accountData.ID != actualAccountData.ID {
			return errors.Errorf("AssertAccountData: %s: expected %s, got %s", node.Name, accountData.ID, actualAccountData.ID)
		}

		if accountData.Credits.Value != actualAccountData.Credits.Value {
			return errors.Errorf("AssertAccountData: %s: accountID %s expected credits value %d, got %d", node.Name, accountData.ID, accountData.Credits.Value, actualAccountData.Credits.Value)
		}

		if accountData.Credits.UpdateTime != actualAccountData.Credits.UpdateTime {
			return errors.Errorf("AssertAccountData: %s: accountID %s expected credits update time %d, got %d", node.Name, accountData.ID, accountData.Credits.UpdateTime, actualAccountData.Credits.UpdateTime)
		}

		if accountData.OutputID != actualAccountData.OutputID {
			return errors.Errorf("AssertAccountData: %s: accountID %s expected output %s, got %s", node.Name, accountData.ID, accountData.OutputID, actualAccountData.OutputID)
		}

		if !cmp.Equal(accountData.PubKeys.Slice(), actualAccountData.PubKeys.Slice()) {
			return errors.Errorf("AssertAccountData: %s: accountID %s expected pub keys %s, got %s", node.Name, accountData.ID, accountData.PubKeys, actualAccountData.PubKeys)
		}

		return nil
	})
}

func (t *TestSuite) AssertAccountDiff(accountID iotago.AccountID, index iotago.SlotIndex, accountDiff *prunable.AccountDiff, destroyed bool, node *mock.Node) {
	t.Eventually(func() error {
		accountsDiffStorage := node.Protocol.MainEngineInstance().Storage.AccountDiffs(index)

		if has, err := accountsDiffStorage.Has(accountID); err != nil {
			return errors.Wrapf(err, "AssertAccountDiff: %s: failed to load accounts diff for slot %d", node.Name, index)
		} else if !has {
			return errors.Wrapf(err, "AssertAccountDiff: %s: accounts diff for slot %d does not contain account %s", node.Name, index, accountID)
		}

		actualAccountDiff, actualDestroyed, err := accountsDiffStorage.Load(accountID)
		if err != nil {
			return errors.Wrapf(err, "AssertAccountDiff: %s: failed to load account diff for account %s at slot %d", node.Name, accountID, index)
		}

		if destroyed != actualDestroyed {
			return errors.Errorf("AssertAccountDiff: %s: expected destroyed %t but actual %t for account %s at slot %d", node.Name, destroyed, actualDestroyed, accountID, index)
		}

		if accountDiff.Change != actualAccountDiff.Change {
			return errors.Errorf("AssertAccountDiff: %s: expected change %d but actual %d for account %s at slot %d", node.Name, accountDiff.Change, actualAccountDiff.Change, accountID, index)
		}

		if accountDiff.PreviousUpdatedTime != actualAccountDiff.PreviousUpdatedTime {
			return errors.Errorf("AssertAccountDiff: %s: expected previous updated time %d but actual %d for account %s at slot %d", node.Name, accountDiff.PreviousUpdatedTime, actualAccountDiff.PreviousUpdatedTime, accountID, index)
		}

		if accountDiff.NewOutputID != actualAccountDiff.NewOutputID {
			return errors.Errorf("AssertAccountDiff: %s: expected new output ID %s but actual %s for account %s at slot %d", node.Name, accountDiff.NewOutputID, actualAccountDiff.NewOutputID, accountID, index)
		}

		if accountDiff.PreviousOutputID != actualAccountDiff.PreviousOutputID {
			return errors.Errorf("AssertAccountDiff: %s: expected previous output ID %s but actual %s for account %s at slot %d", node.Name, accountDiff.PreviousOutputID, actualAccountDiff.PreviousOutputID, accountID, index)
		}

		if !cmp.Equal(accountDiff.PubKeysAdded, actualAccountDiff.PubKeysAdded) {
			return errors.Errorf("AssertAccountDiff: %s: expected pub keys added %s but actual %s for account %s at slot %d", node.Name, accountDiff.PubKeysAdded, actualAccountDiff.PubKeysAdded, accountID, index)
		}

		if !cmp.Equal(accountDiff.PubKeysRemoved, actualAccountDiff.PubKeysRemoved) {
			return errors.Errorf("AssertAccountDiff: %s: expected pub keys removed %s but actual %s for account %s at slot %d", node.Name, accountDiff.PubKeysRemoved, actualAccountDiff.PubKeysRemoved, accountID, index)
		}

		return nil
	})
}