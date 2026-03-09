package server

import (
	"testing"

	"gitee.com/kwdb/kwdb-mcp-server/pkg/db"
)

// TestCreateServer tests the server creation process
func TestCreateServer(t *testing.T) {
	// Stateless mode (no default connection string)
	s, err := CreateServer("")
	if err != nil {
		t.Fatalf("CreateServer failed in stateless mode: %v", err)
	}
	if s == nil {
		t.Fatal("CreateServer returned nil server in stateless mode")
	}
	Cleanup()

	// Single-DB compatible mode with (potentially invalid) connection string.
	// InitDB uses lazy loading, so this should succeed even if the DSN is invalid.
	s2, err := CreateServer("postgresql://invalid:invalid@localhost:26257/nonexistent")
	if err != nil {
		t.Fatalf("CreateServer failed with connection string: %v", err)
	}
	if s2 == nil {
		t.Fatal("CreateServer returned nil server with connection string")
	}
	Cleanup()
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
