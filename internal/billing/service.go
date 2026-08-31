package billing

import (
	"context"
	"database/sql"
	"fmt"
	"reni-social/pkg/db"
)

// GetWalletBalance returns the current credit balance for a user.
func GetWalletBalance(ctx context.Context, userID string) (int, error) {
	var balance int
	err := db.DB.QueryRowContext(ctx, "SELECT balance FROM wallets WHERE user_id = $1", userID).Scan(&balance)
	if err == sql.ErrNoRows {
		return 0, nil // Wallet doesn't exist yet
	}
	return balance, err
}

// DeductCredits safely deducts credits. Returns error if insufficient funds.
func DeductCredits(ctx context.Context, userID string, amount int, txType, reference string) error {
	tx, err := db.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var balance int
	// FOR UPDATE locks the row so concurrent worker goroutines don't cause a race condition
	err = tx.QueryRowContext(ctx, "SELECT balance FROM wallets WHERE user_id = $1 FOR UPDATE", userID).Scan(&balance)
	if err != nil {
		return fmt.Errorf("wallet not found")
	}

	if balance < amount {
		return fmt.Errorf("insufficient credits")
	}

	// Deduct balance
	_, err = tx.ExecContext(ctx, "UPDATE wallets SET balance = balance - $1, updated_at = NOW() WHERE user_id = $2", amount, userID)
	if err != nil {
		return err
	}

	// Log transaction
	_, err = tx.ExecContext(ctx, `
		INSERT INTO transactions (id, user_id, amount, type, reference, status, created_at)
		VALUES (gen_random_uuid(), $1, $2, $3, $4, 'completed', NOW())
	`, userID, -amount, txType, reference)
	
	return tx.Commit()
}

// AddCredits is called by your Payment Gateway Webhook when a user buys credits.
func AddCredits(ctx context.Context, userID string, amount int, txType, reference string) error {
	tx, err := db.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Upsert wallet (create if doesn't exist)
	_, err = tx.ExecContext(ctx, `
		INSERT INTO wallets (id, user_id, balance, created_at, updated_at)
		VALUES (gen_random_uuid(), $1, $2, NOW(), NOW())
		ON CONFLICT (user_id) DO UPDATE SET balance = wallets.balance + $2, updated_at = NOW()
	`, userID, amount)
	if err != nil {
		return err
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO transactions (id, user_id, amount, type, reference, status, created_at)
		VALUES (gen_random_uuid(), $1, $2, $3, $4, 'completed', NOW())
	`, userID, amount, txType, reference)

	return tx.Commit()
}