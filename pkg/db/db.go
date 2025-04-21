package db

import (
	"database/sql"
	"fmt"
	"log"
	"regexp"
	"strings"

	_ "github.com/lib/pq" // PostgreSQL driver
)

// DB is the database connection
var DB *sql.DB

// InitDB initializes the database connection
func InitDB(connectionString string) error {
	var err error
	DB, err = sql.Open("postgres", connectionString)
	if err != nil {
		return err
	}

	// Test connection
	err = DB.Ping()
	if err != nil {
		return err
	}

	log.Println("Successfully connected to the database")
	return nil
}

// GetTables returns a list of all tables in the database
func GetTables() ([]string, error) {
	rows, err := DB.Query(`
		SELECT table_name 
		FROM information_schema.tables 
		WHERE table_schema = 'public'
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var tableName string
		if err := rows.Scan(&tableName); err != nil {
			return nil, err
		}
		tables = append(tables, tableName)
	}

	return tables, nil
}

// GetTableColumns returns the columns of a table
func GetTableColumns(tableName string) ([]map[string]interface{}, error) {
	// Get all column information with single SHOW COLUMNS WITH COMMENT command
	showColumnsQuery := fmt.Sprintf("SHOW COLUMNS FROM %s WITH COMMENT", tableName)
	rows, err := DB.Query(showColumnsQuery)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Get column names to handle all returned fields
	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	var result []map[string]interface{}

	// Prepare containers for the returned values
	values := make([]interface{}, len(columns))
	valuePtrs := make([]interface{}, len(columns))

	for rows.Next() {
		// Initialize pointer slice
		for i := range columns {
			valuePtrs[i] = &values[i]
		}

		// Scan row data
		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, err
		}

		// Create column info map
		colInfo := make(map[string]interface{})
		for i, col := range columns {
			val := values[i]

			// Handle different data types
			switch v := val.(type) {
			case []byte:
				colInfo[col] = string(v)
			default:
				colInfo[col] = v
			}
		}

		result = append(result, colInfo)
	}

	return result, nil
}

// ClassifyQuery checks if a query is a read or write operation
// Returns true for write operations, false for read operations
func ClassifyQuery(query string) (bool, string) {
	// Trim and convert to lowercase
	trimmedQuery := strings.TrimSpace(strings.ToLower(query))

	// Define write operation patterns (DML and DDL)
	writePatterns := map[string]*regexp.Regexp{
		// DML operations
		"INSERT": regexp.MustCompile(`^insert\s`),
		"UPDATE": regexp.MustCompile(`^update\s`),
		"DELETE": regexp.MustCompile(`^delete\s`),
		// DDL operations
		"DROP":     regexp.MustCompile(`^drop\s`),
		"CREATE":   regexp.MustCompile(`^create\s`),
		"ALTER":    regexp.MustCompile(`^alter\s`),
		"TRUNCATE": regexp.MustCompile(`^truncate\s`),
		"GRANT":    regexp.MustCompile(`^grant\s`),
		"REVOKE":   regexp.MustCompile(`^revoke\s`),
	}

	// Check if the query matches any write operation pattern
	for opName, pattern := range writePatterns {
		if pattern.MatchString(trimmedQuery) {
			return true, opName // true means it's a write operation
		}
	}

	// If no write operation pattern matches, it's a read operation
	return false, ""
}

// ExecuteQuery executes a read-only SQL query
func ExecuteQuery(query string) ([]map[string]interface{}, error) {
	// Check if it's a write operation
	isWrite, opName := ClassifyQuery(query)
	if isWrite {
		return nil, fmt.Errorf("write operation not allowed in read-query: %s", opName)
	}

	rows, err := DB.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Get column names
	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	// Prepare result set
	var result []map[string]interface{}
	values := make([]interface{}, len(columns))
	valuePtrs := make([]interface{}, len(columns))

	for rows.Next() {
		// Initialize pointer slice
		for i := range columns {
			valuePtrs[i] = &values[i]
		}

		// Scan row data
		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, err
		}

		// Create row map
		row := make(map[string]interface{})
		for i, col := range columns {
			val := values[i]

			// Handle different data types
			switch v := val.(type) {
			case []byte:
				row[col] = string(v)
			default:
				row[col] = v
			}
		}
		result = append(result, row)
	}

	return result, nil
}

// ExecuteWriteQuery executes a write operation (DML or DDL)
func ExecuteWriteQuery(query string) (int64, error) {
	// Check if it's a write operation
	isWrite, _ := ClassifyQuery(query)
	if !isWrite {
		return 0, fmt.Errorf("not a write operation: expected INSERT, UPDATE, DELETE, CREATE, DROP, ALTER, etc")
	}

	// Execute write operation
	result, err := DB.Exec(query)
	if err != nil {
		return 0, err
	}

	// Get affected rows
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}

	return rowsAffected, nil
}

// Close closes the database connection
func Close() {
	if DB != nil {
		DB.Close()
	}
}

// DatabaseInfo represents information about a database
type DatabaseInfo struct {
	Name       string                 `json:"name"`
	Version    string                 `json:"version"`
	EngineType string                 `json:"engine_type"`
	Comment    string                 `json:"comment"`
	Properties map[string]interface{} `json:"properties"`
}

// GetDatabaseInfoByName retrieves information about a specific database
func GetDatabaseInfoByName(dbName string) (DatabaseInfo, error) {
	// Query database version
	var version string
	err := DB.QueryRow("SELECT version()").Scan(&version)
	if err != nil {
		return DatabaseInfo{}, fmt.Errorf("failed to get database version: %v", err)
	}

	// Query database engine type and comment using SHOW DATABASES
	var engineType string
	comment := "" // Initialize comment as empty string

	// Use SHOW DATABASES to get engine type
	showDBQuery := `SHOW DATABASES`
	rows, err := DB.Query(showDBQuery)
	if err != nil {
		// If error, set default values but return the database name as provided
		engineType = "KaiwuDB"
		// Log the error but continue
		fmt.Printf("Warning: Failed to execute SHOW DATABASES: %v\n", err)
	} else {
		defer rows.Close()

		// Get column names to handle different column orders
		columns, err := rows.Columns()
		if err != nil {
			fmt.Printf("Warning: Failed to get column names: %v\n", err)
			engineType = "KaiwuDB"
		} else {
			// Debug column names
			fmt.Printf("SHOW DATABASES columns: %v\n", columns)

			// Find the index of database_name and engine_type columns
			dbNameIdx := -1
			engineTypeIdx := -1
			for i, col := range columns {
				if strings.ToLower(col) == "database_name" {
					dbNameIdx = i
				} else if strings.ToLower(col) == "engine_type" {
					engineTypeIdx = i
				}
			}

			if dbNameIdx == -1 || engineTypeIdx == -1 {
				fmt.Printf("Warning: Could not find database_name or engine_type columns in SHOW DATABASES result\n")
				engineType = "KaiwuDB"
			} else {
				// Scan through results to find the matching database
				found := false
				for rows.Next() {
					// Create a slice to hold all column values
					values := make([]interface{}, len(columns))
					valuePtrs := make([]interface{}, len(columns))
					for i := range values {
						valuePtrs[i] = &values[i]
					}

					if err := rows.Scan(valuePtrs...); err != nil {
						fmt.Printf("Warning: Failed to scan database row: %v\n", err)
						continue
					}

					// Extract database name and engine type from the scanned values
					var dbNameResult, engineTypeResult string
					if dbVal := values[dbNameIdx]; dbVal != nil {
						if byteVal, ok := dbVal.([]byte); ok {
							dbNameResult = string(byteVal)
						} else if strVal, ok := dbVal.(string); ok {
							dbNameResult = strVal
						}
					}

					if engineVal := values[engineTypeIdx]; engineVal != nil {
						if byteVal, ok := engineVal.([]byte); ok {
							engineTypeResult = string(byteVal)
						} else if strVal, ok := engineVal.(string); ok {
							engineTypeResult = strVal
						}
					}

					// Debug values
					fmt.Printf("Database: %s, Engine: %s\n", dbNameResult, engineTypeResult)

					if dbNameResult == dbName {
						engineType = engineTypeResult
						found = true
						break
					}
				}

				if !found {
					engineType = "KaiwuDB"
					fmt.Printf("Warning: Database %s not found in SHOW DATABASES results\n", dbName)
				}
			}
		}
	}

	// Get additional properties
	properties := make(map[string]interface{})

	// Add database encoding
	var encoding string
	encodingQuery := `
		SELECT pg_encoding_to_char(encoding) 
		FROM pg_database 
		WHERE datname = $1
	`
	if err := DB.QueryRow(encodingQuery, dbName).Scan(&encoding); err == nil {
		properties["encoding"] = encoding
	}

	// Add database owner
	var owner string
	ownerQuery := `
		SELECT pg_catalog.pg_get_userbyid(d.datdba) as owner
		FROM pg_catalog.pg_database d
		WHERE d.datname = $1
	`
	if err := DB.QueryRow(ownerQuery, dbName).Scan(&owner); err == nil {
		properties["owner"] = owner
	}

	// Add database creation time information
	var creationTime string
	timeQuery := `
		SELECT MIN(mod_time) as creation_time
		FROM kwdb_internal.tables
		WHERE database_name = $1
	`
	if err := DB.QueryRow(timeQuery, dbName).Scan(&creationTime); err == nil {
		properties["creation_time"] = creationTime
	} else {
		// Fallback to empty value if query fails
		properties["creation_time"] = ""
	}

	// Return database info
	return DatabaseInfo{
		Name:       dbName,
		Version:    version,
		EngineType: engineType,
		Comment:    comment,
		Properties: properties,
	}, nil
}

// GetTableExampleQueries generates example queries for a specific table
func GetTableExampleQueries(tableName string) (map[string][]string, error) {
	// Verify table exists
	var exists bool
	err := DB.QueryRow("SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = $1)", tableName).Scan(&exists)
	if err != nil {
		return nil, fmt.Errorf("failed to check if table exists: %v", err)
	}
	if !exists {
		return nil, fmt.Errorf("table %s does not exist", tableName)
	}

	// Get table columns
	columns, err := GetTableColumns(tableName)
	if err != nil {
		return nil, fmt.Errorf("failed to get table columns: %v", err)
	}

	// Generate example queries
	examples := map[string][]string{
		"read":  generateReadExamples(tableName, columns),
		"write": generateWriteExamples(tableName, columns),
	}

	return examples, nil
}

// generateReadExamples generates example read queries for a table
func generateReadExamples(tableName string, columns []map[string]interface{}) []string {
	examples := []string{}

	// Basic SELECT
	examples = append(examples, fmt.Sprintf("SELECT * FROM %s LIMIT 10;", tableName))

	// SELECT with specific columns
	if len(columns) > 0 {
		columnNames := []string{}
		for _, col := range columns {
			if name, ok := col["column_name"].(string); ok {
				columnNames = append(columnNames, name)
				if len(columnNames) >= 3 {
					break
				}
			}
		}
		if len(columnNames) > 0 {
			examples = append(examples, fmt.Sprintf("SELECT %s FROM %s LIMIT 10;", strings.Join(columnNames, ", "), tableName))
		}
	}

	// SELECT with WHERE clause
	if len(columns) > 0 {
		for _, col := range columns {
			if name, ok := col["column_name"].(string); ok {
				dataType, _ := col["data_type"].(string)
				if strings.Contains(strings.ToLower(dataType), "int") {
					examples = append(examples, fmt.Sprintf("SELECT * FROM %s WHERE %s > 0 LIMIT 10;", tableName, name))
					break
				} else if strings.Contains(strings.ToLower(dataType), "char") || strings.Contains(strings.ToLower(dataType), "text") {
					examples = append(examples, fmt.Sprintf("SELECT * FROM %s WHERE %s LIKE 'A%%' LIMIT 10;", tableName, name))
					break
				} else if strings.Contains(strings.ToLower(dataType), "date") || strings.Contains(strings.ToLower(dataType), "time") {
					examples = append(examples, fmt.Sprintf("SELECT * FROM %s WHERE %s > NOW() - INTERVAL '1 month' LIMIT 10;", tableName, name))
					break
				}
			}
		}
	}

	// SELECT with ORDER BY
	if len(columns) > 0 {
		for _, col := range columns {
			if name, ok := col["column_name"].(string); ok {
				examples = append(examples, fmt.Sprintf("SELECT * FROM %s ORDER BY %s DESC LIMIT 10;", tableName, name))
				break
			}
		}
	}

	// SELECT with GROUP BY and aggregation
	if len(columns) > 1 {
		var groupByCol, aggregateCol string
		for _, col := range columns {
			if name, ok := col["column_name"].(string); ok {
				dataType, _ := col["data_type"].(string)
				if groupByCol == "" && (strings.Contains(strings.ToLower(dataType), "char") || strings.Contains(strings.ToLower(dataType), "text")) {
					groupByCol = name
				} else if aggregateCol == "" && strings.Contains(strings.ToLower(dataType), "int") {
					aggregateCol = name
				}
				if groupByCol != "" && aggregateCol != "" {
					break
				}
			}
		}
		if groupByCol != "" && aggregateCol != "" {
			examples = append(examples, fmt.Sprintf("SELECT %s, COUNT(*), AVG(%s) FROM %s GROUP BY %s LIMIT 10;", groupByCol, aggregateCol, tableName, groupByCol))
		}
	}

	return examples
}

// generateWriteExamples generates example write queries for a table
func generateWriteExamples(tableName string, columns []map[string]interface{}) []string {
	examples := []string{}

	// INSERT example
	if len(columns) > 0 {
		columnNames := []string{}
		valueTemplates := []string{}
		for _, col := range columns {
			if name, ok := col["column_name"].(string); ok {
				// Skip auto-increment columns
				if isIdentity, ok := col["is_identity"].(bool); ok && isIdentity {
					continue
				}
				// Skip generated columns
				if isGenerated, ok := col["is_generated"].(bool); ok && isGenerated {
					continue
				}

				columnNames = append(columnNames, name)
				dataType, _ := col["data_type"].(string)
				if strings.Contains(strings.ToLower(dataType), "int") {
					valueTemplates = append(valueTemplates, "42")
				} else if strings.Contains(strings.ToLower(dataType), "char") || strings.Contains(strings.ToLower(dataType), "text") {
					valueTemplates = append(valueTemplates, "'example_value'")
				} else if strings.Contains(strings.ToLower(dataType), "date") || strings.Contains(strings.ToLower(dataType), "time") {
					valueTemplates = append(valueTemplates, "NOW()")
				} else if strings.Contains(strings.ToLower(dataType), "bool") {
					valueTemplates = append(valueTemplates, "true")
				} else if strings.Contains(strings.ToLower(dataType), "numeric") || strings.Contains(strings.ToLower(dataType), "decimal") {
					valueTemplates = append(valueTemplates, "123.45")
				} else {
					valueTemplates = append(valueTemplates, "NULL")
				}
			}
		}
		if len(columnNames) > 0 {
			examples = append(examples, fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s);", tableName, strings.Join(columnNames, ", "), strings.Join(valueTemplates, ", ")))
		}
	}

	// UPDATE example
	if len(columns) > 0 {
		var updateCol, whereCol string
		for _, col := range columns {
			if name, ok := col["column_name"].(string); ok {
				dataType, _ := col["data_type"].(string)
				if updateCol == "" && (strings.Contains(strings.ToLower(dataType), "char") || strings.Contains(strings.ToLower(dataType), "text")) {
					updateCol = name
				} else if whereCol == "" && (strings.Contains(strings.ToLower(dataType), "int") || name == "id" || strings.HasSuffix(name, "_id")) {
					whereCol = name
				}
				if updateCol != "" && whereCol != "" {
					break
				}
			}
		}
		if updateCol != "" {
			if whereCol != "" {
				examples = append(examples, fmt.Sprintf("UPDATE %s SET %s = 'new_value' WHERE %s = 1;", tableName, updateCol, whereCol))
			} else {
				examples = append(examples, fmt.Sprintf("UPDATE %s SET %s = 'new_value' LIMIT 1;", tableName, updateCol))
			}
		}
	}

	// DELETE example
	if len(columns) > 0 {
		var whereCol string
		for _, col := range columns {
			if name, ok := col["column_name"].(string); ok {
				if name == "id" || strings.HasSuffix(name, "_id") {
					whereCol = name
					break
				}
				dataType, _ := col["data_type"].(string)
				if strings.Contains(strings.ToLower(dataType), "int") {
					whereCol = name
					break
				}
			}
		}
		if whereCol != "" {
			examples = append(examples, fmt.Sprintf("DELETE FROM %s WHERE %s = 1;", tableName, whereCol))
		} else {
			examples = append(examples, fmt.Sprintf("DELETE FROM %s WHERE false; -- Add your condition here", tableName))
		}
	}

	return examples
}

// GetTablesForDatabase returns a list of all tables in the specified database
func GetTablesForDatabase(databaseName string) ([]string, error) {
	// Try different query approaches to get tables for the specified database

	// First approach: Using information_schema with table_catalog filter
	query := `
		SELECT table_name 
		FROM information_schema.tables 
		WHERE table_schema = 'public' 
		AND table_catalog = $1
		AND table_type = 'BASE TABLE'
		ORDER BY table_name
	`

	rows, err := DB.Query(query, databaseName)
	if err != nil {
		// If the first approach fails, try an alternative approach
		fmt.Printf("Warning: First approach to get tables for database %s failed: %v\n", databaseName, err)

		// Second approach: Try to use pg_tables
		query = `
			SELECT tablename 
			FROM pg_catalog.pg_tables 
			WHERE schemaname = 'public'
			ORDER BY tablename
		`

		// If we're querying the current database, we can use this query directly
		if databaseName == getCurrentDatabase() {
			rows, err = DB.Query(query)
			if err != nil {
				return nil, fmt.Errorf("failed to get tables for database %s: %v", databaseName, err)
			}
		} else {
			// For other databases, we might want to
			// create a temporary connection to the target database
			return []string{}, fmt.Errorf("cannot list tables for database %s: not connected to this database", databaseName)
		}
	}

	defer rows.Close()

	var tables []string
	for rows.Next() {
		var tableName string
		if err := rows.Scan(&tableName); err != nil {
			return nil, err
		}
		tables = append(tables, tableName)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating table rows: %v", err)
	}

	return tables, nil
}

// getCurrentDatabase returns the name of the current database
func getCurrentDatabase() string {
	var dbName string
	err := DB.QueryRow("SELECT current_database()").Scan(&dbName)
	if err != nil {
		fmt.Printf("Warning: Failed to get current database name: %v\n", err)
		return ""
	}
	return dbName
}

// GetCurrentDatabase returns the name of the current database (exported version)
func GetCurrentDatabase() string {
	return getCurrentDatabase()
}

// GetDatabases returns a list of all databases in the KaiwuDB instance
func GetDatabases() ([]string, error) {
	// Query to list all databases
	rows, err := DB.Query(`
		SELECT datname 
		FROM pg_database 
		WHERE datistemplate = false
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to list databases: %v", err)
	}
	defer rows.Close()

	var databases []string
	for rows.Next() {
		var dbName string
		if err := rows.Scan(&dbName); err != nil {
			return nil, fmt.Errorf("failed to scan database name: %v", err)
		}
		databases = append(databases, dbName)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating database rows: %v", err)
	}

	return databases, nil
}

// ProductInfo represents information about the KWDB product
type ProductInfo struct {
	ProductName string                 `json:"product_name"`
	Description string                 `json:"description"`
	Features    []string               `json:"features"`
	VersionInfo map[string]interface{} `json:"version_info"`
}

// GetProductInfo returns information about the KWDB product
func GetProductInfo() (ProductInfo, error) {
	// Query database version
	var versionStr string
	err := DB.QueryRow("SELECT version()").Scan(&versionStr)
	if err != nil {
		return ProductInfo{}, fmt.Errorf("failed to get database version: %v", err)
	}

	// Parse version string
	// Example: "KaiwuDB 2.1.1 (x86_64-linux-gnu, built 2024/12/04 07:44:35, go1.16.15, gcc 13.2.0)"
	version := "unknown"
	buildDate := "unknown"
	platform := "unknown"

	// Extract version number
	if strings.Contains(versionStr, "KaiwuDB") {
		parts := strings.Split(versionStr, " ")
		if len(parts) >= 2 {
			version = parts[1]
		}
	}

	// Extract build date if available
	if strings.Contains(versionStr, "built") {
		buildInfo := versionStr
		startIdx := strings.Index(buildInfo, "built")
		if startIdx > 0 {
			buildDateStr := buildInfo[startIdx+6:]
			endIdx := strings.Index(buildDateStr, ",")
			if endIdx > 0 {
				buildDate = strings.TrimSpace(buildDateStr[:endIdx])
			}
		}
	}

	// Extract platform if available
	if strings.Contains(versionStr, "(") && strings.Contains(versionStr, ")") {
		startIdx := strings.Index(versionStr, "(")
		endIdx := strings.Index(versionStr, ",")
		if startIdx > 0 && endIdx > startIdx {
			platform = strings.TrimSpace(versionStr[startIdx+1 : endIdx])
		}
	}

	// Construct version info
	versionInfo := map[string]interface{}{
		"version":      version,
		"full_version": versionStr,
		"build_date":   buildDate,
		"platform":     platform,
		"api_version":  "v1",
	}

	// Construct product info
	productInfo := ProductInfo{
		ProductName: "KWDB (KaiwuDB)",
		Description: "Time-series database with advanced analytics capabilities",
		Features: []string{
			"Time-series data storage",
			"SQL query support",
			"High-performance analytics",
			"Scalable architecture",
		},
		VersionInfo: versionInfo,
	}

	return productInfo, nil
}

// GetTableMetadata returns the metadata of a table, including indexes, primary key, and table type
func GetTableMetadata(tableName string) (map[string]interface{}, error) {
	metadata := make(map[string]interface{})

	// Get table type and storage engine using SHOW CREATE TABLE
	var createTableSQL string
	query := fmt.Sprintf("SHOW CREATE TABLE %s", tableName)

	rows, err := DB.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to get CREATE TABLE statement: %v", err)
	}
	defer rows.Close()

	// Get column names
	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("failed to get column names: %v", err)
	}

	// Find create_statement column index
	createColumnIndex := -1
	for i, col := range columns {
		if strings.ToLower(col) == "create_statement" {
			createColumnIndex = i
			break
		}
	}

	if createColumnIndex == -1 {
		return nil, fmt.Errorf("create_statement column not found in SHOW CREATE TABLE result")
	}

	// Scan row data
	if rows.Next() {
		// Initialize values slice for scanning
		values := make([]interface{}, len(columns))
		valuePtrs := make([]interface{}, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		// Scan row
		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, fmt.Errorf("failed to scan SHOW CREATE TABLE row: %v", err)
		}

		// Extract create statement
		val := values[createColumnIndex]
		if byteVal, ok := val.([]byte); ok {
			createTableSQL = string(byteVal)
		} else if strVal, ok := val.(string); ok {
			createTableSQL = strVal
		} else {
			return nil, fmt.Errorf("unexpected type for create_statement column")
		}
	} else {
		return nil, fmt.Errorf("no rows returned by SHOW CREATE TABLE %s", tableName)
	}

	// Determine table type using SHOW TABLES
	var tableType string

	// 获取所有表的类型信息
	tableTypeQuery := `SHOW TABLES`
	rows, err = DB.Query(tableTypeQuery)
	if err != nil {
		fmt.Printf("Warning: Failed to get table types from SHOW TABLES: %v\n", err)
		// 如果无法从SHOW TABLES获取，尝试从CREATE TABLE语句推断
		if strings.Contains(createTableSQL, "TAGS") || strings.Contains(createTableSQL, "TIME SERIES") {
			tableType = "TIME SERIES TABLE"
		} else {
			tableType = "BASE TABLE"
		}
	} else {
		defer rows.Close()
		// 遍历结果找到目标表
		found := false
		var currentTable, currentType string
		for rows.Next() {
			if err := rows.Scan(&currentTable, &currentType); err != nil {
				fmt.Printf("Warning: Error scanning table type: %v\n", err)
				continue
			}
			if currentTable == tableName {
				tableType = currentType
				found = true
				break
			}
		}
		if !found {
			// 如果在SHOW TABLES结果中没找到，使用默认推断
			if strings.Contains(createTableSQL, "TAGS") || strings.Contains(createTableSQL, "TIME SERIES") {
				tableType = "TIME SERIES TABLE"
			} else {
				tableType = "BASE TABLE"
			}
		}
	}

	metadata["table_type"] = tableType

	// Extract partition information if available
	partitionInfo := extractPartitionInfo(createTableSQL)
	if partitionInfo != nil {
		metadata["partition_info"] = partitionInfo
	}

	// Get indexes and primary key
	indexes, primaryKey, err := GetTableIndexes(tableName, createTableSQL, tableType)
	if err != nil {
		fmt.Printf("Warning: Failed to get indexes for table %s: %v\n", tableName, err)
	} else {
		metadata["indexes"] = indexes
		if len(primaryKey) > 0 {
			metadata["primary_key"] = primaryKey
		}
	}

	return metadata, nil
}

// GetTableIndexes retrieves all indexes including primary key for a table
func GetTableIndexes(tableName string, createTableSQL string, tableType string) ([]map[string]interface{}, []string, error) {
	var indexes []map[string]interface{}
	var primaryKeyColumns []string
	var err error

	// If we already have the CREATE TABLE statement, use it directly
	if createTableSQL != "" {
		indexes, primaryKeyColumns, err = getTableIndexesFromCreateSQL(tableName, createTableSQL, tableType)
		if err == nil && len(indexes) > 0 {
			return indexes, primaryKeyColumns, nil
		}
	}

	// Otherwise, get CREATE TABLE statement and process
	if createTableSQL == "" {
		// Get the create table statement
		query := fmt.Sprintf("SHOW CREATE TABLE %s", tableName)
		rows, err := DB.Query(query)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to get CREATE TABLE statement: %v", err)
		}
		defer rows.Close()

		// Get column names
		columns, err := rows.Columns()
		if err != nil {
			return nil, nil, fmt.Errorf("failed to get column names: %v", err)
		}

		// Find create_statement column index
		createColumnIndex := -1
		for i, col := range columns {
			if strings.ToLower(col) == "create_statement" {
				createColumnIndex = i
				break
			}
		}

		if createColumnIndex == -1 {
			return nil, nil, fmt.Errorf("create_statement column not found in SHOW CREATE TABLE result")
		}

		// Scan row data
		if rows.Next() {
			// Initialize values slice for scanning
			values := make([]interface{}, len(columns))
			valuePtrs := make([]interface{}, len(columns))
			for i := range values {
				valuePtrs[i] = &values[i]
			}

			// Scan row
			if err := rows.Scan(valuePtrs...); err != nil {
				return nil, nil, fmt.Errorf("failed to scan SHOW CREATE TABLE row: %v", err)
			}

			// Extract create statement
			val := values[createColumnIndex]
			if byteVal, ok := val.([]byte); ok {
				createTableSQL = string(byteVal)
			} else if strVal, ok := val.(string); ok {
				createTableSQL = strVal
			} else {
				return nil, nil, fmt.Errorf("unexpected type for create_statement column")
			}
		} else {
			return nil, nil, fmt.Errorf("no rows returned by SHOW CREATE TABLE %s", tableName)
		}

		// Determine table type if not provided
		if tableType == "" {
			if strings.Contains(createTableSQL, "TAGS") || strings.Contains(createTableSQL, "TIME SERIES") {
				tableType = "TIME SERIES TABLE"
			} else {
				tableType = "BASE TABLE"
			}
		}
	}

	// Process the CREATE TABLE statement
	indexes, primaryKeyColumns, err = getTableIndexesFromCreateSQL(tableName, createTableSQL, tableType)
	if err != nil || len(indexes) == 0 {
		// Fallback to PostgreSQL system tables if the first method fails
		indexes, primaryKeyColumns, err = getTableIndexesFromSystemTables(tableName)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to get indexes using any method: %v", err)
		}
	}

	return indexes, primaryKeyColumns, nil
}

// getTableIndexesFromSystemTables retrieves indexes using PostgreSQL system tables
func getTableIndexesFromSystemTables(tableName string) ([]map[string]interface{}, []string, error) {
	query := `
		SELECT
			i.relname AS index_name,
			a.attname AS column_name,
			ix.indisprimary AS is_primary,
			ix.indisunique AS is_unique,
			am.amname AS index_type
		FROM
			pg_index ix
			JOIN pg_class i ON i.oid = ix.indexrelid
			JOIN pg_class t ON t.oid = ix.indrelid
			JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = ANY(ix.indkey)
			JOIN pg_am am ON am.oid = i.relam
		WHERE
			t.relname = $1
		ORDER BY
			i.relname, a.attnum;
	`
	rows, err := DB.Query(query, tableName)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get indexes from system tables: %v", err)
	}
	defer rows.Close()

	// Map to group columns by index name
	indexMap := make(map[string]map[string]interface{})
	primaryKeyColumns := []string{}

	for rows.Next() {
		var indexName, columnName, indexType string
		var isPrimary, isUnique bool

		if err := rows.Scan(&indexName, &columnName, &isPrimary, &isUnique, &indexType); err != nil {
			return nil, nil, err
		}

		// For primary key indexes
		if isPrimary {
			primaryKeyColumns = append(primaryKeyColumns, columnName)
		}

		// Initialize the index in the map if it doesn't exist
		if _, exists := indexMap[indexName]; !exists {
			indexMap[indexName] = map[string]interface{}{
				"name":    indexName,
				"columns": []string{},
			}
			if isUnique {
				indexMap[indexName]["unique"] = true
			}
		}

		// Add column to the columns array for this index
		columns := indexMap[indexName]["columns"].([]string)
		indexMap[indexName]["columns"] = append(columns, columnName)
	}

	// Convert map to array
	indexes := make([]map[string]interface{}, 0, len(indexMap))
	for _, idx := range indexMap {
		indexes = append(indexes, idx)
	}

	return indexes, primaryKeyColumns, nil
}

// getTableIndexesFromCreateSQL extracts index information from CREATE TABLE statement
func getTableIndexesFromCreateSQL(tableName string, createTableSQL string, tableType string) ([]map[string]interface{}, []string, error) {
	indexes := []map[string]interface{}{}
	primaryKeyColumns := []string{}

	// For time series tables, handle differently
	if tableType == "TIME SERIES TABLE" {
		// Get table columns to identify timestamp column and data types
		tableColumns, err := GetTableColumns(tableName)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to get table columns: %v", err)
		}

		// For time series tables, the first column is the time index (not gtime which is usually the last column)
		if len(tableColumns) > 0 {
			firstColumn, ok := tableColumns[0]["column_name"].(string)
			if ok {
				// Add time index for the first column
				indexes = append(indexes, map[string]interface{}{
					"name":    "time index",
					"columns": []string{firstColumn},
				})
			}
		}

		// Extract PRIMARY TAGS
		primaryTagsRegex := regexp.MustCompile(`PRIMARY\s+TAGS\s*\(\s*([^)]+)\s*\)`)
		primaryTagsMatch := primaryTagsRegex.FindStringSubmatch(createTableSQL)
		if len(primaryTagsMatch) > 1 {
			// Extract tag columns
			tagsStr := primaryTagsMatch[1]
			tags := []string{}
			for _, tag := range strings.Split(tagsStr, ",") {
				tag = strings.TrimSpace(tag)
				tag = strings.Trim(tag, "`\"'")
				tags = append(tags, tag)
			}

			primaryKeyColumns = tags

			// Add primary key index for the tags
			indexes = append(indexes, map[string]interface{}{
				"name":    "primary tag",
				"columns": tags,
			})
		}

		// Extract all tag columns from TAGS section
		tagsRegex := regexp.MustCompile(`TAGS\s*\(\s*(.*?)\s*\)`)
		tagsMatch := tagsRegex.FindStringSubmatch(createTableSQL)
		if len(tagsMatch) > 1 {
			// Parse tag columns from the TAGS(...) section
			tagColumnDefs := tagsMatch[1]

			// Define regex to match column definitions: name type [constraints]
			columnDefRegex := regexp.MustCompile(`\s*(\w+)\s+[^,]+`)
			columnMatches := columnDefRegex.FindAllStringSubmatch(tagColumnDefs, -1)

			for _, match := range columnMatches {
				if len(match) > 1 {
					columnName := match[1]
					// Skip if this is already in the primary key
					found := false
					for _, primaryCol := range primaryKeyColumns {
						if primaryCol == columnName {
							found = true
							break
						}
					}

					if !found {
						// Add individual index for each tag column
						indexes = append(indexes, map[string]interface{}{
							"name":    "tag",
							"columns": []string{columnName},
						})
					}
				}
			}
		}
	} else {
		// Traditional table - extract PRIMARY KEY
		primaryKeyRegex := regexp.MustCompile(`PRIMARY\s+TAGS\s*\(\s*([^)]+)\s*\)`)
		primaryKeyMatch := primaryKeyRegex.FindStringSubmatch(createTableSQL)
		if len(primaryKeyMatch) > 1 {
			// Extract columns from primary key definition
			columnsStr := primaryKeyMatch[1]
			columns := []string{}
			for _, col := range strings.Split(columnsStr, ",") {
				col = strings.TrimSpace(col)
				// Remove backticks or quotes if present
				col = strings.Trim(col, "`\"'")
				columns = append(columns, col)
			}

			primaryKeyColumns = columns

			// Add primary key to indexes
			indexes = append(indexes, map[string]interface{}{
				"name":    "primary key",
				"columns": columns,
			})
		}

		// Extract other indexes (KEY, UNIQUE KEY, INDEX, etc.)
		indexRegex := regexp.MustCompile(`(UNIQUE\s+)?(?:KEY|INDEX)\s+(\S+)\s*\(([^)]+)\)(?:\s+(?:USING\s+(\S+)))?`)
		indexMatches := indexRegex.FindAllStringSubmatch(createTableSQL, -1)

		for _, match := range indexMatches {
			isUnique := match[1] != ""
			indexName := strings.Trim(match[2], "`\"'")
			columnsStr := match[3]

			columns := []string{}
			for _, col := range strings.Split(columnsStr, ",") {
				col = strings.TrimSpace(col)
				// Handle cases like `column`(10) for partial indexes
				if strings.Contains(col, "(") {
					col = col[:strings.Index(col, "(")]
				}
				// Remove backticks or quotes
				col = strings.Trim(col, "`\"'")
				columns = append(columns, col)
			}

			indexInfo := map[string]interface{}{
				"name":    indexName,
				"columns": columns,
			}

			if isUnique {
				indexInfo["unique"] = true
			}

			indexes = append(indexes, indexInfo)
		}
	}

	return indexes, primaryKeyColumns, nil
}

// extractPartitionInfo extracts partition information from CREATE TABLE statement
func extractPartitionInfo(createTableSQL string) map[string]interface{} {
	// Check if table is partitioned
	if !strings.Contains(strings.ToUpper(createTableSQL), "PARTITION BY") {
		return nil
	}

	partitionInfo := make(map[string]interface{})

	// Extract partition type (RANGE, LIST, HASH, etc.)
	partitionTypeRegex := regexp.MustCompile(`PARTITION BY (\w+)\s*\(([^)]+)\)`)
	typeMatch := partitionTypeRegex.FindStringSubmatch(createTableSQL)
	if len(typeMatch) > 2 {
		partitionType := strings.ToUpper(typeMatch[1])
		partitionKey := strings.Trim(typeMatch[2], "`\"' ")

		partitionInfo["type"] = partitionType
		partitionInfo["key"] = partitionKey
	}

	// Extract partition interval for time series data
	if strings.Contains(strings.ToUpper(createTableSQL), "INTERVAL") {
		intervalRegex := regexp.MustCompile(`INTERVAL\s+(['"]?)([^'"]+)(['"]?)`)
		intervalMatch := intervalRegex.FindStringSubmatch(createTableSQL)
		if len(intervalMatch) > 2 {
			partitionInfo["interval"] = intervalMatch[2]
		}
	}

	return partitionInfo
}
