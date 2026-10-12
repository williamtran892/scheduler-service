package platform

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/go-sql-driver/mysql"
)

// OpenMySQL opens the application pool and checks the database is reachable.
// The schema is created by the database container (local/mysql/schema.sql), never here.
func OpenMySQL(ctx context.Context, dsn string) (*sql.DB, error) {
	if _, err := mysql.ParseDSN(dsn); err != nil {
		return nil, fmt.Errorf("parse MySQL DSN: %w", err)
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("open MySQL: %w", err)
	}
	db.SetMaxOpenConns(50)
	db.SetConnMaxLifetime(30 * time.Minute)

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping MySQL: %w", err)
	}
	return db, nil
}
