package main

// by default, Makefile `make build` compiles without this file
// if sqlite3 is required,
//      env CGO_ENABLED=1 make build BUILD_TARGET="./cmd/dbmigrate"

import (
	"context"
	"database/sql"
	"net/url"

	_ "github.com/MichaelS11/go-cql-driver"
	"github.com/choonkeat/dbmigrate"
	"github.com/pkg/errors"
)

func init() {
	dbmigrate.Register("cql", dbmigrate.Adapter{
		CreateVersionsTable: func(_ *string) string {
			return `CREATE TABLE IF NOT EXISTS dbmigrate_versions (version text, PRIMARY KEY (version));`
		},
		SelectExistingVersions: func(_ *string) string { return `SELECT version FROM dbmigrate_versions` },
		InsertNewVersion:       func(_ *string) string { return `INSERT INTO dbmigrate_versions (version) VALUES (?)` },
		DeleteOldVersion:       func(_ *string) string { return `DELETE FROM dbmigrate_versions WHERE version = ?` },
		PingQuery:              `SELECT gossip_generation FROM system.local`,
		BaseDatabaseURL: func(databaseURL string) (string, string, error) {
			u, err := url.Parse(databaseURL)
			if err != nil {
				return "", "", errors.Wrapf(err, "invalid cassandra dsn")
			}
			q := u.Query()
			dbName := q.Get("keyspace")
			q.Set("keyspace", "system") // default connection
			u.RawQuery = q.Encode()
			return u.String(), dbName, nil
		},
		BeginTx: func(ctx context.Context, db *sql.DB, opts *sql.TxOptions) (dbmigrate.ExecCommitRollbacker, error) {
			return &noTx{db: db}, nil
		},
		SupportsLocking: false,
		AcquireLock:     nil,
		ReleaseLock:     nil,
	})
}

// Implements dbmigrate.ExecCommitRollbacker
type noTx struct {
	db *sql.DB
}

func (tx *noTx) ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	return tx.db.ExecContext(ctx, query, args...)
}

func (tx *noTx) Commit() error {
	return nil
}

func (tx *noTx) Rollback() error {
	return nil
}
