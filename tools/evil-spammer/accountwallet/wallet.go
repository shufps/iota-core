package accountwallet

import (
	"os"
	"time"

	"github.com/mr-tron/base58"

	"github.com/iotaledger/hive.go/ierrors"
	"github.com/iotaledger/hive.go/runtime/options"
	"github.com/iotaledger/hive.go/runtime/timeutil"
	"github.com/iotaledger/iota-core/pkg/testsuite/mock"
	"github.com/iotaledger/iota-core/tools/evil-spammer/logger"
	"github.com/iotaledger/iota-core/tools/evil-spammer/models"
	iotago "github.com/iotaledger/iota.go/v4"
	"github.com/iotaledger/iota.go/v4/builder"
	"github.com/iotaledger/iota.go/v4/tpkg"
)

var log = logger.New("AccountWallet")

func Run(lastFaucetUnspentOutputID iotago.OutputID) (*AccountWallet, error) {
	// read config here
	config := loadAccountConfig()

	var opts []options.Option[AccountWallet]
	if config.BindAddress != "" {
		opts = append(opts, WithClientURL(config.BindAddress))
	}
	if config.AccountStatesFile != "" {
		opts = append(opts, WithAccountStatesFile(config.AccountStatesFile))
	}

	opts = append(opts, WithFaucetUnspendOutputID(lastFaucetUnspentOutputID))

	wallet := NewAccountWallet(opts...)

	// load wallet
	err := wallet.fromAccountStateFile()
	if err != nil {
		return nil, ierrors.Wrap(err, "failed to load wallet from file")
	}

	return wallet, nil
}

func SaveState(w *AccountWallet) error {
	return w.toAccountStateFile()
}

type AccountWallet struct {
	faucet *faucet
	seed   [32]byte

	accountsAliases map[string]*models.AccountData

	//accountsStatus map[string]models.AccountMetadata
	latestUsedIndex uint64

	client *models.WebClient
	api    iotago.API

	optsClientBindAddress     string
	optsAccountStatesFile     string
	optsFaucetUnspendOutputID iotago.OutputID
	optsRequestTimeout        time.Duration
	optsRequestTicker         time.Duration
}

func NewAccountWallet(opts ...options.Option[AccountWallet]) *AccountWallet {
	return options.Apply(&AccountWallet{
		accountsAliases:    make(map[string]*models.AccountData),
		seed:               tpkg.RandEd25519Seed(),
		optsRequestTimeout: time.Second * 120,
		optsRequestTicker:  time.Second * 5,
	}, opts, func(w *AccountWallet) {
		w.client = models.NewWebClient(w.optsClientBindAddress)
		log.Infof("APi: %s", w.client.CurrentAPI())
		w.api = w.client.CurrentAPI()
		w.faucet = newFaucet(w.client, w.optsFaucetUnspendOutputID, w.api)
	})
}

func (a *AccountWallet) LastFaucetUnspentOutputID() iotago.OutputID {
	return a.faucet.unspentOutput.OutputID
}

// toAccountStateFile write account states to file.
func (a *AccountWallet) toAccountStateFile() error {
	accounts := make([]*models.AccountData, 0)

	for _, acc := range a.accountsAliases {
		accounts = append(accounts, acc)
	}

	stateBytes, err := a.api.Encode(&StateData{
		Seed:          base58.Encode(a.seed[:]),
		LastUsedIndex: a.latestUsedIndex,
		AccountsData:  accounts,
	})
	if err != nil {
		return ierrors.Wrap(err, "failed to encode state")
	}

	//nolint:gosec // users should be able to read the file
	if err = os.WriteFile(a.optsAccountStatesFile, stateBytes, 0o644); err != nil {
		return ierrors.Wrap(err, "failed to write account states to file")
	}

	return nil
}

func (a *AccountWallet) fromAccountStateFile() error {
	walletStateBytes, err := os.ReadFile(a.optsAccountStatesFile)
	if err != nil {
		if !os.IsNotExist(err) {
			return ierrors.Wrap(err, "failed to read file")
		}
		return nil
	}

	var data StateData
	_, err = a.api.Decode(walletStateBytes, &data)
	if err != nil {
		return ierrors.Wrap(err, "failed to decode from file")
	}

	// copy seeds
	decodedSeeds, err := base58.Decode(data.Seed)
	if err != nil {
		return ierrors.Wrap(err, "failed to decode seed")
	}
	copy(a.seed[:], decodedSeeds)

	// set latest used index
	a.latestUsedIndex = data.LastUsedIndex

	// account data
	for _, acc := range data.AccountsData {
		a.accountsAliases[acc.Alias] = acc
	}

	return nil
}

func (a *AccountWallet) readAccountsStateFile() (map[string]*models.AccountData, error) {
	err := a.fromAccountStateFile()
	if err != nil {
		return nil, ierrors.Wrap(err, "failed to load wallet from file")
	}
	return a.accountsAliases, nil
}

func (a *AccountWallet) registerAccount(alias string, outputID iotago.OutputID, index uint64) iotago.AccountID {
	accountID := iotago.AccountIDFromOutputID(outputID)
	a.accountsAliases[alias] = &models.AccountData{
		Alias:     alias,
		AccountID: accountID,
		Status:    models.AccountPending,
		OutputID:  outputID,
		Index:     index,
	}

	return accountID
}

func (a *AccountWallet) updateAccountStatus(alias string, status models.AccountStatus) (*models.AccountData, bool) {
	accData, exists := a.accountsAliases[alias]
	if !exists {
		return nil, false
	}

	if accData.Status == status {
		return accData, false
	}

	accData.Status = status
	a.accountsAliases[alias] = accData

	return accData, true
}

func (a *AccountWallet) getAccount(alias string) (*models.AccountData, error) {
	accData, exists := a.accountsAliases[alias]
	if !exists {
		return nil, ierrors.Errorf("account with alias %s does not exist", alias)
	}

	// check if account is ready (to be included in a commitment)
	ready := a.isAccountReady(accData)
	if !ready {
		return nil, ierrors.Errorf("account with alias %s is not ready", alias)
	}

	accData, _ = a.updateAccountStatus(alias, models.AccountReady)

	return accData, nil
}

func (a *AccountWallet) isAccountReady(accData *models.AccountData) bool {
	if accData.Status == models.AccountReady {
		return true
	}

	creationSlot := accData.OutputID.CreationSlot()

	// wait for the account to be committed
	log.Infof("Waiting for account %s to be committed within slot %d...", accData.Alias, creationSlot)
	err := a.retry(func() (bool, error) {
		resp, err := a.client.GetBlockIssuance()
		if err != nil {
			return false, err
		}

		if resp.Commitment.Slot >= creationSlot {
			log.Infof("Slot %d commited, account %s is ready to use", creationSlot, accData.Alias)
			return true, nil
		}

		return false, nil
	})

	if err != nil {
		log.Errorf("failed to get commitment details while waiting %s: %s", accData.Alias, err)
		return false
	}

	return true
}

func (a *AccountWallet) getFunds(amount uint64, addressType iotago.AddressType) (*models.Output, error) {
	hdWallet := mock.NewHDWallet("", a.seed[:], a.latestUsedIndex+1)

	receiverAddr := hdWallet.Address(addressType)
	createdOutput, err := a.RequestFaucetFunds(a.client, receiverAddr, iotago.BaseToken(amount))
	if err != nil {
		return nil, ierrors.Wrap(err, "failed to request funds from Faucet")
	}

	a.latestUsedIndex++
	createdOutput.Index = a.latestUsedIndex

	return createdOutput, nil
}

func (a *AccountWallet) destroyAccount(alias string) error {
	accData, err := a.getAccount(alias)
	if err != nil {
		return err
	}
	hdWallet := mock.NewHDWallet("", a.seed[:], accData.Index)

	// get output from node
	// From TIP42: Indexers and node plugins shall map the account address of the output derived with Account ID to the regular address -> output mapping table, so that given an Account Address, its most recent unspent account output can be retrieved.
	// TODO: use correct outputID
	accountOutput := a.client.GetOutput(accData.OutputID)

	txBuilder := builder.NewTransactionBuilder(a.api)
	txBuilder.AddInput(&builder.TxInput{
		UnlockTarget: a.accountsAliases[alias].AccountID.ToAddress(),
		InputID:      accData.OutputID,
		Input:        accountOutput,
	})

	// send all tokens to faucet
	txBuilder.AddOutput(&iotago.BasicOutput{
		Amount: accountOutput.BaseTokenAmount(),
		Conditions: iotago.BasicOutputUnlockConditions{
			&iotago.AddressUnlockCondition{Address: a.faucet.address},
		},
	})

	tx, err := txBuilder.Build(hdWallet.AddressSigner())
	if err != nil {
		return ierrors.Wrap(err, "failed to build transaction")
	}

	_, err = a.client.PostTransaction(tx)
	if err != nil {
		return ierrors.Wrap(err, "failed to post transaction")
	}

	// remove account from wallet
	delete(a.accountsAliases, alias)

	log.Infof("Account %s has been destroyed", alias)
	return nil
}

func (a *AccountWallet) retry(requestFunc func() (bool, error)) error {
	timeout := time.NewTimer(a.optsRequestTimeout)
	interval := time.NewTicker(a.optsRequestTicker)
	defer timeutil.CleanupTimer(timeout)
	defer timeutil.CleanupTicker(interval)

	for {
		done, err := requestFunc()
		if err != nil {
			return err
		}
		if done {
			return nil
		}
		select {
		case <-interval.C:
			continue
		case <-timeout.C:
			return ierrors.New("timeout while trying to request")
		}
	}
}
