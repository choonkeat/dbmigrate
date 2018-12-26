package dbmigrate

import (
	"context"
	"database/sql"
	"io/ioutil"
	"os"
	"path"
	"sort"
	"strings"

	"github.com/derekparker/trie"
	"github.com/pkg/errors"
)

// Config to perform dbmigrate
type Config struct {
	dirname        string
	db             *sql.DB
	adapter        dbAdapter
	migrationFiles []os.FileInfo
}

// New instance of Config to perform dbmigrate
func New(dirname string, driverName string, databaseURL string) (*Config, error) {
	// ensure db and driverName is legit
	databaseURL = strings.TrimSpace(databaseURL)
	if databaseURL == "" {
		return nil, errors.Errorf("either `-url` command line flag or DATABASE_URL environment variable must be set. see `--help` for more details.")
	}
	if driverName == "" {
		// fall back to use the `scheme` part of the url as driverName
		// e.g. `postgres://localhost:5432/dbmigrate_test` will thus be `postgres`
		driverName = strings.Split(databaseURL, ":")[0]
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

	migrationFiles, err := ioutil.ReadDir(dirname)
	if err != nil {
		return nil, errors.Wrapf(err, "unable to read from -dir %q", dirname)
	}

	return &Config{
		dirname:        dirname,
		db:             db,
		adapter:        adapter,
		migrationFiles: migrationFiles,
	}, nil
}

// CloseDB closes DB
func (c *Config) CloseDB() error {
	return c.db.Close()
}

func (c *Config) existingVersions(ctx context.Context) (*trie.Trie, error) {
	// best effort create before we select; if the table is not there, next query will fail anyway
	_, _ = c.db.ExecContext(ctx, c.adapter.sqlCreateVersionsTable)
	rows, err := c.db.QueryContext(ctx, c.adapter.sqlSelectExistingVersions)
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

// MigrateUp applies pending migrations in ascending order
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
		filecontent, err := ioutil.ReadFile(path.Join(c.dirname, currName))
		if err != nil {
			return errors.Wrapf(err, currName)
		}
		if _, err := tx.ExecContext(ctx, string(filecontent)); err != nil {
			return errors.Wrapf(err, currName)
		}
		if _, err := tx.ExecContext(ctx, c.adapter.sqlInsertNewVersion, currVer); err != nil {
			return errors.Wrapf(err, "fail to register version %q", currVer)
		}
		logFilename(currName)
	}
	return tx.Commit()
}

// MigrateDown un-apply up to N migrations in descending order
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
		filecontent, err := ioutil.ReadFile(path.Join(c.dirname, currName))
		if err != nil {
			return errors.Wrapf(err, currName)
		}
		if _, err := tx.ExecContext(ctx, string(filecontent)); err != nil {
			return errors.Wrapf(err, currName)
		}
		if _, err := tx.ExecContext(ctx, c.adapter.sqlDeleteOldVersion, currVer); err != nil {
			return errors.Wrapf(err, "fail to unregister version %q", currVer)
		}
		logFilename(currName)
	}
	return tx.Commit()
}

//

type dbAdapter struct {
	sqlCreateVersionsTable    string
	sqlSelectExistingVersions string
	sqlInsertNewVersion       string
	sqlDeleteOldVersion       string
}

var adapters = map[string]dbAdapter{
	"postgres": dbAdapter{
		sqlCreateVersionsTable:    `CREATE TABLE dbmigrate_versions (version char(14) NOT NULL PRIMARY KEY)`,
		sqlSelectExistingVersions: `SELECT version FROM dbmigrate_versions ORDER BY version ASC`,
		sqlInsertNewVersion:       `INSERT INTO dbmigrate_versions (version) VALUES ($1)`,
		sqlDeleteOldVersion:       `DELETE FROM dbmigrate_versions WHERE version = $1`,
	},
	"mysql": dbAdapter{
		sqlCreateVersionsTable:    `CREATE TABLE dbmigrate_versions (version char(14) NOT NULL PRIMARY KEY)`,
		sqlSelectExistingVersions: `SELECT version FROM dbmigrate_versions ORDER BY version ASC`,
		sqlInsertNewVersion:       `INSERT INTO dbmigrate_versions (version) VALUES (?)`,
		sqlDeleteOldVersion:       `DELETE FROM dbmigrate_versions WHERE version = ?`,
	},
}
