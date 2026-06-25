package diff

import (
	"fmt"
	"strings"
	"reni-social/schema/inspector"
	"reni-social/schema/types"
	"log"
)



// func DiffColumns(yamlCols map[string]types.Column, dbCols map[string]inspector.Column) []string {
// 	var changes []string

// 	// 1. Add or modify columns (Check YAML against DB)
// 	for name, col := range yamlCols {
// 		dbCol, exists := dbCols[name]

// 		// Explicit drop requested in YAML
// 		if col.Drop {
// 			if exists {
// 				changes = append(changes, fmt.Sprintf("DROP COLUMN %s", name))
// 			}
// 			continue
// 		}

// 		if !exists {
// 			// Column missing in DB â†’ Create ADD statement
// 			line := fmt.Sprintf("ADD COLUMN %s %s", name, col.Type)
// 			if col.NotNull {
// 				line += " NOT NULL"
// 			}
// 			if col.Default != "" {
// 				line += " DEFAULT " + col.Default
// 			}
// 			changes = append(changes, line)
// 			continue
// 		}

// 		// Normalize types for comparison (e.g., "UUID" vs "uuid")
// 		normalizedDbType := strings.ToLower(dbCol.Type)
// 		normalizedYamlType := strings.ToLower(col.Type)

// 		// Compare type
// 		if normalizedDbType != normalizedYamlType {
// 			changes = append(changes, fmt.Sprintf("ALTER COLUMN %s TYPE %s USING %s::%s", name, col.Type, name, col.Type))
// 		}

// 		// Compare nullability
// 		// dbCol.Nullable is true if the DB allows NULL
// 		// col.NotNull is true if the YAML forbids NULL
// 		if dbCol.Nullable == col.NotNull {
// 			if col.NotNull {
// 				changes = append(changes, fmt.Sprintf("ALTER COLUMN %s SET NOT NULL", name))
// 			} else {
// 				changes = append(changes, fmt.Sprintf("ALTER COLUMN %s DROP NOT NULL", name))
// 			}
// 		}
// 	}

// 	// 2. Check for extra columns in DB that are NOT in YAML (Cleanup)
// 	// This identifies columns like 'org_id' that exist in DB but aren't in your YAML
// 	for dbName := range dbCols {
// 		if _, exists := yamlCols[dbName]; !exists {
// 			changes = append(changes, fmt.Sprintf("DROP COLUMN %s", dbName))
// 		}
// 	}

// 	return changes
// }




func DiffColumns(yamlCols map[string]types.Column, dbCols map[string]inspector.Column) []string {
	var changes []string

	// 1. Process YAML definitions against the Database
	for name, col := range yamlCols {
		dbCol, exists := dbCols[name]

		// --- THE DROP TRIGGER ---
		// Only drop if 'drop: true' is explicitly set in the YAML
		if col.Drop {
			if exists {
				log.Printf("ðŸ§¨ [DANGER] Explicit drop requested for column: %s", name)
				changes = append(changes, fmt.Sprintf("DROP COLUMN %s", name))
			}
			continue // Move to the next column; don't try to add/modify a dropped column
		}

		// Handle New Columns
		if !exists {
			line := fmt.Sprintf("ADD COLUMN %s %s", name, col.Type)
			if col.NotNull {
				line += " NOT NULL"
			}
			if col.Default != "" {
				line += " DEFAULT " + col.Default
			}
			changes = append(changes, line)
			continue
		}

		// Handle Existing Column Modifications
		normalizedDbType := strings.ToLower(dbCol.Type)
		normalizedYamlType := strings.ToLower(col.Type)

		if normalizedDbType != normalizedYamlType {
			changes = append(changes, fmt.Sprintf("ALTER COLUMN %s TYPE %s USING %s::%s", name, col.Type, name, col.Type))
		}

		if dbCol.Nullable == col.NotNull {
			if col.NotNull {
				changes = append(changes, fmt.Sprintf("ALTER COLUMN %s SET NOT NULL", name))
			} else {
				changes = append(changes, fmt.Sprintf("ALTER COLUMN %s DROP NOT NULL", name))
			}
		}
	}

	// 2. SAFETY CHECK: Check for "Ghost Columns"
	// These are columns that exist in the DB but are NOT in the YAML.
	// We no longer drop them automatically. We just warn you.
	for dbName := range dbCols {
		if _, exists := yamlCols[dbName]; !exists {
			// This column is in the DB but you removed the line from your YAML.
			// We do nothing, preserving your data.
			log.Printf("ðŸ’¡ [INFO] Ghost column detected: '%s'. To delete this, add it back to YAML with 'drop: true'.", dbName)
		}
	}

	return changes
}