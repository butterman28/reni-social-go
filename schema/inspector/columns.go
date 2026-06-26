package inspector

import (
	"database/sql"
)



type Column struct {
	Name     string
	Type     string
	Nullable bool
}



func GetColumns(db *sql.DB, schema, table string) ([]Column, error) {
	rows, err := db.Query(`
		SELECT column_name, data_type, is_nullable
		FROM information_schema.columns
		WHERE table_schema=$1 AND table_name=$2
	`, schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var cols []Column
	for rows.Next() {
		var name, datatype, nullable string
		if err := rows.Scan(&name, &datatype, &nullable); err != nil {
			return nil, err
		}
		cols = append(cols, Column{
			Name:     name,
			Type:     datatype,
			Nullable: nullable == "YES",
		})
	}
	return cols, nil
}