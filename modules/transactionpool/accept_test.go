package transactionpool

import (
	"crypto/rand"
	"testing"

	"github.com/NebulousLabs/Sia/modules"
	"github.com/NebulousLabs/Sia/types"
)

// mockGatewayCheckBroadcast is a mock implementation of modules.Gateway that
// enables testing of selective broadcasting by mocking the Peers and Broadcast
// methods.
type mockGatewayCheckBroadcast struct {
	modules.Gateway
	peers            []modules.Peer
	broadcastedPeers chan []modules.Peer
}

// Peers is a mock implementation of Gateway.Peers that returns the mocked
// peers.
func (g *mockGatewayCheckBroadcast) Peers() []modules.Peer {
	return g.peers
}

// Broadcast is a mock implementation of Gateway.Broadcast that writes the
// peers it receives as an argument to the broadcastedPeers channel.
func (g *mockGatewayCheckBroadcast) Broadcast(_ string, _ interface{}, peers []modules.Peer) {
	g.broadcastedPeers <- peers
}

// TestAcceptTransactionSetBroadcasts tests that AcceptTransactionSet only
// broadcasts to peers v0.4.7 and above.
func TestAcceptTransactionSetBroadcasts(t *testing.T) {
	tpt, err := createTpoolTester("TestAcceptTransactionSetBroadcasts")
	if err != nil {
		t.Fatal(err)
	}
	mockPeers := []modules.Peer{
		modules.Peer{Version: "0.0.0"},
		modules.Peer{Version: "0.4.6"},
		modules.Peer{Version: "0.4.7"},
		modules.Peer{Version: "9.9.9"},
	}
	mg := &mockGatewayCheckBroadcast{
		Gateway:          tpt.tpool.gateway,
		peers:            mockPeers,
		broadcastedPeers: make(chan []modules.Peer),
	}
	tpt.tpool.gateway = mg

	go func() {
		err = tpt.tpool.AcceptTransactionSet([]types.Transaction{{}})
		if err != nil {
			t.Fatal(err)
		}
	}()
	broadcastedPeers := <-mg.broadcastedPeers
	if len(broadcastedPeers) != 2 {
		t.Fatalf("only 2 peers have version >= v0.4.7, but AcceptTransactionSet relayed the transaction set to %v peers", len(broadcastedPeers))
	}
	for _, bp := range broadcastedPeers {
		if bp.Version != "0.4.7" && bp.Version != "9.9.9" {
			t.Fatalf("AcceptTransactionSet relayed the transaction to a peer with version < v0.4.7 (%v)", bp.Version)
		}
	}
}

// TestIntegrationAcceptTransactionSet probes the AcceptTransactionSet method
// of the transaction pool.
func TestIntegrationAcceptTransactionSet(t *testing.T) {
	// Create a transaction pool tester.
	tpt, err := createTpoolTester("TestIntegrationAcceptTransactionSet")
	if err != nil {
		t.Fatal(err)
	}

	// Check that the transaction pool is empty.
	if len(tpt.tpool.transactionSets) != 0 {
		t.Error("transaction pool is not empty")
	}

	// Create a valid transaction set using the wallet.
	txns, err := tpt.wallet.SendSiacoins(types.NewCurrency64(100), types.UnlockHash{})
	if err != nil {
		t.Fatal(err)
	}
	if len(tpt.tpool.transactionSets) != 1 {
		t.Error("sending coins did not increase the transaction sets by 1")
	}

	// Submit the transaction set again to trigger a duplication error.
	err = tpt.tpool.AcceptTransactionSet(txns)
	if err != modules.ErrDuplicateTransactionSet {
		t.Error(err)
	}

	// Mine a block and check that the transaction pool gets emptied.
	block, _ := tpt.miner.FindBlock()
	err = tpt.cs.AcceptBlock(block)
	if err != nil {
		t.Fatal(err)
	}
	if len(tpt.tpool.TransactionList()) != 0 {
		t.Error("transaction pool was not emptied after mining a block")
	}

	// Try to resubmit the transaction set to verify
	err = tpt.tpool.AcceptTransactionSet(txns)
	if err == nil {
		t.Error("transaction set was supposed to be rejected")
	}
}

// TestIntegrationConflictingTransactionSets tries to add two transaction sets
// to the transaction pool that are each legal individually, but double spend
// an output.
func TestIntegrationConflictingTransactionSets(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	// Create a transaction pool tester.
	tpt, err := createTpoolTester("TestIntegrationConflictingTransactionSets")
	if err != nil {
		t.Fatal(err)
	}

	// Fund a partial transaction.
	fund := types.NewCurrency64(30e6)
	txnBuilder := tpt.wallet.StartTransaction()
	err = txnBuilder.FundSiacoins(fund)
	if err != nil {
		t.Fatal(err)
	}
	// wholeTransaction is set to false so that we can use the same signature
	// to create a double spend.
	txnSet, err := txnBuilder.Sign(false)
	if err != nil {
		t.Fatal(err)
	}
	txnSetDoubleSpend := make([]types.Transaction, len(txnSet))
	copy(txnSetDoubleSpend, txnSet)

	// There are now two sets of transactions that are signed and ready to
	// spend the same output. Have one spend the money in a miner fee, and the
	// other create a siacoin output.
	txnIndex := len(txnSet) - 1
	txnSet[txnIndex].MinerFees = append(txnSet[txnIndex].MinerFees, fund)
	txnSetDoubleSpend[txnIndex].SiacoinOutputs = append(txnSetDoubleSpend[txnIndex].SiacoinOutputs, types.SiacoinOutput{Value: fund})

	// Add the first and then the second txn set.
	err = tpt.tpool.AcceptTransactionSet(txnSet)
	if err != nil {
		t.Error(err)
	}
	err = tpt.tpool.AcceptTransactionSet(txnSetDoubleSpend)
	if err == nil {
		t.Error("transaction should not have passed inspection")
	}

	// Purge and try the sets in the reverse order.
	tpt.tpool.PurgeTransactionPool()
	err = tpt.tpool.AcceptTransactionSet(txnSetDoubleSpend)
	if err != nil {
		t.Error(err)
	}
	err = tpt.tpool.AcceptTransactionSet(txnSet)
	if err == nil {
		t.Error("transaction should not have passed inspection")
	}
}

// TestIntegrationCheckMinerFees probes the checkMinerFees method of the
// transaction pool.
func TestIntegrationCheckMinerFees(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	// Create a transaction pool tester.
	tpt, err := createTpoolTester("TestIntegrationCheckMinerFees")
	if err != nil {
		t.Fatal(err)
	}

	// Fill the transaction pool to the fee limit.
	for i := 0; i < TransactionPoolSizeForFee/10e3; i++ {
		arbData := make([]byte, 10e3)
		copy(arbData, modules.PrefixNonSia[:])
		_, err = rand.Read(arbData[100:116]) // prevents collisions with other transacitons in the loop.
		if err != nil {
			t.Fatal(err)
		}
		txn := types.Transaction{ArbitraryData: [][]byte{arbData}}
		err := tpt.tpool.AcceptTransactionSet([]types.Transaction{txn})
		if err != nil {
			t.Fatal(err)
		}
	}

	// Add another transaction, this one should fail for having too few fees.
	err = tpt.tpool.AcceptTransactionSet([]types.Transaction{{}})
	if err != errLowMinerFees {
		t.Error(err)
	}

	// Add a transaction that has sufficient fees.
	_, err = tpt.wallet.SendSiacoins(types.NewCurrency64(100), types.UnlockHash{})
	if err != nil {
		t.Error(err)
	}

	// TODO: fill the pool up all the way and try again.
}

// TestTransactionSuperset submits a single transaction to the network,
// followed by a transaction set containing that single transaction.
func TestIntegrationTransactionSuperset(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	// Create a transaction pool tester.
	tpt, err := createTpoolTester("TestTransactionSuperset")
	if err != nil {
		t.Fatal(err)
	}

	// Fund a partial transaction.
	fund := types.NewCurrency64(30e6)
	txnBuilder := tpt.wallet.StartTransaction()
	err = txnBuilder.FundSiacoins(fund)
	if err != nil {
		t.Fatal(err)
	}
	txnBuilder.AddMinerFee(fund)
	// wholeTransaction is set to false so that we can use the same signature
	// to create a double spend.
	txnSet, err := txnBuilder.Sign(false)
	if err != nil {
		t.Fatal(err)
	}
	if len(txnSet) <= 1 {
		t.Fatal("test is invalid unless the transaction set has two or more transactions")
	}
	// Check that the second transaction is dependent on the first.
	err = tpt.tpool.AcceptTransactionSet(txnSet[1:])
	if err == nil {
		t.Fatal("transaction set must have dependent transactions")
	}

	// Submit the first transaction in the set to the transaction pool, and
	// then the superset.
	err = tpt.tpool.AcceptTransactionSet(txnSet[:1])
	if err != nil {
		t.Fatal("first transaction in the transaction set was not valid?")
	}
	err = tpt.tpool.AcceptTransactionSet(txnSet)
	if err != nil {
		t.Fatal("super setting is not working:", err)
	}

	// Try resubmitting the individual transaction and the superset, a
	// duplication error should be returned for each case.
	err = tpt.tpool.AcceptTransactionSet(txnSet[:1])
	if err != modules.ErrDuplicateTransactionSet {
		t.Fatal(err)
	}
	err = tpt.tpool.AcceptTransactionSet(txnSet)
	if err != modules.ErrDuplicateTransactionSet {
		t.Fatal("super setting is not working:", err)
	}
}

// TestTransactionSubset submits a transaction set to the network, followed by
// just a subset, expectint ErrDuplicateTransactionSet as a response.
func TestIntegrationTransactionSubset(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	// Create a transaction pool tester.
	tpt, err := createTpoolTester("TestTransactionSubset")
	if err != nil {
		t.Fatal(err)
	}

	// Fund a partial transaction.
	fund := types.NewCurrency64(30e6)
	txnBuilder := tpt.wallet.StartTransaction()
	err = txnBuilder.FundSiacoins(fund)
	if err != nil {
		t.Fatal(err)
	}
	txnBuilder.AddMinerFee(fund)
	// wholeTransaction is set to false so that we can use the same signature
	// to create a double spend.
	txnSet, err := txnBuilder.Sign(false)
	if err != nil {
		t.Fatal(err)
	}
	if len(txnSet) <= 1 {
		t.Fatal("test is invalid unless the transaction set has two or more transactions")
	}
	// Check that the second transaction is dependent on the first.
	err = tpt.tpool.AcceptTransactionSet(txnSet[1:])
	if err == nil {
		t.Fatal("transaction set must have dependent transactions")
	}

	// Submit the set to the pool, followed by just the transaction.
	err = tpt.tpool.AcceptTransactionSet(txnSet)
	if err != nil {
		t.Fatal("super setting is not working:", err)
	}
	err = tpt.tpool.AcceptTransactionSet(txnSet[:1])
	if err != modules.ErrDuplicateTransactionSet {
		t.Fatal(err)
	}
}

// TestIntegrationTransactionChild submits a single transaction to the network,
// followed by a child transaction.
func TestIntegrationTransactionChild(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	// Create a transaction pool tester.
	tpt, err := createTpoolTester("TestTransactionChild")
	if err != nil {
		t.Fatal(err)
	}

	// Fund a partial transaction.
	fund := types.NewCurrency64(30e6)
	txnBuilder := tpt.wallet.StartTransaction()
	err = txnBuilder.FundSiacoins(fund)
	if err != nil {
		t.Fatal(err)
	}
	txnBuilder.AddMinerFee(fund)
	// wholeTransaction is set to false so that we can use the same signature
	// to create a double spend.
	txnSet, err := txnBuilder.Sign(false)
	if err != nil {
		t.Fatal(err)
	}
	if len(txnSet) <= 1 {
		t.Fatal("test is invalid unless the transaction set has two or more transactions")
	}
	// Check that the second transaction is dependent on the first.
	err = tpt.tpool.AcceptTransactionSet([]types.Transaction{txnSet[1]})
	if err == nil {
		t.Fatal("transaction set must have dependent transactions")
	}

	// Submit the first transaction in the set to the transaction pool.
	err = tpt.tpool.AcceptTransactionSet(txnSet[:1])
	if err != nil {
		t.Fatal("first transaction in the transaction set was not valid?")
	}
	err = tpt.tpool.AcceptTransactionSet(txnSet[1:])
	if err != nil {
		t.Fatal("child transaction not seen as valid")
	}
}

// TestIntegrationNilAccept tries submitting a nil transaction set and a 0-len
// transaction set to the transaction pool.
func TestIntegrationNilAccept(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	tpt, err := createTpoolTester("TestTransactionChild")
	if err != nil {
		t.Fatal(err)
	}

	err = tpt.tpool.AcceptTransactionSet(nil)
	if err == nil {
		t.Error("no error returned when submitting nothing to the transaction pool")
	}
	err = tpt.tpool.AcceptTransactionSet([]types.Transaction{})
	if err == nil {
		t.Error("no error returned when submitting nothing to the transaction pool")
	}
}

// TestAcceptFCAndConflictingRevision checks that the transaction pool
// correctly accepts a file contract in a transaction set followed by a correct
// revision to that file contract in the a following transaction set, with no
// block separating them.
func TestAcceptFCAndConflictingRevision(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	tpt, err := createTpoolTester("TestAcceptFCAndConflictingRevision")
	if err != nil {
		t.Fatal(err)
	}

	// Create and fund a valid file contract.
	builder := tpt.wallet.StartTransaction()
	payout := types.NewCurrency64(1e9)
	err = builder.FundSiacoins(payout)
	if err != nil {
		t.Fatal(err)
	}
	builder.AddFileContract(types.FileContract{
		WindowStart:        tpt.cs.Height() + 2,
		WindowEnd:          tpt.cs.Height() + 5,
		Payout:             payout,
		ValidProofOutputs:  []types.SiacoinOutput{{Value: types.PostTax(tpt.cs.Height(), payout)}},
		MissedProofOutputs: []types.SiacoinOutput{{Value: types.PostTax(tpt.cs.Height(), payout)}},
		UnlockHash:         types.UnlockConditions{}.UnlockHash(),
	})
	tSet, err := builder.Sign(true)
	if err != nil {
		t.Fatal(err)
	}
	err = tpt.tpool.AcceptTransactionSet(tSet)
	if err != nil {
		t.Fatal(err)
	}
	fcid := tSet[len(tSet)-1].FileContractID(0)

	// Create a file contract revision and submit it.
	rSet := []types.Transaction{{
		FileContractRevisions: []types.FileContractRevision{{
			ParentID:          fcid,
			NewRevisionNumber: 2,

			NewWindowStart:        tpt.cs.Height() + 2,
			NewWindowEnd:          tpt.cs.Height() + 5,
			NewValidProofOutputs:  []types.SiacoinOutput{{Value: types.PostTax(tpt.cs.Height(), payout)}},
			NewMissedProofOutputs: []types.SiacoinOutput{{Value: types.PostTax(tpt.cs.Height(), payout)}},
		}},
	}}
	err = tpt.tpool.AcceptTransactionSet(rSet)
	if err != nil {
		t.Fatal(err)
	}
}
