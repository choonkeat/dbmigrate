package main

import (
	"log"
	"os"
)

func main() {
	switch os.Getenv("DATABASE_DRIVER") {
	case "sqlite3":
		// DATABASE_DRIVER=sqlite3 DATABASE_URL="./sqlite3.db" go run examples/*.go
		log.Println(sqlite3DbmigrateUp())
	default:
		log.Println(simpleDbmigrateUp())
	}
}
