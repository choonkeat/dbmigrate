package dbmigrate

import (
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestBaseDatabaseURL(t *testing.T) {
	defaultDbName := "/" + time.Now().Format("db20060102150405")
	testCases := []struct {
		givenDatabaseURL string
		expectedBaseURL  string
		expectedDbname   string
	}{
		{
			givenDatabaseURL: "postgres://user:password@host:5432/foobar?sslmode=disabled",
			expectedBaseURL:  "postgres://user:password@host:5432" + defaultDbName + "?sslmode=disabled",
			expectedDbname:   "foobar",
		},
		{
			givenDatabaseURL: "postgres://user:password@host:5432/foobar",
			expectedBaseURL:  "postgres://user:password@host:5432" + defaultDbName,
			expectedDbname:   "foobar",
		},
		{
			givenDatabaseURL: "postgres://host:5432/foobar",
			expectedBaseURL:  "postgres://host:5432" + defaultDbName,
			expectedDbname:   "foobar",
		},
		{
			givenDatabaseURL: "root:password@tcp(127.0.0.1:65500)/foobar?multiStatements=true",
			expectedBaseURL:  "root:password@tcp(127.0.0.1:65500)" + defaultDbName + "?multiStatements=true",
			expectedDbname:   "foobar",
		},
		{
			givenDatabaseURL: "root:password@tcp(127.0.0.1:65500)/foobar",
			expectedBaseURL:  "root:password@tcp(127.0.0.1:65500)" + defaultDbName,
			expectedDbname:   "foobar",
		},
		{
			givenDatabaseURL: "tcp(127.0.0.1:65500)/foobar",
			expectedBaseURL:  "tcp(127.0.0.1:65500)" + defaultDbName,
			expectedDbname:   "foobar",
		},
	}

	for i, tc := range testCases {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			actualBaseURL, actualDbname, err := BaseDatabaseURL("unused", tc.givenDatabaseURL, defaultDbName)
			assert.Nil(t, err)
			assert.Equal(t, tc.expectedBaseURL, actualBaseURL)
			assert.Equal(t, tc.expectedDbname, actualDbname)
		})
	}
}
