package db

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
)

// TestInitDB tests the database initialization
func TestInitDB(t *testing.T) {
	// Test with valid connection string
	err := InitDB("postgresql://kwdb:Kaiwudb%40123@localhost:26257/db_shig")
	if err != nil {
		t.Fatalf("InitDB failed with valid connection string: %v", err)
	}
	defer Close()

	// Verify connection pool is initialized
	poolMgr := GetPoolManager()
	if !poolMgr.IsInitialized() {
		t.Fatal("Connection pool not initialized after successful initialization")
	}

	err = poolMgr.ExecuteWithConnection(context.Background(), func(db *sql.DB) error {
		return db.Ping()
	})
	if err != nil {
		t.Fatalf("DB ping failed after initialization: %v", err)
	}

	// Test with invalid connection string - should succeed in initialization but fail on actual use
	err = InitDB("postgresql://invalid:invalid@localhost:26257/nonexistent")
	if err != nil {
		t.Fatalf("InitDB should succeed with invalid connection string (lazy loading): %v", err)
	}

	// But should fail when actually trying to use the connection
	err = GetPoolManager().ExecuteWithConnection(context.Background(), func(db *sql.DB) error {
		return db.Ping()
	})
	if err == nil {
		t.Fatal("Connection should fail with invalid connection string when actually used")
	}
}

// TestGetTables tests retrieving tables from the database
func TestGetTables(t *testing.T) {
	// Setup
	err := InitDB("postgresql://kwdb:Kaiwudb%40123@localhost:26257/db_shig")
	if err != nil {
		t.Fatalf("Failed to initialize DB for test: %v", err)
	}
	defer Close()

	// Test
	tables, err := GetTables()
	if err != nil {
		t.Fatalf("GetTables failed: %v", err)
	}

	// Verify
	if tables == nil {
		t.Fatal("GetTables returned nil")
	}
}

// TestGetDatabases tests retrieving databases
func TestGetDatabases(t *testing.T) {
	// Setup
	err := InitDB("postgresql://kwdb:Kaiwudb%40123@localhost:26257/db_shig")
	if err != nil {
		t.Fatalf("Failed to initialize DB for test: %v", err)
	}
	defer Close()

	// Test
	databases, err := GetDatabases()
	if err != nil {
		t.Fatalf("GetDatabases failed: %v", err)
	}

	// Verify
	if databases == nil {
		t.Fatal("GetDatabases returned nil")
	}

	// Should contain at least the current database
	found := false
	for _, db := range databases {
		if db == "db_shig" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("Current database not found in GetDatabases result")
	}
}

// TestExecuteQuery tests executing a query
func TestExecuteQuery(t *testing.T) {
	// Setup
	err := InitDB("postgresql://kwdb:Kaiwudb%40123@localhost:26257/db_shig")
	if err != nil {
		t.Fatalf("Failed to initialize DB for test: %v", err)
	}
	defer Close()

	// Test valid SELECT query
	result, err := ExecuteQuery("SELECT 1 as test")
	if err != nil {
		t.Fatalf("ExecuteQuery failed with valid query: %v", err)
	}

	// Verify result
	if result == nil {
		t.Fatal("ExecuteQuery returned nil result")
	}

	// Check result structure
	if len(result) != 1 {
		t.Fatalf("Expected 1 row, got %d", len(result))
	}

	// Check for test column existence
	if val, ok := result[0]["test"]; !ok {
		t.Fatalf("Expected column 'test' in result, got %v", result[0])
	} else {
		// Log the actual value and type for debugging
		t.Logf("Value: %v, Type: %T", val, val)

		// Convert to string for comparison if needed
		valStr := fmt.Sprintf("%v", val)
		if valStr != "1" {
			t.Fatalf("Expected result[0]['test'] value to be '1', got '%s'", valStr)
		}
	}

	// Test invalid query
	_, err = ExecuteQuery("INVALID SQL")
	if err == nil {
		t.Fatal("ExecuteQuery should fail with invalid SQL")
	}
}

// TestGetTableColumns tests retrieving columns for a table
func TestGetTableColumns(t *testing.T) {
	// Setup
	err := InitDB("postgresql://kwdb:Kaiwudb%40123@localhost:26257/db_shig")
	if err != nil {
		t.Fatalf("Failed to initialize DB for test: %v", err)
	}
	defer Close()

	// Create a test table
	err = GetPoolManager().ExecuteWithConnection(context.Background(), func(db *sql.DB) error {
		_, err := db.Exec(`
			CREATE TABLE test_columns (
			k_timestamp TIMESTAMP NOT NULL,
			temperature FLOAT NOT NULL,
			humidity FLOAT,
			pressure FLOAT
			) TAGS (
				id INT NOT NULL,
				name VARCHAR(30) NOT NULL
			) PRIMARY TAGS (id);
		`)
		return err
	})
	if err != nil {
		t.Fatalf("Failed to create test table: %v", err)
	}
	defer GetPoolManager().ExecuteWithConnection(context.Background(), func(db *sql.DB) error {
		_, err := db.Exec("DROP TABLE IF EXISTS test_columns")
		return err
	})

	// Test
	columns, err := GetTableColumns("test_columns")
	if err != nil {
		t.Fatalf("GetTableColumns failed: %v", err)
	}

	// Verify
	if columns == nil {
		t.Fatal("GetTableColumns returned nil")
	}

	// Check for expected columns
	expectedColumns := map[string]bool{
		"id":          false,
		"name":        false,
		"k_timestamp": false,
	}

	for _, col := range columns {
		if colName, ok := col["column_name"].(string); ok {
			if _, exists := expectedColumns[colName]; exists {
				expectedColumns[colName] = true
			}
		}
	}

	for colName, found := range expectedColumns {
		if !found {
			t.Fatalf("Expected column %s not found", colName)
		}
	}
}

// TestClose tests closing the database connection
func TestClose(t *testing.T) {
	// Setup
	err := InitDB("postgresql://kwdb:Kaiwudb%40123@localhost:26257/db_shig")
	if err != nil {
		t.Fatalf("Failed to initialize DB for test: %v", err)
	}

	// Test
	Close()

	// Verify connection pool is closed
	poolMgr := GetPoolManager()
	if poolMgr.IsInitialized() {
		t.Fatal("Connection pool still initialized after Close()")
	}
}
