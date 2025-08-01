package server

import (
	"testing"

	"gitee.com/kwdb/kwdb-mcp-server/pkg/db"
)

// TestCreateServer tests the server creation process
func TestCreateServer(t *testing.T) {
	// Test with valid connection string
	s, err := CreateServer("postgresql://kwdb:Kaiwudb%40123@localhost:26257/db_shig")
	if err != nil {
		t.Fatalf("CreateServer failed with valid connection string: %v", err)
	}
	defer Cleanup()

	// Verify server was created
	if s == nil {
		t.Fatal("CreateServer returned nil server")
	}

	// Test with invalid connection string - should succeed in creation but fail on actual use
	s2, err := CreateServer("postgresql://invalid:invalid@localhost:26257/nonexistent")
	if err != nil {
		t.Fatalf("CreateServer should succeed with invalid connection string (lazy loading): %v", err)
	}
	defer Cleanup()

	if s2 == nil {
		t.Fatal("CreateServer should return non-nil server even with invalid connection")
	}
}

// TestCleanup tests the cleanup process
func TestCleanup(t *testing.T) {
	// Setup
	err := db.InitDB("postgresql://kwdb:Kaiwudb%40123@localhost:26257/db_shig")
	if err != nil {
		t.Fatalf("Failed to initialize DB for test: %v", err)
	}

	// Test
	Cleanup()

	// Verify connection pool is closed
	poolMgr := db.GetPoolManager()
	if poolMgr.IsInitialized() {
		t.Fatal("Connection pool still initialized after Cleanup()")
	}
}
