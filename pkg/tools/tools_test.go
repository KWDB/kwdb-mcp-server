package tools

import (
	"testing"
)

// TestIsSelectWithoutLimit tests the SELECT query detection
func TestIsSelectWithoutLimit(t *testing.T) {
	// Test cases
	testCases := []struct {
		query    string
		expected bool
	}{
		{"SELECT * FROM users", true},
		{"select * from users", true},
		{"SELECT * FROM users LIMIT 10", false},
		{"select * from users limit 10", false},
		{"SELECT * FROM users WHERE id > 5", true},
		{"SELECT * FROM users WHERE id > 5 LIMIT 20", false},
		{"INSERT INTO users VALUES (1, 'test')", false},
		{"UPDATE users SET name = 'test'", false},
	}

	// Run tests
	for _, tc := range testCases {
		result := isSelectWithoutLimit(tc.query)
		if result != tc.expected {
			t.Errorf("isSelectWithoutLimit(%q) = %v, expected %v", tc.query, result, tc.expected)
		}
	}
}

// TestAddLimitToQuery tests adding LIMIT to queries
func TestAddLimitToQuery(t *testing.T) {
	// Test cases
	testCases := []struct {
		query    string
		limit    int
		expected string
	}{
		{"SELECT * FROM users", 10, "SELECT * FROM users LIMIT 10"},
		{"select * from users", 20, "select * from users LIMIT 20"},
		{"SELECT * FROM users WHERE id > 5", 15, "SELECT * FROM users WHERE id > 5 LIMIT 15"},
		{"SELECT * FROM users ORDER BY id", 5, "SELECT * FROM users ORDER BY id LIMIT 5"},
	}

	// Run tests
	for _, tc := range testCases {
		result := addLimitToQuery(tc.query, tc.limit)
		if result != tc.expected {
			t.Errorf("addLimitToQuery(%q, %d) = %q, expected %q", tc.query, tc.limit, result, tc.expected)
		}
	}
}
