// cmd/schema/main.go — Database schema synchronization tool.
//
// This tool reads YAML schema definitions from the schemas/ directory and
// applies them to the PostgreSQL database (CREATE TABLE IF NOT EXISTS, ALTER TABLE ADD COLUMN, etc.).
//
// Usage:
//   go run ./cmd/schema
//   go run -tags=prod ./cmd/schema
//
// TODO: Wire up the schema migrator/lock packages once they are ported to reni-social.
// For now, this binary compiles cleanly as a placeholder.
package main

import (
	"log"
	"os"

	"reni-social/pkg/config"
	"reni-social/pkg/db"
)

func main() {
	os.Setenv("DBURL", config.C.DBURL)
	db.Connect()
	defer db.DB.Close()

	log.Println("[Schema] Connected to database.")
	log.Println("[Schema] Schema migration runner — implementation pending.")
	log.Println("[Schema] Manually apply schemas from the schemas/ directory using psql or your migration tool.")
}
