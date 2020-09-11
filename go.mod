module github.com/choonkeat/dbmigrate

go 1.14

replace github.com/MichaelS11/go-cql-driver => github.com/choonkeat/go-cql-driver v0.0.0-20200911061401-46bdfd182e1a

require (
	github.com/MichaelS11/go-cql-driver v0.0.0-20190914174813-cf3b3196aa43
	github.com/derekparker/trie v0.0.0-20180212171413-e608c2733dc7
	github.com/go-sql-driver/mysql v1.4.1
	github.com/gocql/gocql v0.0.0-20200624222514-34081eda590e // indirect
	github.com/lib/pq v1.0.0
	github.com/mattn/go-sqlite3 v1.10.0
	github.com/pkg/errors v0.8.0
	github.com/stretchr/testify v1.3.0
	google.golang.org/appengine v1.4.0 // indirect
)
