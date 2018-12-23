package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/derekparker/trie"
	"github.com/pkg/errors"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
)

var (
	doCreateMigration bool
	doPendingVersions bool
	doMigrateUp       bool
	doMigrateDown     int
	dirname           string
	databaseURL       string
	driverName        string
	timeout           time.Duration
	adapter           dbAdapter
)

func main() {
	if err := _main(); err != nil {
		log.Fatalln(err.Error())
	}
}

func _main() error {
	// options
	flag.BoolVar(&doCreateMigration,
		"create", false, "add new migration files into -dir")
	flag.BoolVar(&doPendingVersions,
		"versions-pending", false, "show versions in `-dir` but not applied in `-url` database")
	flag.BoolVar(&doMigrateUp,
		"up", false, "perform migrations in sequence")
	flag.IntVar(&doMigrateDown,
		"down", 0, "undo the last N applied migrations")
	flag.StringVar(&dirname,
		"dir", "db/migrations", "directory storing all the *.sql files")
	flag.StringVar(&databaseURL,
		"url", os.Getenv("DATABASE_URL"), "connection string to database, e.g. postgres://user:pass@host:5432/myproject_development")
	flag.StringVar(&driverName,
		"driver", os.Getenv("DATABASE_DRIVER"), "drivername, e.g. postgres")
	flag.DurationVar(&timeout,
		"timeout", 5*time.Minute, "database timeout")
	flag.Parse()

	// 1. CREATE new migration; exit
	if doCreateMigration {
		description := strings.Join(flag.Args(), " ")
		name := versionedName(time.Now(), description)
		if err := os.MkdirAll(dirname, 0755); err != nil {
			return errors.Wrapf(err, "failed to create -dir %q", dirname)
		}
		if err := writeFile(dirname, name); err != nil {
			return errors.Wrapf(err, "failed to write into -dir %q", dirname)
		}
		return nil
	}

	// ensure db and driverName is legit
	if driverName == "" {
		// fall back to use the `scheme` part of the url as driverName
		// e.g. `postgres://localhost:5432/dbmigrate_test` will thus be `postgres`
		driverName = strings.Split(databaseURL, ":")[0]
	}
	var ok bool
	adapter, ok = adapters[driverName]
	if !ok {
		return errors.Errorf("unsupported driver name %q", driverName)
	}
	db, err := sql.Open(driverName, databaseURL)
	if err != nil {
		return errors.Wrapf(err, "unable to connect to -url")
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	migratedVersions, err := existingVersions(ctx, db)
	if err != nil {
		return errors.Wrapf(err, "unable to query existing versions")
	}
	migrationFiles, err := ioutil.ReadDir(dirname)
	if err != nil {
		return errors.Wrapf(err, "unable to read from -dir %q", dirname)
	}

	// 2. SHOW pending versions
	if doPendingVersions {
		sort.SliceStable(migrationFiles, func(i int, j int) bool {
			return strings.Compare(migrationFiles[i].Name(), migrationFiles[j].Name()) == -1
		})
		return showVersionsPending(ctx, migrationFiles, migratedVersions)
	}

	tx, err := db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return errors.Wrapf(err, "unable to create transaction")
	}

	// 3. MIGRATE UP or MIGRATE DOWN; exit
	if doMigrateUp {
		sort.SliceStable(migrationFiles, func(i int, j int) bool {
			return strings.Compare(migrationFiles[i].Name(), migrationFiles[j].Name()) == -1
		})
		if err = migrateUp(ctx, tx, dirname, migrationFiles, migratedVersions); err != nil {
			tx.Rollback()
			return err
		}
		return tx.Commit()
	} else if doMigrateDown > 0 {
		sort.SliceStable(migrationFiles, func(i int, j int) bool {
			return strings.Compare(migrationFiles[i].Name(), migrationFiles[j].Name()) == 1
		})
		if err = migrateDown(ctx, tx, dirname, doMigrateDown, migrationFiles, migratedVersions); err != nil {
			tx.Rollback()
			return err
		}
		return tx.Commit()
	}

	// None of the above, fail
	return errors.Errorf("no operation: must be either `-create`, `-versions-pending`, `-up`, or `-down 1`")
}

func existingVersions(ctx context.Context, db *sql.DB) (*trie.Trie, error) {
	// best effort create before we select; if the table is not there, next query will fail anyway
	_, _ = db.ExecContext(ctx, adapter.sqlCreateVersionsTable)
	rows, err := db.QueryContext(ctx, adapter.sqlSelectExistingVersions)
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
		result.Add(s, 1)
	}
	return result, nil
}

func showVersionsPending(ctx context.Context, ascFiles []os.FileInfo, migratedVersions *trie.Trie) error {
	for i := range ascFiles {
		currFile := ascFiles[i]
		currName := currFile.Name()
		if !strings.HasSuffix(currName, ".up.sql") {
			continue // skip if this isn't a `.up.sql`
		}
		currVer := strings.Split(currName, "_")[0]
		if _, found := migratedVersions.Find(currVer); found {
			continue // skip if we've migrated this version
		}
		fmt.Println(currVer)
	}
	return nil
}

func migrateUp(ctx context.Context, tx *sql.Tx, dirname string, ascFiles []os.FileInfo, migratedVersions *trie.Trie) error {
	for i := range ascFiles {
		currFile := ascFiles[i]
		currName := currFile.Name()
		if !strings.HasSuffix(currName, ".up.sql") {
			continue // skip if this isn't a `.up.sql`
		}
		currVer := strings.Split(currName, "_")[0]
		if _, found := migratedVersions.Find(currVer); found {
			continue // skip if we've migrated this version
		}

		// read the file, run the sql and insert a row into `dbmigrate_versions`
		filecontent, err := ioutil.ReadFile(path.Join(dirname, currName))
		if err != nil {
			return errors.Wrapf(err, currName)
		}
		if _, err := tx.ExecContext(ctx, string(filecontent)); err != nil {
			return errors.Wrapf(err, currName)
		}
		if _, err := tx.ExecContext(ctx, adapter.sqlInsertNewVersion, currVer); err != nil {
			return errors.Wrapf(err, "fail to register version %q", currVer)
		}
		log.Println("[up]", currName)
	}
	return nil
}

func migrateDown(ctx context.Context, tx *sql.Tx, dirname string, downStep int, descFiles []os.FileInfo, migratedVersions *trie.Trie) error {
	counted := 0
	for i := range descFiles {
		currFile := descFiles[i]
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
		filecontent, err := ioutil.ReadFile(path.Join(dirname, currName))
		if err != nil {
			return errors.Wrapf(err, currName)
		}
		if _, err := tx.ExecContext(ctx, string(filecontent)); err != nil {
			return errors.Wrapf(err, currName)
		}
		if _, err := tx.ExecContext(ctx, adapter.sqlDeleteOldVersion, currVer); err != nil {
			return errors.Wrapf(err, "fail to unregister version %q", currVer)
		}
		log.Println("[down]", currName)
	}
	return nil
}

var (
	replaceString = "-"
	sanitize      = regexp.MustCompile(`\W+`)
)

func versionedName(now time.Time, description string) string {
	s := sanitize.ReplaceAllString(strings.ToLower(description), replaceString)
	return fmt.Sprintf("%s_%s",
		now.UTC().Format("20060102150405"),
		strings.TrimSuffix(strings.TrimPrefix(s, replaceString), replaceString),
	)
}

func writeFile(dirname, name string) error {
	upfile, downfile := path.Join(dirname, name+".up.sql"), path.Join(dirname, name+".down.sql")
	log.Println("writing", upfile)
	err := ioutil.WriteFile(upfile, []byte(nil), 0644)
	if err != nil {
		return err
	}
	log.Println("writing", downfile)
	return ioutil.WriteFile(downfile, []byte(nil), 0644)
}

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
