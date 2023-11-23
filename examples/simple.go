package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"time"

	"github.com/choonkeat/dbmigrate"
)

func simpleDbmigrateUp() error {
	// though we're using plain local file system in this example
	// `fileSystem` could be anything that implements http.FileSystem
	// e.g. gobuffalo/packr, go-bindata-assetfs, etc
	fileSystem := os.DirFS("db/migrations")

	// Example env variables
	//   DATABASE_DRIVER=postgres
	//   DATABASE_URL=postgres://postgres:postgres@localhost:5432/dbname?sslmode=disable
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
