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

var (
	doCreateMigration bool
	doPendingVersions bool
	doMigrateUp       bool
	doMigrateDown     int
	dirname           string
	databaseURL       string
	driverName        string
	timeout           time.Duration
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

	m, err := dbmigrate.New(dirname, driverName, databaseURL)
	if err != nil {
		return err
	}
	defer m.CloseDB()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// 2. SHOW pending versions; exit
	if doPendingVersions {
		versions, err := m.PendingVersions(ctx)
		if err != nil {
			return err
		}
		fmt.Println(strings.Join(versions, "\n"))
		return nil
	}

	// 3. MIGRATE UP; exit
	if doMigrateUp {
		return m.MigrateUp(ctx, &sql.TxOptions{}, filenameLogger("[up]"))
	}

	// 4. MIGRATE DOWN; exit
	if doMigrateDown > 0 {
		return m.MigrateDown(ctx, &sql.TxOptions{}, filenameLogger("[down]"), doMigrateDown)
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
	err := ioutil.WriteFile(upfile, []byte(nil), 0644)
	if err != nil {
		return err
	}
	log.Println("writing", downfile)
	return ioutil.WriteFile(downfile, []byte(nil), 0644)
}
