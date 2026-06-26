package migrator

import (
	"database/sql"
)



func EnsureTemplateSchema(db *sql.DB) error {

	_, err := db.Exec(`
		CREATE SCHEMA IF NOT EXISTS template_org
	`)

	return err
}