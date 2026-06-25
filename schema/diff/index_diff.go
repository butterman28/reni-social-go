package diff

import (
	"fmt"
	"strings"
	"reni-social/schema/types"
	// "reni-social/schema/inspector"
)




func DiffIndexes(
	yamlIndexes []types.Index,
	dbIndexes map[string][]string,
	schema string,
	table string,
) []string {

	var queries []string

	for _, idx := range yamlIndexes {

		// Ignore postgres auto primary key index
		if strings.HasSuffix(idx.Name, "_pkey") {
			continue
		}

		existingCols, exists := dbIndexes[idx.Name]

		// Index missing â†’ create
		if !exists {

			sql := fmt.Sprintf(
				`CREATE %s INDEX "%s" ON "%s"."%s" (%s)`,
				ifUnique(idx.Unique),
				idx.Name,
				schema,
				table,
				strings.Join(idx.Columns, ", "),
			)

			queries = append(queries, sql)
			continue
		}

		// Index exists but structure changed
		// if !IndexEqual(existingCols, idx.Columns) {

		// 	queries = append(queries,
		// 		fmt.Sprintf("DROP INDEX %s.%s", schema, idx.Name),
		// 	)

		// 	queries = append(queries,
		// 		fmt.Sprintf(
		// 			"CREATE %s INDEX %s ON %s.%s (%s)",
		// 			ifUnique(idx.Unique),
		// 			idx.Name,
		// 			schema,
		// 			table,
		// 			strings.Join(idx.Columns, ", "),
		// 		),
		// 	)
		// }

		// Index exists but structure changed
		if !IndexEqual(existingCols, idx.Columns) {
			// Added quoting for safety
			queries = append(queries,
				fmt.Sprintf(`DROP INDEX IF EXISTS "%s"."%s"`, schema, idx.Name),
			)

			queries = append(queries,
				fmt.Sprintf(
					`CREATE %s INDEX "%s" ON "%s"."%s" (%s)`,
					ifUnique(idx.Unique),
					idx.Name,
					schema,
					table,
					strings.Join(idx.Columns, ", "),
				),
			)
		}
	}

	return queries
}




// Helper: compare slices of columns
func equalColumns(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if strings.TrimSpace(a[i]) != strings.TrimSpace(b[i]) {
			return false
		}
	}
	return true
}