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

type dbAdapter struct {
	sqlCreateVersionsTable    string
	sqlSelectExistingVersions string
	sqlInsertNewVersion       string
	sqlDeleteOldVersion       string
}

var adapters = map[string]dbAdapter{
	"postgres": dbAdapter{
		sqlCreateVersionsTable:    `CREATE TABLE dbmigrate_versions (version text NOT NULL PRIMARY KEY)`,
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

var (
	operationCreate bool
	operationUp     bool
	downStep        int
	dirname         string
	databaseURL     string
	driverName      string
	timeout         time.Duration
	adapter         dbAdapter
)

func main() {
	if err := _main(); err != nil {
		log.Fatalln(err.Error())
	}
}

func _main() error {
	// options
	flag.BoolVar(&operationCreate, "create", false, "add new migration files into -dirname")
	flag.BoolVar(&operationUp, "up", false, "perform migrations in sequence")
	flag.IntVar(&downStep, "down", 0, "undo the last N migrations")
	flag.StringVar(&dirname, "dir", "db/migrations", "directory storing all the *.sql files")
	flag.StringVar(&databaseURL, "url", os.Getenv("DATABASE_URL"), "connection string to database, e.g. postgres://user:pass@host:5432/myproject_development")
	flag.StringVar(&driverName, "driver", os.Getenv("DATABASE_DRIVER"), "drivername, e.g. postgres")
	flag.DurationVar(&timeout, "timeout", time.Minute, "database timeout")
	flag.Parse()

	// 1. CREATE new migration; exit
	if operationCreate {
		description := strings.Join(flag.Args(), " ")
		name := versionedName(time.Now(), description)
		if err := os.MkdirAll(dirname, 0755); err != nil {
			return errors.Wrapf(err, "failed to create -dirname %q", dirname)
		}
		if err := writeFile(dirname, name); err != nil {
			return errors.Wrapf(err, "failed to write into -dirname %q", dirname)
		}
		return nil
	}

	// ensure db and driverName is legit
	if driverName == "" {
		// most of the time, the driver name is the `scheme` part of the url
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
	tx, err := db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return errors.Wrapf(err, "unable to create transaction")
	}

	// 2. MIGRATE UP or DOWN; exit
	if operationUp {
		sort.SliceStable(migrationFiles, func(i int, j int) bool {
			return strings.Compare(migrationFiles[i].Name(), migrationFiles[j].Name()) == -1
		})
		if err = migrateUp(ctx, tx, dirname, migrationFiles, migratedVersions); err != nil {
			tx.Rollback()
			return err
		}
		return tx.Commit()
	} else if downStep > 0 {
		sort.SliceStable(migrationFiles, func(i int, j int) bool {
			return strings.Compare(migrationFiles[i].Name(), migrationFiles[j].Name()) == 1
		})
		if err = migrateDown(ctx, tx, dirname, downStep, migrationFiles, migratedVersions); err != nil {
			tx.Rollback()
			return err
		}
		return tx.Commit()
	}

	return errors.Errorf("no operation: must be either `-create`, `-up`, or `-down 1`")
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
