package modules

import (
	"errors"

	"github.com/NebulousLabs/Sia/types"
)

const (
	WalletDir = "wallet"

	PublicKeysPerSeed = 100
)

var (
	LowBalanceErr = errors.New("Insufficient Balance")
)

// WalletTransaction contains the metadata of a single output that changed the
// balance of the wallet, either incoming or outgoing (which can be gleaned
// from the 'Source' and 'Destination'. A WalletTransaction will never contain
// a refund output.
type WalletTransaction struct {
	TransactionID crypto.Hash
	Confirmations uint64
	ConfirmationHeight types.BlockHeight
	ConfirmationTimestamp types.Timestamp
	Transaction types.Transaction

	FundType types.Specifier
	Source types.UnlockHash
	Destination types.UnlockHash
	Value types.Currency

}

// The TransactionBuilder is used to construct custom transactions. A
// transaction builder is intialized via 'RegisterTransaction' and then can be
// modified by adding funds or other fields. The transaction is completed by
// calling 'Sign', which will sign all inputs added via the 'FundSiacoins' or
// 'FundSiafunds' call. All modifications are additive.
//
// Parents of the transaction are kept in the transaction builder. A parent is
// any unconfirmed transaction that is required for the child to be valid.
//
// Transaction builders are not thread safe.
type TransactionBuilder interface {
	// FundSiacoins will add a siacoin input of exaclty 'amount' to the
	// transaction. A parent transaction may be needed to achieve an input with
	// the correct value. The siacoin input will not be signed until 'Sign' is
	// called on the transaction builder.
	FundSiacoins(amount types.Currency) error

	// FundSiafunds will add a siafund input of exaclty 'amount' to the
	// transaction. A parent transaction may be needed to achieve an input with
	// the correct value. The siafund input will not be signed until 'Sign' is
	// called on the transaction builder. Any siacoins that are released by
	// spending the siafund outputs will be sent to another address owned by
	// the wallet.
	FundSiafunds(amount types.Currency) error

	// AddMinerFee adds a miner fee to the transaction, returning the index of
	// the miner fee within the transaction.
	AddMinerFee(fee types.Currency) uint64

	// AddSiacoinInput adds a siacoin input to the transaction, returning the
	// index of the siacoin input within the transaction. When 'Sign' gets
	// called, this input will be left unsigned.
	AddSiacoinInput(types.SiacoinInput) uint64

	// AddSiacoinOutput adds a siacoin output to the transaction, returning the
	// index of the siacoin output within the transaction.
	AddSiacoinOutput(types.SiacoinOutput) uint64

	// AddFileContract adds a file contract to the transaction, returning the
	// index of the file contract within the transaction.
	AddFileContract(types.FileContract) uint64

	// AddFileContractRevision adds a file contract revision to the
	// transaction, returning the index of the file contract revision within
	// the transaction. When 'Sign' gets called, this revision will be left
	// unsigned.
	AddFileContractRevision(types.FileContractRevision) uint64

	// AddStorageProof adds a storage proof to the transaction, returning the
	// index of the storage proof within the transaction.
	AddStorageProof(types.StorageProof) uint64

	// AddSiafundInput adds a siafund input to the transaction, returning the
	// index of the siafund input within the transaction. When 'Sign' is
	// called, this input will be left unsigned.
	AddSiafundInput(types.SiafundInput) uint64

	// AddSiafundOutput adds a siafund output to the transaction, returning the
	// index of the siafund output within the transaction.
	AddSiafundOutput(types.SiafundOutput) uint64

	// AddArbitraryData adds arbitrary data to the transaction, returning the
	// index of the data within the transaction.
	AddArbitraryData(arb []byte) uint64

	// AddTransactionSignature adds a transaction signature to the transaction,
	// returning the index of the signature within the transaction. The
	// signature should already be valid, and shouldn't sign any of the inputs
	// that were added by calling 'FundSiacoins' or 'FundSiafunds'.
	AddTransactionSignature(types.TransactionSignature) uint64

	// Sign will sign any inputs added by 'FundSiacoins' or 'FundSiafunds' and
	// return a transaction set that contains all parents prepended to the
	// transaction. If more fields need to be added, a new transaction builder
	// will need to be created.
	//
	// If the whole transaction flag  is set to true, then the whole
	// transaction flag will be set in the covered fields object. If the whole
	// transaction flag is set to false, then the covered fields object will
	// cover all fields that have already been added to the transaction, but
	// will also leave room for more fields to be added.
	Sign(masterKey crypto.TwofishKey, wholeTransaction bool) ([]types.Transaction, error)

	// View returns the incomplete transaction along with all of its parents.
	View() (txn types.Transaction, parents []types.Transaction)
}

// Wallet stores and manages siacoins and siafunds. The wallet file is
// encrypted using a user-specified password. Common addresses are all dervied
// from a single address seed.
type Wallet interface {
	// Encrypted returns whether or not the wallet has been encrypted yet. User
	// facings apps are recommended to check if the wallet is encrypted before
	// calling Unlock, because the key used in the first call to 'Unlock' will
	// be the key that encrypts the wallet going forward. User facing apps
	// should verify that the correct password/phrase/key was chosen before
	// permanently encrypting the wallet.
	Encrypted() bool

	// Unlock must be called before the wallet is usable. All wallets and
	// wallet seeds are encrypted by default, and the wallet will not know
	// which addresses to watch for on the blockchain until unlock has been
	// called.
	//
	// All items in the wallet are encrypted using different keys which are
	// derived from the master key.
	Unlock(masterKey crypto.TwofishKey) error

	// NewPrimarySeed will generate a new primary seed from which addresses
	// will be derived. Each seed can produce up to 'PublicKeysPerSeed' seeds,
	// after which an error will be returned when requesting new addresses. The
	// string returned is the recovery string for the seed. If the wallet file
	// is lost, the recovery string may be used to regain the files.
	NewPrimarySeed(masterKey crypto.TwofishKey) (string, error)

	// PrimarySeed returns the current primary seed of the wallet, unencrypted,
	// with an int indicating how many addresses have been consumed out of
	// 'PublicKeysPerSeed' total addresses.
	PrimarySeed(masterKey crypto.Twofish) (string, error)

	// AllSeeds returns all of the seeds that are being tracked by the wallet,
	// including the primary seed. Only the primary seed is used to generate
	// new addresses, but the wallet can spend funds sent to public keys
	// generated by any of the seeds returned.
	AllSeeds(masterKey crypto.Twofish) ([]string, error)

	// RegisterTransaction takes a transaction and its parents and returns a
	// TransactionBuilder which can be used to expand the transaction. The most
	// typical call is 'RegisterTransaction(types.Transaction{}, nil)', which
	// registers a new transaction without parents.
	RegisterTransaction(t types.Transaction, parents []types.Transaction) TransactionBuilder

	// StartTransaction is a convenience method that calls
	// RegisterTransaction(types.Transaction{}, nil)
	StartTransaction() TransactionBuilder

	// ConfirmedBalance returns the confirmed balance of the wallet, minus any
	// outgoing transactions. ConfirmedBalance will include unconfirmed refund
	// transacitons.
	ConfirmedBalance() types.Currency

	// UnconfirmedBalance returns the unconfirmed balance of the wallet.
	// Outgoing funds and incoming funds are reported separately. Refund
	// outputs are not included in 'incoming', and are subracted from
	// 'outgoing'. (For example, if a transaction is created with an output of
	// 10 coins, with a 9 coin refund, UnconfirmedBalance would report
	// 'outgoing: 1, incoming: 0'.)
	UnconfirmedBalance() (outgoing types.Currency, incoming types.Currency)

	// TransacitonHistory will return a chronologically ordered set of
	// 'WalletTransactions' that make up the history of the wallet.
	TransactionHistory() []WalletTransaction

	// PartialTransactionHistory returns all of the transactions that were
	// confirmed at heights [startingBlock, endingBlock].
	PartialTransactionHistory(startingBlock types.BlockHeight, endingBlock types.BlockHeight) ([]WalletTransaction, error)

	// AddressTransactionHistory returns all of the transactions that are
	// related to a given address.
	AddressTransactionHistory(types.UnlockHash) []WalletTransaction

	// CoinAddress returns an address that can receive coins.
	CoinAddress() (types.UnlockHash, types.UnlockConditions, error)

	// SendCoins is a tool for sending coins from the wallet to an address.
	// Sending money usually results in multiple transactions. The transactions
	// are automatically given to the transaction pool, and are also returned
	// to the caller.
	SendCoins(masterKey crypto.TwofishKey, amount types.Currency, dest types.UnlockHash) ([]types.Transaction, error)

	// SiafundBalance returns the number of siafunds owned by the wallet, and
	// the number of siacoins available through siafund claims.
	SiafundBalance() (siafundBalance types.Currency, siacoinClaimBalance types.Currency)

	// MergeWallet takes a filepath to another wallet that should be merged
	// with the current wallet. Repeat addresses will not be merged.
	MergeWallet(string) error

	// SendSiagSiafunds sends siafunds to another address. The siacoins stored
	// in the siafunds are sent to an address in the wallet.
	SendSiagSiafunds(amount types.Currency, dest types.UnlockHash, keyfiles []string) ([]types.Transaction, error)

	// WatchSiagSiafundAddress adds a siafund address pulled from a siag keyfile.
	WatchSiagSiafundAddress(keyfile string) error

	// Close prepares the wallet for shutdown.
	Close() error
}
