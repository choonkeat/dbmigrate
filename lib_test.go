package dbmigrate

import (
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
