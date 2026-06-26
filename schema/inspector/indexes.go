package inspector

import (
	"database/sql"
	"strings"
)

type DBIndex struct {
	Name    string
	Columns []string
	Unique  bool
}


// func GetIndexes(db *sql.DB, schema, table string) (map[string]DBIndex, error) {
// 	rows, err := db.Query(`
// 	SELECT indexname, indexdef
// 	FROM pg_indexes
// 	WHERE schemaname=$1 AND tablename=$2
// 	`, schema, table)
// 	if err != nil {
// 		return nil, err
// 	}
// 	defer rows.Close()

// 	indexes := map[string]DBIndex{}

// 	for rows.Next() {
// 		var name, def string
// 		rows.Scan(&name, &def)

// 		// simple parse for columns and unique
// 		unique := false
// 		if len(def) >= 6 && def[:6] == "CREATE" && def[:12] == "CREATE UNIQUE" {
// 			unique = true
// 		}

// 		// extract columns from indexdef using simple parsing (can be enhanced)
// 		start := strings.Index(def, "(")
// 		end := strings.Index(def, ")")
// 		cols := []string{}
// 		if start >= 0 && end > start {
// 			cols = strings.Split(def[start+1:end], ",")
// 			for i := range cols {
// 				cols[i] = strings.TrimSpace(cols[i])
// 			}
// 		}

// 		indexes[name] = DBIndex{
// 			Name:    name,
// 			Columns: cols,
// 			Unique:  unique,
// 		}
// 	}

// 	return indexes, nil
// }



func GetIndexes(db *sql.DB, schema string, table string) (map[string][]string, error) {

	query := `
	SELECT
	    i.relname AS index_name,
	    a.attname AS column_name
	FROM
	    pg_class t
	JOIN pg_index ix ON t.oid = ix.indrelid
	JOIN pg_class i ON i.oid = ix.indexrelid
	JOIN pg_attribute a ON a.attrelid = t.oid
	AND a.attnum = ANY(ix.indkey)
	JOIN pg_namespace n ON n.oid = t.relnamespace
	WHERE
	    n.nspname = $1
	    AND t.relname = $2
	ORDER BY
	    i.relname, a.attnum
	`

	rows, err := db.Query(query, schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	indexes := map[string][]string{}

	for rows.Next() {

		var name string
		var column string

		if err := rows.Scan(&name, &column); err != nil {
			return nil, err
		}

		// Skip primary key indexes
		if strings.HasSuffix(name, "_pkey") {
			continue
		}

		indexes[name] = append(indexes[name], column)
	}

	return indexes, nil
}



// func IndexEqual(a []string, b []string) bool {

// 	if len(a) != len(b) {
// 		return false
// 	}

// 	for i := range a {
// 		if a[i] != b[i] {
// 			return false
// 		}
// 	}

// 	return true
// }

