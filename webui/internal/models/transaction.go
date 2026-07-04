package models

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Transaction struct {
	ID            int       `json:"id"`
	UserID        int       `json:"user_id"`
	Username      string    `json:"username,omitempty"` // populated on joins
	Package       string    `json:"package"`
	Amount        int64     `json:"amount"`
	DurationType  string    `json:"duration_type"` // bulanan, semester, tahunan
	Status        string    `json:"status"`        // pending, approved, rejected
	PaymentMethod string    `json:"payment_method"`
	ProofPath     string    `json:"proof_path"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
	Notes         string    `json:"notes"`
}

const (
	TxStatusPending  = "pending"
	TxStatusApproved = "approved"
	TxStatusRejected = "rejected"
)

// CreateTransaction inserts a new pending transaction.
func CreateTransaction(ctx context.Context, pool *pgxpool.Pool, tx *Transaction) (*Transaction, error) {
	sql := `INSERT INTO transactions 
	(user_id, package, amount, duration_type, status, payment_method, proof_path, notes)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	RETURNING id, created_at, updated_at`

	err := pool.QueryRow(ctx, sql,
		tx.UserID, tx.Package, tx.Amount, tx.DurationType, TxStatusPending,
		tx.PaymentMethod, tx.ProofPath, tx.Notes,
	).Scan(&tx.ID, &tx.CreatedAt, &tx.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("create transaction: %w", err)
	}

	return tx, nil
}

// CreateTransactionTx inserts a new pending transaction within an existing transaction.
func CreateTransactionTx(ctx context.Context, dbTx pgx.Tx, tx *Transaction) (*Transaction, error) {
	sql := `INSERT INTO transactions
		(user_id, package, amount, duration_type, status, payment_method, proof_path, notes)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, created_at, updated_at`

	err := dbTx.QueryRow(ctx, sql,
		tx.UserID, tx.Package, tx.Amount, tx.DurationType, TxStatusPending,
		tx.PaymentMethod, tx.ProofPath, tx.Notes,
	).Scan(&tx.ID, &tx.CreatedAt, &tx.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("create transaction in tx: %w", err)
	}

	return tx, nil
}

// GetTransactionByID retrieves a single transaction.
func GetTransactionByID(ctx context.Context, pool *pgxpool.Pool, id int) (*Transaction, error) {
	var tx Transaction
	sql := `SELECT id, user_id, package, amount, duration_type, status, payment_method, proof_path, created_at, updated_at, notes
	FROM transactions WHERE id = $1`

	err := pool.QueryRow(ctx, sql, id).Scan(
		&tx.ID, &tx.UserID, &tx.Package, &tx.Amount, &tx.DurationType, &tx.Status,
		&tx.PaymentMethod, &tx.ProofPath, &tx.CreatedAt, &tx.UpdatedAt, &tx.Notes,
	)
	if err != nil {
		return nil, fmt.Errorf("get transaction by id: %w", err)
	}

	return &tx, nil
}

// UpdateTransactionStatus updates transaction status and runs on a transaction context.
func UpdateTransactionStatus(ctx context.Context, pool *pgxpool.Pool, id int, status, notes string) error {
	sql := `UPDATE transactions SET status = $1, notes = $2, updated_at = CURRENT_TIMESTAMP WHERE id = $3`
	_, err := pool.Exec(ctx, sql, status, notes, id)
	if err != nil {
		return fmt.Errorf("update transaction status: %w", err)
	}
	return nil
}

// ListTransactionsOpts defines filter options for listing transactions.
type ListTransactionsOpts struct {
	Page    int
	PerPage int
	UserID  int    // 0 means all users
	Status  string // empty means all statuses
}

type ListTransactionsResult struct {
	Transactions []Transaction
	Total        int
	TotalPages   int
	Page         int
	PerPage      int
}

// ListTransactions retrieves paginated transactions.
func ListTransactions(ctx context.Context, pool *pgxpool.Pool, opts ListTransactionsOpts) (*ListTransactionsResult, error) {
	if opts.Page < 1 {
		opts.Page = 1
	}
	if opts.PerPage < 1 {
		opts.PerPage = 10
	}

	whereClause := "WHERE 1=1"
	args := []interface{}{}
	argIdx := 1

	if opts.UserID > 0 {
		whereClause += fmt.Sprintf(" AND t.user_id = $%d", argIdx)
		args = append(args, opts.UserID)
		argIdx++
	}

	if opts.Status != "" {
		whereClause += fmt.Sprintf(" AND t.status = $%d", argIdx)
		args = append(args, opts.Status)
		argIdx++
	}

	// Count query
	countSQL := fmt.Sprintf(`SELECT COUNT(*) FROM transactions t %s`, whereClause)
	var total int
	err := pool.QueryRow(ctx, countSQL, args...).Scan(&total)
	if err != nil {
		return nil, fmt.Errorf("count transactions: %w", err)
	}

	totalPages := (total + opts.PerPage - 1) / opts.PerPage
	if totalPages < 1 {
		totalPages = 1
	}

	// Fetch query
	offset := (opts.Page - 1) * opts.PerPage
	fetchSQL := fmt.Sprintf(`
		SELECT t.id, t.user_id, u.username, t.package, t.amount, t.duration_type, t.status, 
		       t.payment_method, t.proof_path, t.created_at, t.updated_at, t.notes
		FROM transactions t
		JOIN admin_users u ON t.user_id = u.id
		%s
		ORDER BY t.created_at DESC
		LIMIT %d OFFSET %d
	`, whereClause, opts.PerPage, offset)

	rows, err := pool.Query(ctx, fetchSQL, args...)
	if err != nil {
		return nil, fmt.Errorf("list transactions query: %w", err)
	}
	defer rows.Close()

	txs := []Transaction{}
	for rows.Next() {
		var tx Transaction
		err := rows.Scan(
			&tx.ID, &tx.UserID, &tx.Username, &tx.Package, &tx.Amount, &tx.DurationType, &tx.Status,
			&tx.PaymentMethod, &tx.ProofPath, &tx.CreatedAt, &tx.UpdatedAt, &tx.Notes,
		)
		if err != nil {
			return nil, fmt.Errorf("scan transaction: %w", err)
		}
		txs = append(txs, tx)
	}

	return &ListTransactionsResult{
		Transactions: txs,
		Total:        total,
		TotalPages:   totalPages,
		Page:         opts.Page,
		PerPage:      opts.PerPage,
	}, nil
}
