package migrator

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"reni-social/schema/loader"
	"reni-social/schema/sqlgen"
)

func RunMigrations(db *sql.DB, schema string, dir string) error {

	files, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	var migrations []string

	for _, f := range files {
		migrations = append(migrations, f.Name())
	}

	sort.Strings(migrations)

	for _, file := range migrations {

		path := filepath.Join(dir, file)

		table, err := loader.LoadTable(path)
		if err != nil {
			return err
		}

		sql := sqlgen.GenerateCreateTable(schema, table)

		fmt.Println("Running:", sql)

		_, err = db.Exec(sql)
		if err != nil {
			return err
		}
	}

	return nil
}