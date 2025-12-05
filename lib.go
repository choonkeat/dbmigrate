package dbmigrate

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"hash/crc32"
	"io/fs"
	"io/ioutil"
	"net/url"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/derekparker/trie"
	"github.com/pkg/errors"
)

// DbTxnMode controls how migrations are wrapped in transactions
type DbTxnMode string

const (
	// DbTxnModeAll wraps all pending migrations in a single transaction (existing behavior)
	DbTxnModeAll DbTxnMode = "all"
	// DbTxnModePerFile wraps each migration file in its own transaction
	DbTxnModePerFile DbTxnMode = "per-file"
	// DbTxnModeNone runs migrations without transaction wrapping
	DbTxnModeNone DbTxnMode = "none"
)

// ValidDbTxnModes lists all valid transaction mode values
var ValidDbTxnModes = []DbTxnMode{DbTxnModeAll, DbTxnModePerFile, DbTxnModeNone}

// ParseDbTxnMode parses a string into DbTxnMode, returns error if invalid
func ParseDbTxnMode(s string) (DbTxnMode, error) {
	mode := DbTxnMode(s)
	for _, valid := range ValidDbTxnModes {
		if mode == valid {
			return mode, nil
		}
	}
	return "", errors.Errorf("invalid -db-txn-mode %q: must be one of: all, per-file, none", s)
}

const noDbTxnMarker = ".no-db-txn."

// requiresNoTransaction returns true if filename contains the .no-db-txn. marker
func requiresNoTransaction(filename string) bool {
	return strings.Contains(filename, noDbTxnMarker)
}

// DbTxnModeConflictError is returned when .no-db-txn. files exist but mode is not "per-file" or "none"
type DbTxnModeConflictError struct {
	Files       []string
	CurrentMode DbTxnMode
}

func (e *DbTxnModeConflictError) Error() string {
	var sb strings.Builder
	sb.WriteString("Error: Cannot apply migrations in -db-txn-mode=all (default)\n\n")
	sb.WriteString("The following migrations require -db-txn-mode=per-file:\n")
	for _, f := range e.Files {
		sb.WriteString("  - ")
		sb.WriteString(f)
		sb.WriteString("\n")
	}
	sb.WriteString("\nRun with: dbmigrate -up -db-txn-mode=per-file")
	return sb.String()
}

// LockingNotSupportedError is returned when locking is required but not supported
type LockingNotSupportedError struct {
	DriverName string
}

func (e *LockingNotSupportedError) Error() string {
	return fmt.Sprintf(`%s does not support cross-process locking.

If you are certain only one migration process runs at a time, use:

  dbmigrate -up -no-lock

This is safe for single-process deployments (e.g., local development,
single-node production with migrations run before app starts).`, e.DriverName)
}

// validateDbTxnMode checks if pending files are compatible with the transaction mode
// Returns error if mode is "all" but .no-db-txn. files exist
func validateDbTxnMode(files []string, mode DbTxnMode) error {
	if mode != DbTxnModeAll {
		return nil
	}
	var conflicts []string
	for _, f := range files {
		if requiresNoTransaction(f) {
			conflicts = append(conflicts, f)
		}
	}
	if len(conflicts) > 0 {
		return &DbTxnModeConflictError{
			Files:       conflicts,
			CurrentMode: mode,
		}
	}
	return nil
}

// warnMySQLDDL prints a warning about MySQL DDL limitations
func warnMySQLDDL(driverName string, log func(string)) {
	if driverName != "mysql" {
		return
	}
	log("Warning: MySQL does not support transactional DDL.")
	log("         DDL statements (CREATE, ALTER, DROP) commit implicitly.")
	log("         Transaction mode has limited effect on DDL-heavy migrations.")
}

// RequireDriverName to indicate explicit driver name
var RequireDriverName = errors.Errorf("Cannot discern db driver. Please set -driver flag or DATABASE_DRIVER environment variable.")

// SanitizeDriverNameURL sanitizes `driverName` and `databaseURL` values
func SanitizeDriverNameURL(driverName string, databaseURL string) (dbdriver string, dburl string, err error) {
	// ensure db and driverName is legit
	databaseURL = strings.TrimSpace(databaseURL)
	if databaseURL == "" {
		return driverName, databaseURL, errors.Errorf("database url not set")
	}
	driverName = strings.TrimSpace(driverName)
	if driverName != "" {
		return driverName, databaseURL, nil
	}
	if u, err := url.Parse(databaseURL); strings.Contains(databaseURL, "://") && u != nil && err == nil {
		return u.Scheme, databaseURL, nil
	}
	return "", databaseURL, RequireDriverName
}

// ReadyWait for server to be ready, and try to create db and connect again
func ReadyWait(ctx context.Context, driverName string, databaseURLs []string, logger func(...interface{})) error {
	logger(driverName, "checking connection")
	adapter, err := AdapterFor(driverName)
	if err != nil {
		return err
	}

	count := len(databaseURLs)
	curr := -1
	for {
		curr = (curr + 1) % count
		db, err := sql.Open(driverName, databaseURLs[curr])
		if err == nil {
			logger(driverName, "server up")
			var num int
			if err = db.QueryRow(adapter.PingQuery).Scan(&num); err == nil {
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
	dir            fs.FS
	db             *sql.DB
	driverName     string
	databaseName   string
	adapter        Adapter
	migrationFiles []string
}

// New returns an instance of &Config
//
// Returns error when
// - database driver is unsupported (try adding support via `dbmigrate.Register`)
// - database fails to connect or retrieve existing versions
// - unable to read list of files from `dir`
func New(dir fs.FS, driverName string, databaseURL string) (*Config, error) {
	driverName, databaseURL, err := SanitizeDriverNameURL(driverName, databaseURL)
	if err != nil {
		return nil, errors.Wrapf(err, "see `--help` for more details.")
	}
	adapter, err := AdapterFor(driverName)
	if err != nil {
		return nil, err
	}

	// Extract database name for lock ID
	var databaseName string
	if adapter.BaseDatabaseURL != nil {
		_, databaseName, _ = adapter.BaseDatabaseURL(databaseURL)
	}
	if databaseName == "" {
		// Fallback: use the whole URL as identifier
		databaseName = databaseURL
	}

	db, err := sql.Open(driverName, databaseURL)
	if err != nil {
		return nil, errors.Wrapf(err, "unable to connect to -url")
	}

	var migrationFiles []string
	err = fs.WalkDir(dir, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		fp := path
		if !strings.HasSuffix(path, ".sql") &&
			strings.HasSuffix(d.Name(), ".sql") {
			fp = filepath.Join(path, d.Name())
		}
		migrationFiles = append(migrationFiles, fp)
		return nil
	})
	if err != nil {
		return nil, errors.Wrapf(err, "unable to read from directory %q", dir)
	}

	return &Config{
		dir:            dir,
		db:             db,
		driverName:     driverName,
		databaseName:   databaseName,
		adapter:        adapter,
		migrationFiles: migrationFiles,
	}, nil
}

// CloseDB should be run when Config is no longer in use; ideally `defer CloseDB` after every `New`
func (c *Config) CloseDB() error {
	return c.db.Close()
}

// DriverName returns the database driver name for this config
func (c *Config) DriverName() string {
	return c.driverName
}

// acquireLock acquires the migration lock, returns the connection holding the lock
// Returns nil conn if noLock is true or adapter doesn't support locking
func (c *Config) acquireLock(ctx context.Context, schema *string, noLock bool, log func(string)) (*sql.Conn, error) {
	if noLock {
		if c.adapter.SupportsLocking {
			log("Warning: Running without cross-process locking. Concurrent migrations may cause corruption.")
		}
		return nil, nil
	}

	if !c.adapter.SupportsLocking {
		return nil, &LockingNotSupportedError{DriverName: c.driverName}
	}

	conn, err := c.db.Conn(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "unable to get connection for locking")
	}

	lockID := generateLockID(c.databaseName, schema, "dbmigrate_versions")
	if err := c.adapter.AcquireLock(ctx, conn, fmt.Sprint(lockID), log); err != nil {
		conn.Close()
		return nil, errors.Wrap(err, "unable to acquire migration lock")
	}

	return conn, nil
}

// releaseLock releases the migration lock
func (c *Config) releaseLock(ctx context.Context, conn *sql.Conn, schema *string) {
	if conn == nil {
		return
	}
	lockID := generateLockID(c.databaseName, schema, "dbmigrate_versions")
	_ = c.adapter.ReleaseLock(ctx, conn, fmt.Sprint(lockID))
	conn.Close()
}

func (c *Config) existingVersions(ctx context.Context, schema *string) (*trie.Trie, error) {
	// best effort create before we select; if the table is not there, next query will fail anyway
	_, errctx := c.db.ExecContext(ctx, c.adapter.CreateVersionsTable(schema))
	rows, err := c.db.QueryContext(ctx, c.adapter.SelectExistingVersions(schema))
	if err != nil {
		return nil, errors.Wrap(err, errctx.Error())
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
func (c *Config) PendingVersions(ctx context.Context, schema *string) ([]string, error) {
	migratedVersions, err := c.existingVersions(ctx, schema)
	if err != nil {
		return nil, errors.Wrapf(err, "unable to query existing versions")
	}

	migrationFiles := c.migrationFiles
	sort.SliceStable(migrationFiles, func(i int, j int) bool {
		return strings.Compare(migrationFiles[i], migrationFiles[j]) == -1 // in ascending order
	})

	result := []string{}
	for i := range migrationFiles {
		currName := migrationFiles[i]
		if !strings.HasSuffix(currName, "up.sql") {
			continue // skip if this isn't a `up.sql`
		}
		currVer := strings.Split(currName, "_")[0]
		if _, found := migratedVersions.Find(currVer); found {
			continue // skip if we've migrated this version
		}
		result = append(result, currVer)
	}
	return result, nil
}

// ExecCommitRollbacker interface for sql.Tx
type ExecCommitRollbacker interface {
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
	Commit() error
	Rollback() error
}

// MigrateUp applies pending migrations in ascending order, in a transaction
//
// Transaction is committed on success, rollback on error. Different databases will behave
// differently, e.g. postgres & sqlite3 can rollback DDL changes but mysql cannot
func (c *Config) MigrateUp(ctx context.Context, txOpts *sql.TxOptions, schema *string, logFilename func(string)) error {
	return c.MigrateUpWithMode(ctx, txOpts, schema, logFilename, DbTxnModeAll, false)
}

// MigrateUpWithMode applies pending migrations with the specified transaction mode
func (c *Config) MigrateUpWithMode(ctx context.Context, txOpts *sql.TxOptions, schema *string, logFilename func(string), mode DbTxnMode, noLock bool) error {
	// Acquire lock
	conn, err := c.acquireLock(ctx, schema, noLock, logFilename)
	if err != nil {
		return err
	}
	defer c.releaseLock(ctx, conn, schema)

	// MySQL DDL warning
	warnMySQLDDL(c.driverName, logFilename)

	migratedVersions, err := c.existingVersions(ctx, schema)
	if err != nil {
		return errors.Wrapf(err, "unable to query existing versions")
	}

	migrationFiles := c.migrationFiles
	sort.SliceStable(migrationFiles, func(i int, j int) bool {
		return strings.Compare(migrationFiles[i], migrationFiles[j]) == -1 // in ascending order
	})

	// Collect pending files for validation
	var pendingFiles []string
	for i := range migrationFiles {
		currName := migrationFiles[i]
		if !strings.HasSuffix(currName, "up.sql") {
			continue
		}
		currVer := strings.Split(currName, "_")[0]
		if _, found := migratedVersions.Find(currVer); found {
			continue
		}
		pendingFiles = append(pendingFiles, currName)
	}

	// Validate transaction mode compatibility
	if err := validateDbTxnMode(pendingFiles, mode); err != nil {
		return err
	}

	// Dispatch to appropriate migration strategy
	switch mode {
	case DbTxnModeAll:
		return c.migrateUpAll(ctx, txOpts, schema, logFilename, pendingFiles)
	case DbTxnModePerFile:
		return c.migrateUpPerFile(ctx, txOpts, schema, logFilename, pendingFiles)
	case DbTxnModeNone:
		return c.migrateUpNoTx(ctx, schema, logFilename, pendingFiles)
	default:
		return errors.Errorf("unknown transaction mode: %s", mode)
	}
}

// migrateUpAll runs all pending migrations in a single transaction (existing behavior)
func (c *Config) migrateUpAll(ctx context.Context, txOpts *sql.TxOptions, schema *string, logFilename func(string), pendingFiles []string) error {
	tx, err := c.adapter.BeginTx(ctx, c.db, txOpts)
	if err != nil {
		return errors.Wrapf(err, "unable to create transaction")
	}
	defer tx.Rollback()

	for _, currName := range pendingFiles {
		currVer := strings.Split(currName, "_")[0]

		filecontent, err := c.fileContent(currName)
		if err != nil {
			return errors.Wrapf(err, currName)
		}

		if len(bytes.TrimSpace(filecontent)) == 0 {
			// treat empty file as success; don't run it
		} else if _, err := tx.ExecContext(ctx, string(filecontent)); err != nil {
			return errors.Wrapf(err, currName)
		}
		if _, err := tx.ExecContext(ctx, c.adapter.InsertNewVersion(schema), currVer); err != nil {
			return errors.Wrapf(err, "fail to register version %q", currVer)
		}
		logFilename(currName)
	}

	err = tx.Commit()
	if err != nil && err.Error() == "pq: unexpected transaction status idle" {
		return nil
	}
	return errors.Wrapf(err, "unable to commit transaction")
}

// migrateUpPerFile runs each migration in its own transaction
// .no-db-txn. files run without transaction
func (c *Config) migrateUpPerFile(ctx context.Context, txOpts *sql.TxOptions, schema *string, logFilename func(string), pendingFiles []string) error {
	applied := 0
	for _, currName := range pendingFiles {
		currVer := strings.Split(currName, "_")[0]

		filecontent, err := c.fileContent(currName)
		if err != nil {
			return errors.Wrapf(err, currName)
		}

		if requiresNoTransaction(currName) {
			// Run without transaction
			if len(bytes.TrimSpace(filecontent)) > 0 {
				if _, err := c.db.ExecContext(ctx, string(filecontent)); err != nil {
					if applied > 0 {
						logFilename(fmt.Sprintf("%d migrations applied before failure.", applied))
					}
					return errors.Wrapf(err, currName)
				}
			}
			if _, err := c.db.ExecContext(ctx, c.adapter.InsertNewVersion(schema), currVer); err != nil {
				return errors.Wrapf(err, "fail to register version %q", currVer)
			}
		} else {
			// Run in transaction
			tx, err := c.adapter.BeginTx(ctx, c.db, txOpts)
			if err != nil {
				return errors.Wrapf(err, "unable to create transaction for %s", currName)
			}

			if len(bytes.TrimSpace(filecontent)) > 0 {
				if _, err := tx.ExecContext(ctx, string(filecontent)); err != nil {
					tx.Rollback()
					if applied > 0 {
						logFilename(fmt.Sprintf("%d migrations applied before failure.", applied))
					}
					return errors.Wrapf(err, currName)
				}
			}
			if _, err := tx.ExecContext(ctx, c.adapter.InsertNewVersion(schema), currVer); err != nil {
				tx.Rollback()
				return errors.Wrapf(err, "fail to register version %q", currVer)
			}

			if err := tx.Commit(); err != nil {
				if err.Error() != "pq: unexpected transaction status idle" {
					return errors.Wrapf(err, "unable to commit transaction for %s", currName)
				}
			}
		}
		logFilename(currName)
		applied++
	}
	return nil
}

// migrateUpNoTx runs all migrations without any transaction wrapping
func (c *Config) migrateUpNoTx(ctx context.Context, schema *string, logFilename func(string), pendingFiles []string) error {
	applied := 0
	for _, currName := range pendingFiles {
		currVer := strings.Split(currName, "_")[0]

		filecontent, err := c.fileContent(currName)
		if err != nil {
			return errors.Wrapf(err, currName)
		}

		if len(bytes.TrimSpace(filecontent)) > 0 {
			if _, err := c.db.ExecContext(ctx, string(filecontent)); err != nil {
				if applied > 0 {
					logFilename(fmt.Sprintf("%d migrations applied before failure.", applied))
				}
				return errors.Wrapf(err, currName)
			}
		}
		if _, err := c.db.ExecContext(ctx, c.adapter.InsertNewVersion(schema), currVer); err != nil {
			return errors.Wrapf(err, "fail to register version %q", currVer)
		}
		logFilename(currName)
		applied++
	}
	return nil
}

// MigrateDown un-applies at most N migrations in descending order, in a transaction
//
// Transaction is committed on success, rollback on error. Different databases will behave
// differently, e.g. postgres & sqlite3 can rollback DDL changes but mysql cannot
func (c *Config) MigrateDown(ctx context.Context, txOpts *sql.TxOptions, schema *string, logFilename func(string), downStep int) error {
	return c.MigrateDownWithMode(ctx, txOpts, schema, logFilename, downStep, DbTxnModeAll, false)
}

// MigrateDownWithMode un-applies migrations with the specified transaction mode
func (c *Config) MigrateDownWithMode(ctx context.Context, txOpts *sql.TxOptions, schema *string, logFilename func(string), downStep int, mode DbTxnMode, noLock bool) error {
	// Acquire lock
	conn, err := c.acquireLock(ctx, schema, noLock, logFilename)
	if err != nil {
		return err
	}
	defer c.releaseLock(ctx, conn, schema)

	// MySQL DDL warning
	warnMySQLDDL(c.driverName, logFilename)

	migratedVersions, err := c.existingVersions(ctx, schema)
	if err != nil {
		return errors.Wrapf(err, "unable to query existing versions")
	}

	migrationFiles := c.migrationFiles
	sort.SliceStable(migrationFiles, func(i int, j int) bool {
		return strings.Compare(migrationFiles[i], migrationFiles[j]) == 1 // descending order
	})

	// Collect applicable down files
	var downFiles []string
	counted := 0
	for i := range migrationFiles {
		currName := migrationFiles[i]
		if !strings.HasSuffix(currName, "down.sql") {
			continue
		}
		currVer := strings.Split(currName, "_")[0]
		if _, found := migratedVersions.Find(currVer); !found {
			continue
		}
		counted++
		if counted > downStep {
			break
		}
		downFiles = append(downFiles, currName)
	}

	// Validate transaction mode compatibility
	if err := validateDbTxnMode(downFiles, mode); err != nil {
		return err
	}

	// Dispatch to appropriate strategy
	switch mode {
	case DbTxnModeAll:
		return c.migrateDownAll(ctx, txOpts, schema, logFilename, downFiles)
	case DbTxnModePerFile:
		return c.migrateDownPerFile(ctx, txOpts, schema, logFilename, downFiles)
	case DbTxnModeNone:
		return c.migrateDownNoTx(ctx, schema, logFilename, downFiles)
	default:
		return errors.Errorf("unknown transaction mode: %s", mode)
	}
}

// migrateDownAll runs all down migrations in a single transaction
func (c *Config) migrateDownAll(ctx context.Context, txOpts *sql.TxOptions, schema *string, logFilename func(string), downFiles []string) error {
	tx, err := c.adapter.BeginTx(ctx, c.db, txOpts)
	if err != nil {
		return errors.Wrapf(err, "unable to create transaction")
	}
	defer tx.Rollback()

	for _, currName := range downFiles {
		currVer := strings.Split(currName, "_")[0]

		filecontent, err := c.fileContent(currName)
		if err != nil {
			return errors.Wrapf(err, currName)
		}

		if len(bytes.TrimSpace(filecontent)) == 0 {
			// treat empty file as success
		} else if _, err := tx.ExecContext(ctx, string(filecontent)); err != nil {
			return errors.Wrapf(err, currName)
		}
		if _, err := tx.ExecContext(ctx, c.adapter.DeleteOldVersion(schema), currVer); err != nil {
			return errors.Wrapf(err, "fail to unregister version %q", currVer)
		}
		logFilename(currName)
	}

	err = tx.Commit()
	if err != nil && err.Error() == "pq: unexpected transaction status idle" {
		return nil
	}
	return errors.Wrapf(err, "unable to commit transaction")
}

// migrateDownPerFile runs each down migration in its own transaction
func (c *Config) migrateDownPerFile(ctx context.Context, txOpts *sql.TxOptions, schema *string, logFilename func(string), downFiles []string) error {
	applied := 0
	for _, currName := range downFiles {
		currVer := strings.Split(currName, "_")[0]

		filecontent, err := c.fileContent(currName)
		if err != nil {
			return errors.Wrapf(err, currName)
		}

		if requiresNoTransaction(currName) {
			if len(bytes.TrimSpace(filecontent)) > 0 {
				if _, err := c.db.ExecContext(ctx, string(filecontent)); err != nil {
					if applied > 0 {
						logFilename(fmt.Sprintf("%d migrations rolled back before failure.", applied))
					}
					return errors.Wrapf(err, currName)
				}
			}
			if _, err := c.db.ExecContext(ctx, c.adapter.DeleteOldVersion(schema), currVer); err != nil {
				return errors.Wrapf(err, "fail to unregister version %q", currVer)
			}
		} else {
			tx, err := c.adapter.BeginTx(ctx, c.db, txOpts)
			if err != nil {
				return errors.Wrapf(err, "unable to create transaction for %s", currName)
			}

			if len(bytes.TrimSpace(filecontent)) > 0 {
				if _, err := tx.ExecContext(ctx, string(filecontent)); err != nil {
					tx.Rollback()
					if applied > 0 {
						logFilename(fmt.Sprintf("%d migrations rolled back before failure.", applied))
					}
					return errors.Wrapf(err, currName)
				}
			}
			if _, err := tx.ExecContext(ctx, c.adapter.DeleteOldVersion(schema), currVer); err != nil {
				tx.Rollback()
				return errors.Wrapf(err, "fail to unregister version %q", currVer)
			}

			if err := tx.Commit(); err != nil {
				if err.Error() != "pq: unexpected transaction status idle" {
					return errors.Wrapf(err, "unable to commit transaction for %s", currName)
				}
			}
		}
		logFilename(currName)
		applied++
	}
	return nil
}

// migrateDownNoTx runs all down migrations without transaction
func (c *Config) migrateDownNoTx(ctx context.Context, schema *string, logFilename func(string), downFiles []string) error {
	applied := 0
	for _, currName := range downFiles {
		currVer := strings.Split(currName, "_")[0]

		filecontent, err := c.fileContent(currName)
		if err != nil {
			return errors.Wrapf(err, currName)
		}

		if len(bytes.TrimSpace(filecontent)) > 0 {
			if _, err := c.db.ExecContext(ctx, string(filecontent)); err != nil {
				if applied > 0 {
					logFilename(fmt.Sprintf("%d migrations rolled back before failure.", applied))
				}
				return errors.Wrapf(err, currName)
			}
		}
		if _, err := c.db.ExecContext(ctx, c.adapter.DeleteOldVersion(schema), currVer); err != nil {
			return errors.Wrapf(err, "fail to unregister version %q", currVer)
		}
		logFilename(currName)
		applied++
	}
	return nil
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
	CreateVersionsTable    func(*string) string
	SelectExistingVersions func(*string) string
	InsertNewVersion       func(*string) string
	DeleteOldVersion       func(*string) string
	PingQuery              string                                                     // `""` means does NOT support -server-ready
	CreateDatabaseQuery    func(string) string                                        // nil means does NOT support -create-db
	CreateSchemaQuery      func(string) string                                        // nil means does NOT support -schema
	BaseDatabaseURL        func(string) (connString string, dbName string, err error) // nil means does not support -server-ready nor -create-db
	BeginTx                func(ctx context.Context, db *sql.DB, opts *sql.TxOptions) (ExecCommitRollbacker, error)
	// Locking support for cross-process safety
	SupportsLocking bool                                                                           // false means requires -no-lock flag
	AcquireLock     func(ctx context.Context, conn *sql.Conn, lockID string, log func(string)) error // nil if SupportsLocking is false
	ReleaseLock     func(ctx context.Context, conn *sql.Conn, lockID string) error                   // nil if SupportsLocking is false
}

// generateLockID creates a lock ID from database name, schema, and table name
func generateLockID(databaseName string, schema *string, tableName string) int64 {
	parts := []string{databaseName}
	if schema != nil && *schema != "" {
		parts = append(parts, *schema)
	}
	parts = append(parts, tableName)

	input := strings.Join(parts, "\x00")
	sum := crc32.ChecksumIEEE([]byte(input))
	return int64(sum)
}

func fqName(schema *string, name string) string {
	if schema == nil || *schema == "" {
		return name
	}
	return *schema + "." + name
}

var adapters = map[string]Adapter{
	"postgres": {
		CreateVersionsTable: func(schema *string) string {
			return `CREATE TABLE IF NOT EXISTS ` + fqName(schema, "dbmigrate_versions") + ` (version char(14) NOT NULL PRIMARY KEY)`
		},
		SelectExistingVersions: func(schema *string) string {
			return `SELECT version FROM ` + fqName(schema, "dbmigrate_versions") + ` ORDER BY version ASC`
		},
		InsertNewVersion: func(schema *string) string {
			return `INSERT INTO ` + fqName(schema, "dbmigrate_versions") + ` (version) VALUES ($1)`
		},
		DeleteOldVersion: func(schema *string) string {
			return `DELETE FROM ` + fqName(schema, "dbmigrate_versions") + ` WHERE version = $1`
		},
		PingQuery: "SELECT 1",
		BaseDatabaseURL: func(databaseURL string) (string, string, error) {
			paths := strings.Split(databaseURL, "/")
			pathlen := len(paths)
			requestURI := strings.Split(paths[pathlen-1], "?")
			basePaths := []string{strings.Join(paths[:pathlen-1], "/") + "/postgres"}

			if len(requestURI) > 1 {
				basePaths = append(basePaths, requestURI[1:]...)
			}
			return strings.Join(basePaths, "?"), requestURI[0], nil
		},
		CreateDatabaseQuery: func(dbName string) string {
			return "CREATE DATABASE " + dbName
		},
		CreateSchemaQuery: func(schemaName string) string {
			return "CREATE SCHEMA IF NOT EXISTS " + schemaName
		},
		BeginTx: func(ctx context.Context, db *sql.DB, opts *sql.TxOptions) (ExecCommitRollbacker, error) {
			return db.BeginTx(ctx, opts)
		},
		SupportsLocking: true,
		AcquireLock: func(ctx context.Context, conn *sql.Conn, lockID string, log func(string)) error {
			for {
				var acquired bool
				err := conn.QueryRowContext(ctx, "SELECT pg_try_advisory_lock($1)", lockID).Scan(&acquired)
				if err != nil {
					return err
				}
				if acquired {
					return nil
				}
				log("Waiting for migration lock...")
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(2 * time.Second):
				}
			}
		},
		ReleaseLock: func(ctx context.Context, conn *sql.Conn, lockID string) error {
			_, err := conn.ExecContext(ctx, "SELECT pg_advisory_unlock($1)", lockID)
			return err
		},
	},
	"mysql": {
		CreateVersionsTable: func(_ *string) string {
			return `CREATE TABLE dbmigrate_versions (version char(14) NOT NULL PRIMARY KEY)`
		},
		SelectExistingVersions: func(_ *string) string { return `SELECT version FROM dbmigrate_versions ORDER BY version ASC` },
		InsertNewVersion:       func(_ *string) string { return `INSERT INTO dbmigrate_versions (version) VALUES (?)` },
		DeleteOldVersion:       func(_ *string) string { return `DELETE FROM dbmigrate_versions WHERE version = ?` },
		PingQuery:              "SELECT 1",
		BaseDatabaseURL: func(databaseURL string) (string, string, error) {
			paths := strings.Split(databaseURL, "/")
			pathlen := len(paths)
			requestURI := strings.Split(paths[pathlen-1], "?")
			basePaths := []string{strings.Join(paths[:pathlen-1], "/") + "/mysql"}

			if len(requestURI) > 1 {
				basePaths = append(basePaths, requestURI[1:]...)
			}
			return strings.Join(basePaths, "?"), requestURI[0], nil
		},
		CreateDatabaseQuery: func(dbName string) string {
			return "CREATE DATABASE " + dbName
		},
		BeginTx: func(ctx context.Context, db *sql.DB, opts *sql.TxOptions) (ExecCommitRollbacker, error) {
			return db.BeginTx(ctx, opts)
		},
		SupportsLocking: true,
		AcquireLock: func(ctx context.Context, conn *sql.Conn, lockID string, log func(string)) error {
			for {
				var result sql.NullInt64
				err := conn.QueryRowContext(ctx, "SELECT GET_LOCK(?, 0)", lockID).Scan(&result)
				if err != nil {
					return err
				}
				if result.Valid && result.Int64 == 1 {
					return nil
				}
				log("Waiting for migration lock...")
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(2 * time.Second):
				}
			}
		},
		ReleaseLock: func(ctx context.Context, conn *sql.Conn, lockID string) error {
			_, err := conn.ExecContext(ctx, "SELECT RELEASE_LOCK(?)", lockID)
			return err
		},
	},
}

// AdapterFor returns Adapter for given driverName
func AdapterFor(driverName string) (Adapter, error) {
	a, ok := adapters[driverName]
	if !ok {
		return a, errors.Errorf("unsupported driver name %q", driverName)
	}
	return a, nil
}
