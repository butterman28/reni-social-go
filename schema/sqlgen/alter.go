package sqlgen

import (
	"fmt"
	"strings"
)



// func GenerateAlter(schema, table string, changes []string) string {
// 	if len(changes) == 0 {
// 		return ""
// 	}

// 	var alterCmds []string
// 	var otherCmds []string

// 	for _, c := range changes {
// 		upper := strings.ToUpper(c)
// 		// These are sub-commands that belong inside "ALTER TABLE ..."
// 		if strings.HasPrefix(upper, "ADD COLUMN") ||
// 			strings.HasPrefix(upper, "ALTER COLUMN") ||
// 			strings.HasPrefix(upper, "DROP COLUMN") ||
// 			strings.HasPrefix(upper, "ADD CONSTRAINT") ||
// 			strings.HasPrefix(upper, "DROP CONSTRAINT") {
// 			alterCmds = append(alterCmds, c)
// 		} else {
// 			// These are standalone commands like "CREATE INDEX" or "DROP INDEX"
// 			otherCmds = append(otherCmds, c)
// 		}
// 	}

// 	var sqlParts []string
// 	if len(alterCmds) > 0 {
// 		// This creates ONE clean statement: 
// 		// ALTER TABLE schema.table ADD COLUMN ..., ADD CONSTRAINT ...;
// 		sqlParts = append(sqlParts, fmt.Sprintf("ALTER TABLE %s.%s %s;", schema, table, strings.Join(alterCmds, ", ")))
// 	}

// 	sqlParts = append(sqlParts, otherCmds...)
// 	return strings.Join(sqlParts, "\n")
// }




func GenerateAlter(schema, table string, changes []string) string {
	if len(changes) == 0 {
		return ""
	}

	var alterCmds []string
	var otherCmds []string

	for _, c := range changes {
		upper := strings.ToUpper(c)
		if strings.HasPrefix(upper, "ADD COLUMN") ||
			strings.HasPrefix(upper, "ALTER COLUMN") ||
			strings.HasPrefix(upper, "DROP COLUMN") ||
			strings.HasPrefix(upper, "ADD CONSTRAINT") ||
			strings.HasPrefix(upper, "DROP CONSTRAINT") {
			alterCmds = append(alterCmds, c)
		} else {
			otherCmds = append(otherCmds, c)
		}
	}

	var sqlParts []string
	if len(alterCmds) > 0 {
		// FIX: Wrap %s in double quotes to handle reserved words and dots
		sqlParts = append(sqlParts, fmt.Sprintf("ALTER TABLE \"%s\".\"%s\" %s;", schema, table, strings.Join(alterCmds, ", ")))
	}

	sqlParts = append(sqlParts, otherCmds...)
	return strings.Join(sqlParts, "\n")
}