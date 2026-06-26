// sqlgen/create.go
package sqlgen

import (
	"fmt"
	"strings"
	"reni-social/schema/types"
	// "database/sql"
)





func GenerateCreateTable(schema string, table *types.Table) string {
	var colDefs []string
	var pkCols []string

	for name, col := range table.Columns {
		// 1. Basic type and name - Quote column name for safety
		def := fmt.Sprintf("\"%s\" %s", name, col.Type)

		// 2. Not Null
		if col.NotNull {
			def += " NOT NULL"
		}

		// 3. Default Value
		if col.Default != "" {
			def += " DEFAULT " + col.Default
		}

		// 4. Primary Key tracking
		if col.PrimaryKey {
			pkCols = append(pkCols, fmt.Sprintf("\"%s\"", name))
		}

		// 5. Foreign Key (Inline Reference)
		if col.References.Table != "" {
			refTable := col.References.Table
			
			// If the table name doesn't already have a schema prefix, add and quote it
			if !strings.Contains(refTable, ".") {
				// Result: "template_org"."departments"
				def += fmt.Sprintf(" REFERENCES \"%s\".\"%s\"(\"%s\")", 
					schema, refTable, col.References.Column)
			} else {
				// If it already has a dot, we assume it's external (like public.users)
				def += fmt.Sprintf(" REFERENCES %s(\"%s\")", 
					refTable, col.References.Column)
			}
			
			if col.References.OnDelete != "" {
				def += " ON DELETE " + col.References.OnDelete
			}
		}

		colDefs = append(colDefs, def)
	}

	// 6. Add Primary Key constraint if exists
	if len(pkCols) > 0 {
		colDefs = append(colDefs, fmt.Sprintf("PRIMARY KEY (%s)", strings.Join(pkCols, ", ")))
	}

	// 7. Assemble the final SQL - QUOTE EVERYTHING
	// This turns: CREATE TABLE default.categories
	// Into: CREATE TABLE "default"."categories"
	sql := fmt.Sprintf(
		"CREATE TABLE IF NOT EXISTS \"%s\".\"%s\" (\n  %s\n);",
		schema, 
		table.Table,
		strings.Join(colDefs, ",\n  "),
	)
	
	return sql
}




// GenerateCreateTable returns full CREATE TABLE SQL for a given schema and table

// func GenerateCreateTable(schema string, table *types.Table) string {
// 	var colDefs []string
// 	var pkCols []string

// 	for name, col := range table.Columns {
// 		// 1. Basic type and name
// 		def := fmt.Sprintf("%s %s", name, col.Type)

// 		// 2. Not Null
// 		if col.NotNull {
// 			def += " NOT NULL"
// 		}

// 		// 3. Default Value
// 		if col.Default != "" {
// 			def += " DEFAULT " + col.Default
// 		}

// 		// 4. Primary Key tracking
// 		if col.PrimaryKey {
// 			pkCols = append(pkCols, name)
// 		}

// 		// 5. Foreign Key (Inline Reference)
// 		if col.References.Table != "" {
// 			refTable := col.References.Table
			
// 			// If the table name doesn't already have a schema prefix, add the current schema
// 			if !strings.Contains(refTable, ".") {
// 				refTable = fmt.Sprintf("%s.%s", schema, refTable)
// 			}

// 			def += fmt.Sprintf(" REFERENCES %s(%s)", refTable, col.References.Column)
			
// 			if col.References.OnDelete != "" {
// 				def += " ON DELETE " + col.References.OnDelete
// 			}
// 		}

// 		colDefs = append(colDefs, def)
// 	}

// 	// 6. Add Primary Key constraint if exists
// 	if len(pkCols) > 0 {
// 		colDefs = append(colDefs, fmt.Sprintf("PRIMARY KEY (%s)", strings.Join(pkCols, ", ")))
// 	}

// 	// 7. Assemble the final SQL
// 	sql := fmt.Sprintf(
// 		"CREATE TABLE IF NOT EXISTS %s.%s (\n  %s\n);",
// 		schema, 
// 		table.Table,
// 		strings.Join(colDefs, ",\n  "),
// 	)
	
// 	return sql
// }

