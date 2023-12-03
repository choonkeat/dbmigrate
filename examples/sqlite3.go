package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"time"

	"github.com/choonkeat/dbmigrate"
	_ "github.com/mattn/go-sqlite3"
)

func sqlite3DbmigrateUp() error {
	dbmigrate.Register("sqlite3", dbmigrate.Adapter{
		CreateVersionsTable: func(_ *string) string {
			return `CREATE TABLE dbmigrate_versions (version char(14) NOT NULL PRIMARY KEY)`
		},
		SelectExistingVersions: func(_ *string) string { return `SELECT version FROM dbmigrate_versions ORDER BY version ASC` },
		InsertNewVersion:       func(_ *string) string { return `INSERT INTO dbmigrate_versions (version) VALUES (?)` },
		DeleteOldVersion:       func(_ *string) string { return `DELETE FROM dbmigrate_versions WHERE version = ?` },
	})

	// though we're using plain local file system in this example
	// `fileSystem` could be anything that implements http.FileSystem
	// e.g. gobuffalo/packr, go-bindata-assetfs, etc
	fileSystem := os.DirFS("tests/db/sqlite3")

	// Example env variables
	//   DATABASE_DRIVER=sqlite3
	//   DATABASE_URL="./sqlite3.db"
	m, err := dbmigrate.New(fileSystem, os.Getenv("DATABASE_DRIVER"), os.Getenv("DATABASE_URL"))
	if err != nil {
		return err
	}
	defer m.CloseDB()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	return m.MigrateUp(ctx, &sql.TxOptions{}, nil, func(currentFilename string) {
		fmt.Println("[migrate up]", currentFilename) // optional print out of which file was migrated
	})
}
