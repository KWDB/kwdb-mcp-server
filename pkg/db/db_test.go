package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
)

const testDSN = "postgresql://kwdb:Kaiwudb%40123@localhost:26257/db_shig?sslmode=disable"

// skipIfDBUnavailable initializes DB with testDSN and skips the test if connection fails
// (e.g. no DB running, no valid license). Call defer Close() after.
func skipIfDBUnavailable(t *testing.T) {
	if err := InitDB(testDSN); err != nil {
		t.Fatalf("Failed to initialize DB for test: %v", err)
	}
	err := GetPoolManager().ExecuteWithConnection(context.Background(), func(db *sql.DB) error {
		return db.Ping()
	})
	if err != nil {
		Close()
		msg := err.Error()
		if strings.Contains(msg, "SSL is not enabled") ||
			strings.Contains(msg, "connection refused") ||
			strings.Contains(msg, "without valid license") ||
			strings.Contains(msg, "unavailable after reinitialize") {
			t.Skipf("Skipping test: DB unavailable (%v)", err)
		}
		t.Fatalf("DB ping failed: %v", err)
	}
}

// TestInitDB tests the database initialization
func TestInitDB(t *testing.T) {
	err := InitDB(testDSN)
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
		Close()
		msg := err.Error()
		if strings.Contains(msg, "SSL is not enabled") ||
			strings.Contains(msg, "connection refused") ||
			strings.Contains(msg, "without valid license") ||
			strings.Contains(msg, "unavailable after reinitialize") ||
			strings.Contains(msg, "password authentication failed") {
			t.Skipf("Skipping test: DB unavailable (%v)", err)
		}
		t.Fatalf("DB ping failed after initialization: %v", err)
	}

	// Test with invalid connection string - should succeed in initialization but fail on actual use
	err = InitDB("postgresql://invalid:invalid@localhost:26257/nonexistent?sslmode=disable")
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
	skipIfDBUnavailable(t)
	defer Close()

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
	skipIfDBUnavailable(t)
	defer Close()

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
	skipIfDBUnavailable(t)
	defer Close()

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
	skipIfDBUnavailable(t)
	defer Close()

	err := GetPoolManager().ExecuteWithConnection(context.Background(), func(db *sql.DB) error {
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
	err := InitDB(testDSN)
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
