package dbmigrate

import (
	"bytes"
	"context"
	"database/sql"
	"io/ioutil"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/derekparker/trie"
	"github.com/pkg/errors"
)

// SanitizeDriverNameURL sanitizes `driverName` and `databaseURL` values
func SanitizeDriverNameURL(driverName string, databaseURL string) (string, string, error) {
	// ensure db and driverName is legit
	databaseURL = strings.TrimSpace(databaseURL)
	if databaseURL == "" {
		return driverName, databaseURL, errors.Errorf("database url not set")
	}
	if driverName == "" {
		// fall back to use the `scheme` part of the url as driverName
		// e.g. `postgres://localhost:5432/dbmigrate_test` will thus be `postgres`
		driverName = strings.Split(databaseURL, ":")[0]
	}
	return driverName, databaseURL, nil
}

// BaseDatabaseURL returns the connection string to connect to the server (without the database name)
func BaseDatabaseURL(driverName string, databaseURL string, defaultDbName string) (string, string, error) {
	driverName, databaseURL, err := SanitizeDriverNameURL(driverName, databaseURL)
	if err != nil {
		return "", "", err
	}

	paths := strings.Split(databaseURL, "/")
	pathlen := len(paths)
	requestURI := strings.Split(paths[pathlen-1], "?")
	basePaths := []string{strings.Join(paths[:pathlen-1], "/") + defaultDbName}

	if len(requestURI) > 1 {
		basePaths = append(basePaths, requestURI[1:]...)
	}
	return strings.Join(basePaths, "?"), requestURI[0], nil
}

// ReadyWait for server to be ready, and try to create db and connect again
func ReadyWait(ctx context.Context, driverName string, databaseURL string, logger func(...interface{})) error {
	logger(driverName, "checking connection")
	for {
		db, err := sql.Open(driverName, databaseURL)
		if err == nil {
			logger(driverName, "server up")
			var num int
			if err = db.QueryRow("SELECT 1").Scan(&num); err == nil {
				logger(driverName, "connected")
				return db.Close()
			}
			db.Close()
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
			logger(driverName, "retrying...", err)
		}
	}
}

// A Config holds on to an open database to perform dbmigrate
type Config struct {
	dir            http.FileSystem
	db             *sql.DB
	adapter        Adapter
	migrationFiles []os.FileInfo
}

// New returns an instance of &Config
//
// Returns error when
// - database driver is unsupported (try adding support via `dbmigrate.Register`)
// - database fails to connect or retrieve existing versions
// - unable to read list of files from `dir`
func New(dir http.FileSystem, driverName string, databaseURL string) (*Config, error) {
	driverName, databaseURL, err := SanitizeDriverNameURL(driverName, databaseURL)
	if err != nil {
		return nil, errors.Wrapf(err, "see `--help` for more details.")
	}
	var ok bool
	adapter, ok := adapters[driverName]
	if !ok {
		return nil, errors.Errorf("unsupported driver name %q", driverName)
	}
	db, err := sql.Open(driverName, databaseURL)
	if err != nil {
		return nil, errors.Wrapf(err, "unable to connect to -url")
	}

	f, err := dir.Open(".")
	if err != nil {
		return nil, errors.Wrapf(err, "unable to open directory %q", dir)
	}
	defer f.Close()

	migrationFiles, err := f.Readdir(-1)
	if err != nil {
		return nil, errors.Wrapf(err, "unable to read from directory %q", dir)
	}

	return &Config{
		dir:            dir,
		db:             db,
		adapter:        adapter,
		migrationFiles: migrationFiles,
	}, nil
}

// CloseDB should be run when Config is no longer in use; ideally `defer CloseDB` after every `New`
func (c *Config) CloseDB() error {
	return c.db.Close()
}

func (c *Config) existingVersions(ctx context.Context) (*trie.Trie, error) {
	// best effort create before we select; if the table is not there, next query will fail anyway
	_, _ = c.db.ExecContext(ctx, c.adapter.CreateVersionsTable)
	rows, err := c.db.QueryContext(ctx, c.adapter.SelectExistingVersions)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := trie.New()
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		result.Add(strings.TrimSpace(s), 1)
	}
	return result, nil
}

// PendingVersions returns a slice of version strings that are not appled in the database yet
func (c *Config) PendingVersions(ctx context.Context) ([]string, error) {
	migratedVersions, err := c.existingVersions(ctx)
	if err != nil {
		return nil, errors.Wrapf(err, "unable to query existing versions")
	}

	migrationFiles := c.migrationFiles
	sort.SliceStable(migrationFiles, func(i int, j int) bool {
		return strings.Compare(migrationFiles[i].Name(), migrationFiles[j].Name()) == -1 // in ascending order
	})

	result := []string{}
	for i := range migrationFiles {
		currFile := migrationFiles[i]
		currName := currFile.Name()
		if !strings.HasSuffix(currName, ".up.sql") {
			continue // skip if this isn't a `.up.sql`
		}
		currVer := strings.Split(currName, "_")[0]
		if _, found := migratedVersions.Find(currVer); found {
			continue // skip if we've migrated this version
		}
		result = append(result, currVer)
	}
	return result, nil
}

// MigrateUp applies pending migrations in ascending order, in a transaction
//
// Transaction is committed on success, rollback on error. Different databases will behave
// differently, e.g. postgres & sqlite3 can rollback DDL changes but mysql cannot
func (c *Config) MigrateUp(ctx context.Context, txOpts *sql.TxOptions, logFilename func(string)) error {
	migratedVersions, err := c.existingVersions(ctx)
	if err != nil {
		return errors.Wrapf(err, "unable to query existing versions")
	}

	tx, err := c.db.BeginTx(ctx, txOpts)
	if err != nil {
		return errors.Wrapf(err, "unable to create transaction")
	}
	defer tx.Rollback() // ok to fail rollback if we did `tx.Commit`

	migrationFiles := c.migrationFiles
	sort.SliceStable(migrationFiles, func(i int, j int) bool {
		return strings.Compare(migrationFiles[i].Name(), migrationFiles[j].Name()) == -1 // in ascending order
	})

	for i := range migrationFiles {
		currFile := migrationFiles[i]
		currName := currFile.Name()
		if !strings.HasSuffix(currName, ".up.sql") {
			continue // skip if this isn't a `.up.sql`
		}
		currVer := strings.Split(currName, "_")[0]
		if _, found := migratedVersions.Find(currVer); found {
			continue // skip if we've migrated this version
		}

		// read the file, run the sql and insert a row into `dbmigrate_versions`
		filecontent, err := c.fileContent(currName)
		if err != nil {
			return errors.Wrapf(err, currName)
		}

		if len(bytes.TrimSpace(filecontent)) == 0 {
			// treat empty file as success; don't run it
		} else if _, err := tx.ExecContext(ctx, string(filecontent)); err != nil {
			return errors.Wrapf(err, currName)
		}
		if _, err := tx.ExecContext(ctx, c.adapter.InsertNewVersion, currVer); err != nil {
			return errors.Wrapf(err, "fail to register version %q", currVer)
		}
		logFilename(currName)
	}
	return tx.Commit()
}

// MigrateDown un-applies at most N migrations in descending order, in a transaction
//
// Transaction is committed on success, rollback on error. Different databases will behave
// differently, e.g. postgres & sqlite3 can rollback DDL changes but mysql cannot
func (c *Config) MigrateDown(ctx context.Context, txOpts *sql.TxOptions, logFilename func(string), downStep int) error {
	migratedVersions, err := c.existingVersions(ctx)
	if err != nil {
		return errors.Wrapf(err, "unable to query existing versions")
	}

	tx, err := c.db.BeginTx(ctx, txOpts)
	if err != nil {
		return errors.Wrapf(err, "unable to create transaction")
	}
	defer tx.Rollback() // ok to fail rollback if we did `tx.Commit`

	migrationFiles := c.migrationFiles
	sort.SliceStable(migrationFiles, func(i int, j int) bool {
		return strings.Compare(migrationFiles[i].Name(), migrationFiles[j].Name()) == 1 // descending order
	})

	counted := 0
	for i := range migrationFiles {
		currFile := migrationFiles[i]
		currName := currFile.Name()
		if !strings.HasSuffix(currName, ".down.sql") {
			continue // skip if this isn't a `.down.sql`
		}
		currVer := strings.Split(currName, "_")[0]
		if _, found := migratedVersions.Find(currVer); !found {
			continue // skip if we've NOT migrated this version
		}
		counted++
		if counted > downStep {
			break // time to stop
		}

		// read the file, run the sql and delete row from `dbmigrate_versions`
		filecontent, err := c.fileContent(currName)
		if err != nil {
			return errors.Wrapf(err, currName)
		}

		if len(bytes.TrimSpace(filecontent)) == 0 {
			// treat empty file as success; don't run it
		} else if _, err := tx.ExecContext(ctx, string(filecontent)); err != nil {
			return errors.Wrapf(err, currName)
		}
		if _, err := tx.ExecContext(ctx, c.adapter.DeleteOldVersion, currVer); err != nil {
			return errors.Wrapf(err, "fail to unregister version %q", currVer)
		}
		logFilename(currName)
	}
	return tx.Commit()
}

func (c *Config) fileContent(currName string) ([]byte, error) {
	f, err := c.dir.Open(currName)
	if err != nil {
		return nil, errors.Wrapf(err, currName)
	}
	defer f.Close()

	return ioutil.ReadAll(f)
}

// Register a new adapter.
//
// NOTE that postgres and mysql is supported out of the box.
// sqlite3 is supported by including cmd/dbmigrate/sqlite3.go during compilation
func Register(name string, value Adapter) {
	adapters[name] = value
}

// Adapter defines raw sql statements to run for an sql.DB adapter
type Adapter struct {
	CreateVersionsTable    string
	SelectExistingVersions string
	InsertNewVersion       string
	DeleteOldVersion       string
}

var adapters = map[string]Adapter{
	"postgres": Adapter{
		CreateVersionsTable:    `CREATE TABLE dbmigrate_versions (version char(14) NOT NULL PRIMARY KEY)`,
		SelectExistingVersions: `SELECT version FROM dbmigrate_versions ORDER BY version ASC`,
		InsertNewVersion:       `INSERT INTO dbmigrate_versions (version) VALUES ($1)`,
		DeleteOldVersion:       `DELETE FROM dbmigrate_versions WHERE version = $1`,
	},
	"mysql": Adapter{
		CreateVersionsTable:    `CREATE TABLE dbmigrate_versions (version char(14) NOT NULL PRIMARY KEY)`,
		SelectExistingVersions: `SELECT version FROM dbmigrate_versions ORDER BY version ASC`,
		InsertNewVersion:       `INSERT INTO dbmigrate_versions (version) VALUES (?)`,
		DeleteOldVersion:       `DELETE FROM dbmigrate_versions WHERE version = ?`,
	},
}
