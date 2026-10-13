package mysql

import (
	"context"
	"database/sql"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
)

// store is Scheduling's persistence on MySQL. It implements Store.
type store struct {
	db *sql.DB
}

var _ Store = (*store)(nil)

// New returns a store over db.
func New(db *sql.DB) Store { return &store{db: db} }

// tx is one transaction: a booking attempt or a lifecycle transition. It implements Tx.
type tx struct {
	ctx context.Context
	tx  *sql.Tx
}

var _ Tx = (*tx)(nil)

// InTx runs fn in one transaction at the default isolation level: committed if fn returns nil,
// rolled back otherwise. Each transaction is a span, so a request is one trace down to COMMIT.
func (s *store) InTx(ctx context.Context, fn func(tx Tx) error) error {
	ctx, span := otel.Tracer("scheduler").Start(ctx, "scheduling.InTx")
	defer span.End()

	sqlTx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		span.SetStatus(codes.Error, "begin")
		return fmt.Errorf("begin transaction: %w", err)
	}
	if err := fn(&tx{ctx: ctx, tx: sqlTx}); err != nil {
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
