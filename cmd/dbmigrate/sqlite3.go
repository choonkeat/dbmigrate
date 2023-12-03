package main

// by default, Makefile `make build` compiles without this file
// if sqlite3 is required,
//      env CGO_ENABLED=1 make build BUILD_TARGET="./cmd/dbmigrate"

import (
	"context"
	"database/sql"

	"github.com/choonkeat/dbmigrate"
	_ "github.com/mattn/go-sqlite3"
)

func init() {
	dbmigrate.Register("sqlite3", dbmigrate.Adapter{
		CreateVersionsTable: func(_ *string) string {
			return `CREATE TABLE dbmigrate_versions (version char(14) NOT NULL PRIMARY KEY)`
		},
		SelectExistingVersions: func(_ *string) string { return `SELECT version FROM dbmigrate_versions ORDER BY version ASC` },
		InsertNewVersion:       func(_ *string) string { return `INSERT INTO dbmigrate_versions (version) VALUES (?)` },
		DeleteOldVersion:       func(_ *string) string { return `DELETE FROM dbmigrate_versions WHERE version = ?` },
		PingQuery:              "SELECT 1",
		BeginTx: func(ctx context.Context, db *sql.DB, opts *sql.TxOptions) (dbmigrate.ExecCommitRollbacker, error) {
			return db.BeginTx(ctx, opts)
		},
	})
}
