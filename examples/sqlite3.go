package main

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/choonkeat/dbmigrate"
	_ "github.com/mattn/go-sqlite3"
)

func sqlite3DbmigrateUp() error {
	dbmigrate.Register("sqlite3", dbmigrate.Adapter{
		CreateVersionsTable:    `CREATE TABLE dbmigrate_versions (version char(14) NOT NULL PRIMARY KEY)`,
		SelectExistingVersions: `SELECT version FROM dbmigrate_versions ORDER BY version ASC`,
		InsertNewVersion:       `INSERT INTO dbmigrate_versions (version) VALUES (?)`,
		DeleteOldVersion:       `DELETE FROM dbmigrate_versions WHERE version = ?`,
	})

	// though we're using plain local file system in this example
	// `fileSystem` could be anything that implements http.FileSystem
	// e.g. gobuffalo/packr, go-bindata-assetfs, etc
	fileSystem := http.Dir("tests/db/sqlite3")

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

	return m.MigrateUp(ctx, &sql.TxOptions{}, func(currentFilename string) {
		fmt.Println("[migrate up]", currentFilename) // optional print out of which file was migrated
	})
}
