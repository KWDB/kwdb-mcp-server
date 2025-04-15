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

	// Test with invalid connection string
	_, err = CreateServer("postgresql://invalid:invalid@localhost:26257/nonexistent")
	if err == nil {
		t.Fatal("CreateServer should fail with invalid connection string")
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

	// Verify DB connection is closed
	if db.DB != nil {
		err = db.DB.Ping()
		if err == nil {
			t.Fatal("DB connection still active after Cleanup()")
		}
	}
}
