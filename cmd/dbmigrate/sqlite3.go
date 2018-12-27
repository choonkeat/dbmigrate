package main

// by default, Makefile `make build` compiles without this file
// if sqlite3 is required,
//      env CGO_ENABLED=1 make build BUILD_TARGET="./cmd/dbmigrate"

import (
	"github.com/choonkeat/dbmigrate"
	_ "github.com/mattn/go-sqlite3"
)

func init() {
	dbmigrate.Register("sqlite3", dbmigrate.Adapter{
		CreateVersionsTable:    `CREATE TABLE dbmigrate_versions (version char(14) NOT NULL PRIMARY KEY)`,
		SelectExistingVersions: `SELECT version FROM dbmigrate_versions ORDER BY version ASC`,
		InsertNewVersion:       `INSERT INTO dbmigrate_versions (version) VALUES (?)`,
		DeleteOldVersion:       `DELETE FROM dbmigrate_versions WHERE version = ?`,
	})
}
