package dbmigrate

import (
	"errors"
	"fmt"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
)

func fileline() string {
	_, fn, line, _ := runtime.Caller(1)
	return fmt.Sprintf("%s:%d", fn, line)
}

func TestSanitizeDriverNameURL(t *testing.T) {
	testCases := []struct {
		name                string
		givenDatabaseURL    string
		expectedDriverName  string
		expectedDatabaseURL string
		expectedError       error
	}{
		{
			name:                fileline(),
			givenDatabaseURL:    "postgres://user:password@host:1234/dbname?sslmode=disabled",
			expectedDriverName:  "postgres",
			expectedDatabaseURL: "postgres://user:password@host:1234/dbname?sslmode=disabled",
			expectedError:       nil,
		},
		{
			name:                fileline(),
			givenDatabaseURL:    "postgres://host:1234/dbname?sslmode=disabled",
			expectedDriverName:  "postgres",
			expectedDatabaseURL: "postgres://host:1234/dbname?sslmode=disabled",
			expectedError:       nil,
		},
		{
			name:                fileline(),
			givenDatabaseURL:    "user:password@tcp(host:1234)/dbname?multiStatements=true",
			expectedDriverName:  "",
			expectedDatabaseURL: "user:password@tcp(host:1234)/dbname?multiStatements=true",
			// https://github.com/go-sql-driver/mysql#dsn-data-source-name
			expectedError: RequireDriverName,
		},
		{
			name:                fileline(),
			givenDatabaseURL:    "tcp(host:1234)/dbname?multiStatements=true",
			expectedDriverName:  "",
			expectedDatabaseURL: "tcp(host:1234)/dbname?multiStatements=true",
			// https://github.com/go-sql-driver/mysql#dsn-data-source-name
			expectedError: RequireDriverName,
		},
		{
			name:                fileline(),
			givenDatabaseURL:    "./tests/sqlite3.db",
			expectedDriverName:  "",
			expectedDatabaseURL: "./tests/sqlite3.db",
			expectedError:       RequireDriverName,
		},
		{
			name:                fileline(),
			givenDatabaseURL:    "localhost:65500?keyspace=foobar",
			expectedDriverName:  "",
			expectedDatabaseURL: "localhost:65500?keyspace=foobar",
			expectedError:       RequireDriverName,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actualDriverName, actualDatabaseURL, err := SanitizeDriverNameURL("", tc.givenDatabaseURL)
			if tc.expectedError == nil {
				assert.NoError(t, err)
			} else {
				assert.Equal(t, tc.expectedError, err)
			}
			assert.Equal(t, tc.expectedDriverName, actualDriverName, "driver name")
			assert.Equal(t, tc.expectedDatabaseURL, actualDatabaseURL, "database url")
		})
	}
}

func TestValidateDbTxnMode(t *testing.T) {
	tests := []struct {
		name    string
		files   []string
		mode    DbTxnMode
		wantErr bool
	}{
		{
			name:    "all mode with normal files",
			files:   []string{"20240101_create.up.sql", "20240102_add.up.sql"},
			mode:    DbTxnModeAll,
			wantErr: false,
		},
		{
			name:    "all mode with no-db-txn file",
			files:   []string{"20240101_create.up.sql", "20240102_add.no-db-txn.up.sql"},
			mode:    DbTxnModeAll,
			wantErr: true,
		},
		{
			name:    "per-file mode with no-db-txn file",
			files:   []string{"20240101_create.up.sql", "20240102_add.no-db-txn.up.sql"},
			mode:    DbTxnModePerFile,
			wantErr: false,
		},
		{
			name:    "none mode with no-db-txn file",
			files:   []string{"20240101_create.up.sql", "20240102_add.no-db-txn.up.sql"},
			mode:    DbTxnModeNone,
			wantErr: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateDbTxnMode(tc.files, tc.mode)
			if tc.wantErr {
				assert.Error(t, err)
				var conflictErr *DbTxnModeConflictError
				assert.True(t, errors.As(err, &conflictErr))
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestDbTxnModeConflictError(t *testing.T) {
	err := &DbTxnModeConflictError{
		Files:       []string{"20240101130000_add_index.no-db-txn.up.sql"},
		CurrentMode: DbTxnModeAll,
	}
	msg := err.Error()
	assert.Contains(t, msg, "Cannot apply migrations in -db-txn-mode=all")
	assert.Contains(t, msg, "20240101130000_add_index.no-db-txn.up.sql")
	assert.Contains(t, msg, "-db-txn-mode=per-file")
}

func TestLockingNotSupportedError(t *testing.T) {
	err := &LockingNotSupportedError{DriverName: "sqlite3"}
	msg := err.Error()
	assert.Contains(t, msg, "sqlite3 does not support cross-process locking")
	assert.Contains(t, msg, "-no-lock")
}

func TestRequiresNoTransaction(t *testing.T) {
	tests := []struct {
		filename string
		expected bool
	}{
		{"20240101120000_create_users.up.sql", false},
		{"20240101120000_create_users.down.sql", false},
		{"20240101130000_add_index.no-db-txn.up.sql", true},
		{"20240101130000_add_index.no-db-txn.down.sql", true},
		{"some/path/20240101130000_add_index.no-db-txn.up.sql", true},
		// Partial matches should not trigger (exact ".no-db-txn." required)
		{"20240101130000_add_index.no-db-txnup.sql", false},  // missing trailing dot
		{"20240101130000_add_indexno-db-txn.up.sql", false},  // missing leading dot
		{"20240101130000_add_index.no-db-tx.up.sql", false},  // truncated marker
		{"20240101130000_add_index.o-db-txn.up.sql", false},  // missing 'n' at start
		// Case mismatches should not trigger (exact ".no-db-txn." required)
		{"20240101130000_add_index.No-Db-Txn.up.sql", false},
		{"20240101130000_add_index.NO-DB-TXN.up.sql", false},
	}
	for _, tc := range tests {
		result := requiresNoTransaction(tc.filename)
		assert.Equal(t, tc.expected, result, "filename: %s", tc.filename)
	}
}

func TestParseDbTxnMode(t *testing.T) {
	tests := []struct {
		input    string
		expected DbTxnMode
		wantErr  bool
	}{
		{"all", DbTxnModeAll, false},
		{"per-file", DbTxnModePerFile, false},
		{"none", DbTxnModeNone, false},
		{"invalid", "", true},
		{"", "", true},
		// Partial matches should fail (exact match required)
		{"al", "", true},        // "all" missing last char
		{"ll", "", true},        // "all" missing first char
		{"per-fil", "", true},   // "per-file" missing last char
		{"er-file", "", true},   // "per-file" missing first char
		{"non", "", true},       // "none" missing last char
		// Case mismatches should fail (exact match required)
		{"All", "", true},
		{"ALL", "", true},
		{"Per-File", "", true},
		{"PER-FILE", "", true},
		{"None", "", true},
		{"NONE", "", true},
	}
	for _, tc := range tests {
		mode, err := ParseDbTxnMode(tc.input)
		if tc.wantErr {
			assert.Error(t, err)
		} else {
			assert.NoError(t, err)
			assert.Equal(t, tc.expected, mode)
		}
	}
}

func TestGenerateLockID(t *testing.T) {
	schema := "myschema"
	tests := []struct {
		name   string
		dbName string
		schema *string
		table  string
	}{
		{"basic", "mydb", nil, "dbmigrate_versions"},
		{"with schema", "mydb", &schema, "dbmigrate_versions"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			id := generateLockID(tc.dbName, tc.schema, tc.table)
			assert.NotZero(t, id)
			// Same inputs should produce same ID
			id2 := generateLockID(tc.dbName, tc.schema, tc.table)
			assert.Equal(t, id, id2)
		})
	}

	// Different inputs should produce different IDs
	id1 := generateLockID("db1", nil, "dbmigrate_versions")
	id2 := generateLockID("db2", nil, "dbmigrate_versions")
	assert.NotEqual(t, id1, id2)
}

func TestBaseDatabaseURL(t *testing.T) {
	testCases := []struct {
		name             string
		givenDriverName  string
		givenDatabaseURL string
		expectedBaseURL  string
		expectedDbname   string
		expectedError    string
	}{
		{
			name:             fileline(),
			givenDatabaseURL: "postgres://user:password@host:5432/foobar?sslmode=disabled",
			expectedBaseURL:  "postgres://user:password@host:5432/postgres?sslmode=disabled",
			expectedDbname:   "foobar",
		},
		{
			name:             fileline(),
			givenDatabaseURL: "postgres://user:password@host:5432/foobar",
			expectedBaseURL:  "postgres://user:password@host:5432/postgres",
			expectedDbname:   "foobar",
		},
		{
			name:             fileline(),
			givenDatabaseURL: "postgres://host:5432/foobar",
			expectedBaseURL:  "postgres://host:5432/postgres",
			expectedDbname:   "foobar",
		},
		{
			name:             fileline(),
			givenDatabaseURL: "root:password@tcp(127.0.0.1:65500)/foobar?multiStatements=true",
			expectedBaseURL:  "root:password@tcp(127.0.0.1:65500)/mysql?multiStatements=true",
			expectedDbname:   "foobar",
			expectedError:    RequireDriverName.Error(),
		},
		{
			name:             fileline(),
			givenDriverName:  "mysql",
			givenDatabaseURL: "root:password@tcp(127.0.0.1:65500)/foobar?multiStatements=true",
			expectedBaseURL:  "root:password@tcp(127.0.0.1:65500)/mysql?multiStatements=true",
			expectedDbname:   "foobar",
		},
		{
			name:             fileline(),
			givenDatabaseURL: "root:password@tcp(127.0.0.1:65500)/foobar",
			expectedBaseURL:  "root:password@tcp(127.0.0.1:65500)/mysql",
			expectedDbname:   "foobar",
			expectedError:    RequireDriverName.Error(),
		},
		{
			name:             fileline(),
			givenDriverName:  "mysql",
			givenDatabaseURL: "root:password@tcp(127.0.0.1:65500)/foobar",
			expectedBaseURL:  "root:password@tcp(127.0.0.1:65500)/mysql",
			expectedDbname:   "foobar",
		},
		{
			name:             fileline(),
			givenDatabaseURL: "tcp(127.0.0.1:65500)/foobar",
			expectedBaseURL:  "tcp(127.0.0.1:65500)/mysql",
			expectedDbname:   "foobar",
			expectedError:    RequireDriverName.Error(),
		},
		{
			name:             fileline(),
			givenDriverName:  "mysql",
			givenDatabaseURL: "tcp(127.0.0.1:65500)/foobar",
			expectedBaseURL:  "tcp(127.0.0.1:65500)/mysql",
			expectedDbname:   "foobar",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			driverName, databaseURL, err := SanitizeDriverNameURL(tc.givenDriverName, tc.givenDatabaseURL)
			if tc.expectedError == "" {
				assert.NoError(t, err)
			} else {
				assert.EqualError(t, err, tc.expectedError)
				return
			}
			adapter, err := AdapterFor(driverName)
			if tc.expectedError == "" {
				assert.NoError(t, err)
			} else {
				assert.EqualError(t, err, tc.expectedError)
				return
			}
			if err == nil {
				actualBaseURL, actualDbname, err := adapter.BaseDatabaseURL(databaseURL)
				assert.Nil(t, err)
				assert.Equal(t, tc.expectedBaseURL, actualBaseURL)
				assert.Equal(t, tc.expectedDbname, actualDbname)
			}
		})
	}
}
