package migrator

import "database/sql"

func RunGlobalMigrations(db *sql.DB) error {

	return RunMigrations(db, "public", "./migrations/global")
}