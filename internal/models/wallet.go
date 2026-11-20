package models

import (
	"errors"
	"uuid"
)

var (
	ErrWalletNotFound    = errors.New("wallet not found")
	ErrInsufficientFunds = errors.New("insufficient funds")
	ErrInvalidOperation  = errors.New("incorrect operation format")
)

type Wallet struct {
	UUID    uuid.UUID `json:"walletid"`
	Balance int       `json:"balance"`
}

type Operation struct {
	UUID          uuid.UUID `json:"walletid"`
	OperationType string    `json:"operationType"`
	Amount        int       `json:"amount"`
}
