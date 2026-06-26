package migrator

import (
	"database/sql"
	"fmt"
	"log"
	"path/filepath"
	"reni-social/schema/diff"
	"reni-social/schema/inspector"
	"reni-social/schema/loader"
	"reni-social/schema/sqlgen"
	"reni-social/schema/types"
)

// func SyncTable(db *sql.DB, schema, yamlPath string) error {
// 	// Load YAML table definition
// 	yamlTable, err := loader.LoadTable(yamlPath)
// 	if err != nil {
// 		return fmt.Errorf("failed to load YAML %s: %w", yamlPath, err)
// 	}

// 	// Check if table exists
// 	var exists bool
// 	err = db.QueryRow(`
// 		SELECT EXISTS (
// 			SELECT 1 FROM information_schema.tables
// 			WHERE table_schema=$1 AND table_name=$2
// 		)
// 	`, schema, yamlTable.Table).Scan(&exists)
// 	if err != nil {
// 		return fmt.Errorf("failed to check table existence: %w", err)
// 	}

// 	if !exists {
// 		// Table missing, create it
// 		sqlStmt := sqlgen.GenerateCreateTable(schema, yamlTable)
// 		log.Printf("[SYNC] Creating table %s.%s", schema, yamlTable.Table)
// 		_, err := db.Exec(sqlStmt)
// 		if err != nil {
// 			return fmt.Errorf("failed to create table: %w", err)
// 		}
// 		return nil
// 	}

// 	// 1. Inspect existing table state
// 	dbColsSlice, err := inspector.GetColumns(db, schema, yamlTable.Table)
// 	if err != nil {
// 		return fmt.Errorf("failed to get columns: %w", err)
// 	}

// 	// FIX: Convert slice to map for the diff package
// 	dbColsMap := make(map[string]inspector.Column)
// 	for _, col := range dbColsSlice {
// 		dbColsMap[col.Name] = col
// 	}

// 	dbIndexes, err := inspector.GetIndexes(db, schema, yamlTable.Table)
// 	if err != nil {
// 		return fmt.Errorf("failed to get indexes: %w", err)
// 	}

// 	dbConstraints, err := inspector.GetConstraints(db, schema, yamlTable.Table)
// 	if err != nil {
// 		return fmt.Errorf("failed to get constraints: %w", err)
// 	}

// 	// 2. Compute differences
// 	// colChanges is now []string (e.g., ["ADD COLUMN role varchar", "DROP COLUMN org_id"])
// 	colChanges := diff.DiffColumns(yamlTable.Columns, dbColsMap)

// 	// Note: Ensure DiffIndexes and DiffConstraints also return []string
// 	// or adapt them similarly to match the new string-based approach
// 	idxChanges := diff.DiffIndexes(yamlTable.Indexes, dbIndexes, schema, yamlTable.Table)
// 	fkChanges := diff.DiffConstraints(yamlTable.Columns, dbConstraints, schema, yamlTable.Table)

// 	var allChanges []string
// 	allChanges = append(allChanges, colChanges...)
// 	allChanges = append(allChanges, idxChanges...)
// 	allChanges = append(allChanges, fkChanges...)

// 	// 3. Apply changes if any exist
// 	if len(allChanges) > 0 {
// 		// GenerateAlter now just needs to wrap the strings into an ALTER TABLE statement
// 		sqlStmt := sqlgen.GenerateAlter(schema, yamlTable.Table, allChanges)
// 		log.Printf("[SYNC] Applying changes to %s.%s:\n%s", schema, yamlTable.Table, sqlStmt)
// 		_, err := db.Exec(sqlStmt)
// 		if err != nil {
// 			return fmt.Errorf("failed to apply schema changes: %w", err)
// 		}
// 	} else {
// 		log.Printf("[SYNC] Table %s.%s is up-to-date", schema, yamlTable.Table)
// 	}

// 	return nil
// }

func SyncTable(db *sql.DB, schema, yamlPath string) error {
	yamlTable, err := loader.LoadTable(yamlPath)
	if err != nil {
		return fmt.Errorf("failed to load YAML %s: %w", yamlPath, err)
	}

	// 1. Inspect existing table state IMMEDIATELY
	dbColsSlice, err := inspector.GetColumns(db, schema, yamlTable.Table)
	if err != nil {
		return fmt.Errorf("failed to get columns: %w", err)
	}

	// 2. CASE: Table doesn't exist (Inspector returned nothing)
	if len(dbColsSlice) == 0 {
		sqlStmt := sqlgen.GenerateCreateTable(schema, yamlTable)
		log.Printf("[SYNC] ðŸ†• Creating table %s.%s", schema, yamlTable.Table)
		_, err := db.Exec(sqlStmt)
		return err
	}

	// 3. CASE: Table exists, perform delta
	dbColsMap := make(map[string]inspector.Column)
	for _, col := range dbColsSlice {
		dbColsMap[col.Name] = col
	}

	// Get existing indexes/constraints for the diff
	dbIndexes, _ := inspector.GetIndexes(db, schema, yamlTable.Table)
	dbConstraints, _ := inspector.GetConstraints(db, schema, yamlTable.Table)

	// Compute differences
	colChanges := diff.DiffColumns(yamlTable.Columns, dbColsMap)
	idxChanges := diff.DiffIndexes(yamlTable.Indexes, dbIndexes, schema, yamlTable.Table)
	fkChanges := diff.DiffConstraints(yamlTable.Columns, dbConstraints, schema, yamlTable.Table)

	var allChanges []string
	allChanges = append(allChanges, colChanges...)
	allChanges = append(allChanges, idxChanges...)
	allChanges = append(allChanges, fkChanges...)

	// if len(allChanges) > 0 {
	// 	// IMPORTANT: Wrap in a single ALTER statement
	// 	sqlStmt := sqlgen.GenerateAlter(schema, yamlTable.Table, allChanges)
	// 	log.Printf("[SYNC] ðŸ› ï¸  Applying changes to %s.%s:\n%s", schema, yamlTable.Table, sqlStmt)
	// 	_, err := db.Exec(sqlStmt)
	// 	if err != nil {
	// 		return fmt.Errorf("failed to apply schema changes to %s.%s: %w", schema, yamlTable.Table, err)
	// 	}
	// } else {
	// 	// Log this so you can SEE it working
	// 	log.Printf("[SYNC] âœ¨ %s.%s is already up-to-date", schema, yamlTable.Table)
	// }

	// 3. Apply changes if any exist
	if len(allChanges) > 0 {
		// First, apply column-related ALTERs (ADD, DROP, TYPE changes)
		if len(colChanges) > 0 {
			sqlStmt := sqlgen.GenerateAlter(schema, yamlTable.Table, colChanges)
			log.Printf("[SYNC] ðŸ› ï¸  Applying column changes to %s.%s", schema, yamlTable.Table)
			if _, err := db.Exec(sqlStmt); err != nil {
				return fmt.Errorf("failed column changes: %w", err)
			}
		}

		// Second, apply Index and Foreign Key changes individually
		// These cannot be inside an "ALTER TABLE" block in standard SQL
		standaloneChanges := append(idxChanges, fkChanges...)
		for _, stmt := range standaloneChanges {
			if stmt == "" {
				continue
			}
			log.Printf("[SYNC] ðŸ› ï¸  Executing standalone change: %s", stmt)
			// Add a semicolon to ensure clean execution
			if _, err := db.Exec(stmt + ";"); err != nil {
				return fmt.Errorf("failed standalone change (%s): %w", stmt, err)
			}
		}
	} else {
		log.Printf("[SYNC] âœ¨ %s.%s is already up-to-date", schema, yamlTable.Table)
	}

	return nil
}

// SyncPublicTables syncs all YAML tables in the public schema
func SyncPublicTables(db *sql.DB) error {
	yamlFiles, err := filepath.Glob("./schemas/*.yaml")
	if err != nil {
		return fmt.Errorf("failed to list YAML files: %w", err)
	}

	// Load all tables first so we can build the dependency graph
	type entry struct {
		path  string
		table *types.Table
	}
	entries := make([]entry, 0, len(yamlFiles))
	for _, f := range yamlFiles {
		t, err := loader.LoadTable(f)
		if err != nil {
			return fmt.Errorf("failed to load YAML %s: %w", f, err)
		}
		entries = append(entries, entry{path: f, table: t})
	}

	// Build a name→entry map and a dependency graph (table → set of tables it depends on)
	byName := make(map[string]*entry, len(entries))
	for i := range entries {
		byName[entries[i].table.Table] = &entries[i]
	}

	// Kahn's algorithm for topological sort
	deps := make(map[string]map[string]struct{}, len(entries)) // table → its FK deps
	rdeps := make(map[string]int, len(entries))                // in-degree
	for _, e := range entries {
		name := e.table.Table
		if deps[name] == nil {
			deps[name] = make(map[string]struct{})
		}
		if _, ok := rdeps[name]; !ok {
			rdeps[name] = 0
		}
		for _, col := range e.table.Columns {
			ref := col.References.Table
			if ref == "" || ref == name {
				continue
			}
			if _, known := byName[ref]; !known {
				continue // external / already-existing table — skip
			}
			if _, already := deps[name][ref]; !already {
				deps[name][ref] = struct{}{}
				rdeps[name]++ // name depends on ref → name's in-degree goes up
			}
		}
	}

	// Seed the queue with tables that have no dependencies
	queue := make([]string, 0, len(entries))
	for _, e := range entries {
		if rdeps[e.table.Table] == 0 {
			queue = append(queue, e.table.Table)
		}
	}

	sorted := make([]string, 0, len(entries))
	for len(queue) > 0 {
		// Pop front
		cur := queue[0]
		queue = queue[1:]
		sorted = append(sorted, cur)

		// Reduce in-degree of tables that depend on cur
		for _, e := range entries {
			name := e.table.Table
			if _, hasDep := deps[name][cur]; hasDep {
				delete(deps[name], cur)
				rdeps[name]--
				if rdeps[name] == 0 {
					queue = append(queue, name)
				}
			}
		}
	}

	if len(sorted) != len(entries) {
		return fmt.Errorf("cyclic FK dependency detected among schema files")
	}

	// Sync in dependency order
	for _, name := range sorted {
		e := byName[name]
		if err := SyncTable(db, "public", e.path); err != nil {
			return fmt.Errorf("failed to sync table %s: %w", e.path, err)
		}
	}

	log.Println("[SYNC] Public tables are up-to-date")
	return nil
}

