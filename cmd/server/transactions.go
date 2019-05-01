// Copyright 2019 The Moov Authors
// Use of this source code is governed by an Apache License
// license that can be found in the LICENSE file.

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/moov-io/base"
	moovhttp "github.com/moov-io/base/http"
	"github.com/moov-io/gl"

	"github.com/go-kit/kit/log"
	"github.com/gorilla/mux"
)

var (
	errNoAccountId = errors.New("no accountId found")
)

type TransactionPurpose string

var (
	ACHCredit TransactionPurpose = "achcredit"
	ACHDebit  TransactionPurpose = "achdebit"
	Fee       TransactionPurpose = "fee"
	Interest  TransactionPurpose = "interest"
	Transfer  TransactionPurpose = "transfer"
	Wire      TransactionPurpose = "wire"
)

func (p TransactionPurpose) validate() error {
	switch p {
	case ACHCredit, ACHDebit, Fee, Interest, Transfer, Wire:
		return nil
	default:
		return fmt.Errorf("unknown TransactionPurpose %q", p)
	}
}

type transactionLine struct {
	AccountId string             `json:"accountId"`
	Purpose   TransactionPurpose `json:"purpose"`
	Amount    int                `json:"amount"`
}

type createTransactionRequest struct {
	Lines []transactionLine `json:"lines"`
}

func (r *createTransactionRequest) asTransaction(id string) transaction {
	return transaction{
		ID:        id,
		Lines:     r.Lines,
		Timestamp: time.Now(),
	}
}

type transaction struct {
	ID        string            `json:"id"`
	Timestamp time.Time         `json:"timestamp"`
	Lines     []transactionLine `json:"lines"`
}

func (t transaction) validate() error {
	sum := 0
	for i := range t.Lines {
		sum += t.Lines[i].Amount
	}
	if sum == 0 {
		return nil
	}
	return fmt.Errorf("transaction=%s has %d invalid lines sum=%d", t.ID, len(t.Lines), sum)
}

func addTransactionRoutes(logger log.Logger, router *mux.Router, accountRepo accountRepository, transactionRepo transactionRepository) {
	router.Methods("GET").Path("/accounts/{accountId}/transactions").HandlerFunc(getAccountTransactions(logger, transactionRepo))
	router.Methods("POST").Path("/accounts/{accountId}/transactions").HandlerFunc(createTransaction(logger, accountRepo, transactionRepo))
}

func getAccountId(w http.ResponseWriter, r *http.Request) string {
	v, ok := mux.Vars(r)["accountId"]
	if !ok || v == "" {
		moovhttp.Problem(w, errNoAccountId)
		return ""
	}
	return v
}

func getAccountTransactions(logger log.Logger, transactionRepo transactionRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w, err := wrapResponseWriter(logger, w, r)
		if err != nil {
			return
		}

		accountId := getAccountId(w, r)
		if accountId == "" {
			moovhttp.Problem(w, errNoAccountId)
			return
		}

		transactions, err := transactionRepo.getAccountTransactions(accountId)
		if err != nil {
			moovhttp.Problem(w, err)
			return
		}

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(transactions)

	}
}

func createTransaction(logger log.Logger, accountRepo accountRepository, transactionRepo transactionRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w, err := wrapResponseWriter(logger, w, r)
		if err != nil {
			return
		}

		var req createTransactionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			moovhttp.Problem(w, err)
			return
		}

		tx := req.asTransaction(base.ID())

		// Naive balance check on transactions.
		// TODO(adam): This is a bad compare, we need to attempt a commit with balance checks
		accounts, err := accountRepo.GetAccounts(grabAccountIds(tx.Lines))
		if err != nil {
			moovhttp.Problem(w, err)
			return
		}
		if err := checkBalance(accounts, tx); err != nil {
			moovhttp.Problem(w, err)
			return
		}

		// Post the transaction
		if err := transactionRepo.createTransaction(tx); err != nil {
			moovhttp.Problem(w, err)
			return
		}

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(tx)
	}
}

func checkBalance(accounts []*gl.Account, tx transaction) error {
	if len(accounts) < 2 || len(tx.Lines) == 0 {
		return fmt.Errorf("checkBalance: invalid input len(accounts)=%d len(tx.Lines)=%d", len(accounts), len(tx.Lines))
	}
	for i := range accounts {
		if accounts[i].Balance > 0 {
			for j := range tx.Lines {
				if accounts[i].ID == tx.Lines[j].AccountId {
					if accounts[i].Balance < int64(tx.Lines[j].Amount) {
						return fmt.Errorf("checkBalance: account %s has insufficient funds", accounts[i].ID)
					} else {
						goto sufficient
					}
				}
			}
			// no match, logic bug or database bug
			return fmt.Errorf("checkBalance: no transactionLines found for account %s", accounts[i].ID)
		}
	sufficient: // Account had sufficient funds
	}
	return nil
}
