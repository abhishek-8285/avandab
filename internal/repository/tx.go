package repository

import (
	"context"
	"database/sql"
)

type txKey struct{}

// WithTxInContext stores a *sql.Tx in context for repository methods to pick up.
func WithTxInContext(ctx context.Context, tx *sql.Tx) context.Context {
	return context.WithValue(ctx, txKey{}, tx)
}

// TxFromContext retrieves the *sql.Tx from context, or nil if none.
func TxFromContext(ctx context.Context) *sql.Tx {
	tx, _ := ctx.Value(txKey{}).(*sql.Tx)
	return tx
}

// DBGetter provides access to the underlying *sql.DB.
type DBGetter interface {
	DB() *sql.DB
}

// ExecTx runs query on the ambient transaction when present, else db. Raw
// db.ExecContext from inside a UoW transaction grabs a second pool
// connection and deadlocks SQLite's single-writer lock (modernc retries
// forever) — UoW-scoped writes must always route through here.
func ExecTx(ctx context.Context, db *sql.DB, query string, args ...any) (sql.Result, error) {
	if tx := TxFromContext(ctx); tx != nil {
		return tx.ExecContext(ctx, query, args...)
	}
	return db.ExecContext(ctx, query, args...)
}

// QueryRowTx is ExecTx for single-row reads.
func QueryRowTx(ctx context.Context, db *sql.DB, query string, args ...any) *sql.Row {
	if tx := TxFromContext(ctx); tx != nil {
		return tx.QueryRowContext(ctx, query, args...)
	}
	return db.QueryRowContext(ctx, query, args...)
}

// TxManager manages database transactions, ensuring atomicity across
// multiple repository operations.
type TxManager interface {
	WithTransaction(ctx context.Context, fn func(ctx context.Context) error) error
}

type txManager struct {
	db *sql.DB
}

// NewTxManager creates a TxManager backed by the given DBGetter.
func NewTxManager(getter DBGetter) TxManager {
	return &txManager{db: getter.DB()}
}

// WithTransaction begins a transaction, injects it into the context, runs fn,
// and commits or rolls back based on fn's outcome.
func (tm *txManager) WithTransaction(ctx context.Context, fn func(ctx context.Context) error) error {
	tx, err := tm.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	ctx = WithTxInContext(ctx, tx)
	if err := fn(ctx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}
