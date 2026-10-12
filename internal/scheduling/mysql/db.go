package mysql

import (
	"context"
	"database/sql"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"

	"scheduler/internal/scheduling"
)

// Store is Scheduling's persistence on MySQL. It implements scheduling.Store.
type Store struct {
	db *sql.DB
}

var _ scheduling.Store = (*Store)(nil)

// New returns a store over db.
func New(db *sql.DB) *Store { return &Store{db: db} }

// Tx is one booking transaction. It implements scheduling.Tx.
type Tx struct {
	ctx context.Context
	tx  *sql.Tx
}

var _ scheduling.Tx = (*Tx)(nil)

// InTx runs fn in one transaction at the default isolation level: committed if fn returns nil,
// rolled back otherwise. Each transaction is a span, so a request is one trace down to COMMIT.
func (s *Store) InTx(ctx context.Context, fn func(tx scheduling.Tx) error) error {
	ctx, span := otel.Tracer("scheduler").Start(ctx, "scheduling.InTx")
	defer span.End()

	sqlTx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		span.SetStatus(codes.Error, "begin")
		return fmt.Errorf("begin transaction: %w", err)
	}
	if err := fn(&Tx{ctx: ctx, tx: sqlTx}); err != nil {
		_ = sqlTx.Rollback()
		span.RecordError(err)
		span.SetStatus(codes.Error, "rolled back")
		return err
	}
	if err := sqlTx.Commit(); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "commit")
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}
