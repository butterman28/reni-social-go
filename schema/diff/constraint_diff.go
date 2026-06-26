package diff

import (
	"fmt"
	"reni-social/schema/types"
)


// func DiffConstraints(yamlCols map[string]types.Column, schema string, table string) []string {

// 	var changes []string

// 	for colName, col := range yamlCols {

// 		if col.References.Table != "" {

// 			sql := fmt.Sprintf(
// 				"ALTER TABLE %s.%s ADD CONSTRAINT fk_%s FOREIGN KEY (%s) REFERENCES %s(%s) ON DELETE %s",
// 				schema,
// 				table,
// 				colName,
// 				colName,
// 				col.References.Table,
// 				col.References.Column,
// 				col.References.OnDelete,
// 			)

// 			changes = append(changes, sql)
// 		}
// 	}

// 	return changes
// }




// func DiffConstraints(yamlCols map[string]types.Column, schema string, table string) []string {
// 	var changes []string

// 	for colName, col := range yamlCols {
// 		if col.References.Table != "" {
// 			// REMOVE the "ALTER TABLE %s.%s" part here
// 			sql := fmt.Sprintf(
// 				"ADD CONSTRAINT fk_%s FOREIGN KEY (%s) REFERENCES %s(%s) ON DELETE %s",
// 				colName,
// 				colName,
// 				col.References.Table,
// 				col.References.Column,
// 				col.References.OnDelete,
// 			)

// 			changes = append(changes, sql)
// 		}
// 	}
// 	return changes
// }




func DiffConstraints(yamlCols map[string]types.Column, dbConstraints map[string]bool, schema string, table string) []string {
	var changes []string

	for colName, col := range yamlCols {
		if col.References.Table != "" {
			constraintName := fmt.Sprintf("fk_%s", colName)

			// Only add if it DOES NOT exist in the database
			if !dbConstraints[constraintName] {
				sql := fmt.Sprintf(
					"ALTER TABLE \"%s\".\"%s\" ADD CONSTRAINT \"%s\" FOREIGN KEY (\"%s\") REFERENCES \"%s\"(\"%s\")",
					schema,
					table,
					constraintName,
					colName,
					col.References.Table,
					col.References.Column,
				)
				
				// Only append ON DELETE if it's explicitly specified
				if col.References.OnDelete != "" {
					sql += fmt.Sprintf(" ON DELETE %s", col.References.OnDelete)
				}
				
				changes = append(changes, sql)
			}
		}
	}
	return changes
}