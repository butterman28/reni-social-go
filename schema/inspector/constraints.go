package inspector



import(
	"database/sql"
)



// GetConstraints returns a map of constraint names for a specific table
func GetConstraints(db *sql.DB, schema, table string) (map[string]bool, error) {
	constraints := make(map[string]bool)
	query := `
		SELECT constraint_name 
		FROM information_schema.table_constraints 
		WHERE table_schema = $1 AND table_name = $2`
	
	rows, err := db.Query(query, schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		constraints[name] = true
	}
	return constraints, nil
}