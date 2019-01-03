package dbmigrate

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBaseDatabaseURL(t *testing.T) {
	testCases := []struct {
		givenDatabaseURL string
		expectedBaseURL  string
		expectedDbname   string
	}{
		{
			givenDatabaseURL: "postgres://user:password@host:5432/foobar?sslmode=disabled",
			expectedBaseURL:  "postgres://user:password@host:5432/?sslmode=disabled",
			expectedDbname:   "foobar",
		},
		{
			givenDatabaseURL: "postgres://user:password@host:5432/foobar",
			expectedBaseURL:  "postgres://user:password@host:5432/",
			expectedDbname:   "foobar",
		},
		{
			givenDatabaseURL: "postgres://host:5432/foobar",
			expectedBaseURL:  "postgres://host:5432/",
			expectedDbname:   "foobar",
		},
		{
			givenDatabaseURL: "root:password@tcp(127.0.0.1:65500)/foobar?multiStatements=true",
			expectedBaseURL:  "root:password@tcp(127.0.0.1:65500)/?multiStatements=true",
			expectedDbname:   "foobar",
		},
		{
			givenDatabaseURL: "root:password@tcp(127.0.0.1:65500)/foobar",
			expectedBaseURL:  "root:password@tcp(127.0.0.1:65500)/",
			expectedDbname:   "foobar",
		},
		{
			givenDatabaseURL: "tcp(127.0.0.1:65500)/foobar",
			expectedBaseURL:  "tcp(127.0.0.1:65500)/",
			expectedDbname:   "foobar",
		},
	}

	for i, tc := range testCases {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			actualBaseURL, actualDbname, err := BaseDatabaseURL("unused", tc.givenDatabaseURL)
			assert.Nil(t, err)
			assert.Equal(t, tc.expectedBaseURL, actualBaseURL)
			assert.Equal(t, tc.expectedDbname, actualDbname)
		})
	}
}
