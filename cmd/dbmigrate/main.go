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
	"strings"
	"time"

	"github.com/choonkeat/dbmigrate"
	"github.com/pkg/errors"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
)

func main() {
	if err := _main(); err != nil {
		log.Fatalln(err.Error())
	}
}

func _main() error {
	var (
		serverReadyWait   time.Duration
		doCreateDB        bool
		dbSchema          *string
		doCreateMigration bool
		doPendingVersions bool
		doMigrateUp       bool
		doMigrateDown     int
		dirname           string
		databaseURL       string
		driverName        string
		timeout           time.Duration
		errctx            error
	)

	// options
	flag.DurationVar(&serverReadyWait,
		"server-ready", 0, "wait until database server is ready, then continue")
	flag.BoolVar(&doCreateDB,
		"create-db", false, "create postgres database (ignore errors), then continue")
	dbSchema = flag.String("schema", "", "create schema if necessary (ignore errors), then continue")
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
		if err := os.MkdirAll(dirname, 0o755); err != nil {
			return errors.Wrapf(err, "failed to create -dir %q", dirname)
		}
		if err := writeFile(dirname, name); err != nil {
			return errors.Wrapf(err, "failed to write into -dir %q", dirname)
		}
		return nil
	}

	driverName, databaseURL, errctx = dbmigrate.SanitizeDriverNameURL(driverName, databaseURL)

	if doServerReadyWait := serverReadyWait > 0; doServerReadyWait || doCreateDB || dbSchema != nil {
		adapter, err := dbmigrate.AdapterFor(driverName)
		if err != nil {
			return errors.Wrap(err, errctx.Error())
		}

		if doServerReadyWait {
			if adapter.BaseDatabaseURL == nil {
				return errors.Errorf("%q does not support -server-ready", driverName)
			}
			connString, _, err := adapter.BaseDatabaseURL(databaseURL)
			if err != nil {
				return errors.Wrap(err, errctx.Error())
			}
			ctx, cancel := context.WithTimeout(context.Background(), serverReadyWait)
			defer cancel()
			if err := dbmigrate.ReadyWait(ctx, driverName, []string{databaseURL, connString}, log.Println); err != nil {
				return errors.Wrap(err, errctx.Error())
			}
		}

		if doCreateDB {
			if adapter.BaseDatabaseURL == nil {
				return errors.Errorf("%q does not support -create-db", driverName)
			}
			if adapter.CreateDatabaseQuery == nil {
				return errors.Errorf("%q does not support -create-db", driverName)
			}
			connString, dbName, err := adapter.BaseDatabaseURL(databaseURL)
			if err != nil {
				return errors.Wrap(err, errctx.Error())
			}
			db, err := sql.Open(driverName, connString)
			if err != nil {
				return errors.Wrapf(err, "connect to db")
			}
			// leave errors for subsequent actions
			_, errctx = db.Exec(adapter.CreateDatabaseQuery(dbName))
			_ = db.Close()
		}

		if dbSchema != nil && *dbSchema != "" {
			if adapter.CreateSchemaQuery == nil {
				return errors.Errorf("%q does not support -schema", driverName)
			}
			db, err := sql.Open(driverName, databaseURL)
			if err != nil {
				return errors.Wrapf(err, "connect to db")
			}
			// leave errors for subsequent actions
			_, errctx = db.Exec(adapter.CreateSchemaQuery(*dbSchema))
			_ = db.Close()
		}
	}

	m, err := dbmigrate.New(os.DirFS(dirname), driverName, databaseURL)
	if err != nil {
		return errors.Wrap(err, errctx.Error())
	}
	defer m.CloseDB()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// 2. SHOW pending versions; exit
	if doPendingVersions {
		versions, err := m.PendingVersions(ctx, dbSchema)
		if err != nil {
			return errors.Wrap(err, errctx.Error())
		}
		fmt.Println(strings.Join(versions, "\n"))
		return nil
	}

	// 3. MIGRATE UP; exit
	if doMigrateUp {
		return m.MigrateUp(ctx, &sql.TxOptions{}, dbSchema, filenameLogger("[up]"))
	}

	// 4. MIGRATE DOWN; exit
	if doMigrateDown > 0 {
		return m.MigrateDown(ctx, &sql.TxOptions{}, dbSchema, filenameLogger("[down]"), doMigrateDown)
	}

	// None of the above, fail
	return errors.Errorf("no operation: must be either `-create`, `-versions-pending`, `-up`, or `-down 1`")
}

func filenameLogger(prefix string) func(string) {
	return func(s string) {
		log.Println(prefix, s)
	}
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
	err := ioutil.WriteFile(upfile, []byte(nil), 0o644)
	if err != nil {
		return err
	}
	log.Println("writing", downfile)
	return ioutil.WriteFile(downfile, []byte(nil), 0o644)
}
