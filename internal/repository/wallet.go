package repository

import (
	"context"
	"uuid"

	"github.com/jackc/pgx/v5/pgxpool"
	"main.go/internal/models"
)

const (
	Deposit  = "DEPOSIT"
	Withdraw = "WITHDRAW"
)

type PostgresWalletRepository struct {
	Pool *pgxpool.Pool
}

func (r *PostgresWalletRepository) GetByID(ctx context.Context, uuid uuid.UUID) (*models.Wallet, error) {
	result := &models.Wallet{}
	const query = `SELECT walletid, balance
		FROM wallets
		WHERE walletid = $1
		`

	err := r.Pool.QueryRow(ctx, query, uuid).Scan(
		&result.UUID,
		&result.Balance,
	)
	if err != nil {
		return nil, err
	}

	return result, nil
}

func (r *PostgresWalletRepository) ChangeBalance(ctx context.Context, op models.Operation) error {
	if op.OperationType != Deposit && op.OperationType != Withdraw {
		return models.ErrInvalidOperation
	}

	if op.UUID == uuid.Nil() {
		return models.ErrWalletNotFound
	}

	if op.Amount <= 0 {
		return models.ErrInvalidOperation
	}

	var success, walletFound bool
	var err error
	switch op.OperationType {
	case Deposit:
		err = r.Pool.QueryRow(
			ctx,
			`WITH updated AS (
				UPDATE wallets
				SET balance = balance + $1
				WHERE walletid = $2
				RETURNING walletid
			)
			SELECT
				EXISTS (
					SELECT 1 FROM updated
				) AS success,
				EXISTS (
					SELECT 1 FROM wallets
					WHERE walletid = $2
				) AS wallet_exists;`,
			op.Amount,
			op.UUID).Scan(&success, &walletFound)
	case Withdraw:
		err = r.Pool.QueryRow(
			ctx,
			`WITH updated AS (
				UPDATE wallets
				SET balance = balance - $1
				WHERE walletid = $2
				AND balance >= $1
				RETURNING walletid
			)
			SELECT
				EXISTS (
					SELECT 1 FROM updated
				) AS success,
				EXISTS (
					SELECT 1 FROM wallets
					WHERE walletid = $2
				) AS wallet_exists;`,
			op.Amount,
			op.UUID).Scan(&success, &walletFound)
		if err != nil {
			return err
		}
	}

	if !success {
		if !walletFound {
			return models.ErrWalletNotFound
		}
		return models.ErrInsufficientFunds
	}

	return nil
}
