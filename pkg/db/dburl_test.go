package db

import (
	"strings"
	"testing"
)

func TestParseDBURLStandardURL(t *testing.T) {
	info, err := ParseDBURL("postgresql://kwdb:Kaiwudb%40123@localhost:26257/db_shig?sslmode=require&sslcert=/tmp/client.crt&sslkey=/tmp/client.key&sslrootcert=/tmp/ca.crt")
	if err != nil {
		t.Fatalf("ParseDBURL returned error: %v", err)
	}

	if info.Scheme != "postgresql" {
		t.Errorf("Scheme = %q, want %q", info.Scheme, "postgresql")
	}
	if info.User != "kwdb" {
		t.Errorf("User = %q, want %q", info.User, "kwdb")
	}
	if info.Password != "Kaiwudb@123" {
		t.Errorf("Password = %q, want %q (URL-decoded)", info.Password, "Kaiwudb@123")
	}
	if !info.HasPassword {
		t.Error("HasPassword = false, want true")
	}
	if info.Host != "localhost" {
		t.Errorf("Host = %q, want %q", info.Host, "localhost")
	}
	if info.Port != 26257 {
		t.Errorf("Port = %d, want %d", info.Port, 26257)
	}
	if info.Database != "db_shig" {
		t.Errorf("Database = %q, want %q", info.Database, "db_shig")
	}
	if info.SSLMode != "require" {
		t.Errorf("SSLMode = %q, want %q", info.SSLMode, "require")
	}
	if info.SSLCert != "/tmp/client.crt" {
		t.Errorf("SSLCert = %q, want %q", info.SSLCert, "/tmp/client.crt")
	}
	if info.SSLKey != "/tmp/client.key" {
		t.Errorf("SSLKey = %q, want %q", info.SSLKey, "/tmp/client.key")
	}
	if info.SSLRootCert != "/tmp/ca.crt" {
		t.Errorf("SSLRootCert = %q, want %q", info.SSLRootCert, "/tmp/ca.crt")
	}
}

func TestParseDBURLIsTLS(t *testing.T) {
	tests := []struct {
		sslMode string
		want    bool
	}{
		{"require", true},
		{"verify-ca", true},
		{"verify-full", true},
		{"disable", false},
		{"", false},
		{"allow", false},
		{"prefer", false},
		{"bogus", false},
	}

	for _, tt := range tests {
		t.Run(tt.sslMode, func(t *testing.T) {
			if got := (DBURLInfo{SSLMode: tt.sslMode}).IsTLS(); got != tt.want {
				t.Errorf("DBURLInfo{SSLMode: %q}.IsTLS() = %v, want %v", tt.sslMode, got, tt.want)
			}
		})
	}
}

// infoWithSSLMode builds a minimal DBURLInfo carrying only the SSLMode field
// the IsTLS tests care about. Using a constructor keeps the test body focused
// on the matrix of modes, not on incidental URL parsing.
func infoWithSSLMode(mode string) DBURLInfo {
	return DBURLInfo{SSLMode: mode}
}

// TestDBURLIsTLS gives the TLS-detection contract a single, intentionally
// obvious home. Decision 3 of the Design Doc pins IsTLS to exactly three
// TLS values (require / verify-ca / verify-full); every other value
// (unset, disable, allow, prefer, unknown) must be reported as insecure.
// Grouping the two halves of the matrix in their own loops makes a future
// regression show up immediately as the wrong group.
func TestDBURLIsTLS(t *testing.T) {
	for _, mode := range []string{"require", "verify-ca", "verify-full"} {
		mode := mode
		t.Run("tls/"+mode, func(t *testing.T) {
			if got := infoWithSSLMode(mode).IsTLS(); !got {
				t.Errorf("%q: IsTLS() = false, want true", mode)
			}
		})
	}
	for _, mode := range []string{"", "disable", "allow", "prefer", "unknown"} {
		mode := mode
		t.Run("insecure/"+mode, func(t *testing.T) {
			if got := infoWithSSLMode(mode).IsTLS(); got {
				t.Errorf("%q: IsTLS() = true, want false", mode)
			}
		})
	}
}

func TestParseDBURLDefaultPort(t *testing.T) {
	info, err := ParseDBURL("postgresql://kwdb:pass@localhost/db_shig")
	if err != nil {
		t.Fatalf("ParseDBURL returned error: %v", err)
	}
	if info.Port != 26257 {
		t.Errorf("Port = %d, want default %d", info.Port, 26257)
	}
}

func TestParseDBURLPasswordPresence(t *testing.T) {
	tests := []struct {
		name            string
		url             string
		wantPassword    string
		wantHasPassword bool
	}{
		{"with password", "postgresql://kwdb:pass@localhost:26257/db_shig", "pass", true},
		{"empty password after colon", "postgresql://kwdb:@localhost:26257/db_shig", "", false},
		{"no password segment", "postgresql://kwdb@localhost:26257/db_shig", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info, err := ParseDBURL(tt.url)
			if err != nil {
				t.Fatalf("ParseDBURL returned error: %v", err)
			}
			if info.Password != tt.wantPassword {
				t.Errorf("Password = %q, want %q", info.Password, tt.wantPassword)
			}
			if info.HasPassword != tt.wantHasPassword {
				t.Errorf("HasPassword = %v, want %v", info.HasPassword, tt.wantHasPassword)
			}
		})
	}
}

func TestParseDBURLQueryParamsAreCaseSensitive(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{"uppercase", "postgresql://kwdb:pass@localhost:26257/db_shig?SSLMODE=require"},
		{"mixed case", "postgresql://kwdb:pass@localhost:26257/db_shig?SslMode=require"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info, err := ParseDBURL(tt.url)
			if err != nil {
				t.Fatalf("ParseDBURL returned error: %v", err)
			}
			if info.SSLMode != "" {
				t.Errorf("SSLMode = %q, want %q (non-lowercase param must not be recognized)", info.SSLMode, "")
			}
			if info.IsTLS() {
				t.Error("IsTLS() = true, want false")
			}
		})
	}
}

func TestParseDBURLDuplicateQueryParamTakesFirst(t *testing.T) {
	info, err := ParseDBURL("postgresql://kwdb:pass@localhost:26257/db_shig?sslmode=require&sslmode=disable")
	if err != nil {
		t.Fatalf("ParseDBURL returned error: %v", err)
	}
	if info.SSLMode != "require" {
		t.Errorf("SSLMode = %q, want first value %q", info.SSLMode, "require")
	}
}

func TestParseDBURLErrors(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{"wrong scheme", "mysql://kwdb:pass@localhost:26257/db_shig"},
		{"no scheme", "kwdb:pass@localhost:26257/db_shig"},
		{"missing user", "postgresql://localhost:26257/db_shig"},
		{"missing host", "postgresql://kwdb:pass@/db_shig"},
		{"empty database name", "postgresql://kwdb:pass@localhost:26257/"},
		{"no database segment", "postgresql://kwdb:pass@localhost:26257"},
		{"unencoded @ in password", "postgresql://kwdb:plain@pass@localhost:26257/db_shig"},
		{"non-numeric port", "postgresql://kwdb:pass@localhost:abc/db_shig"},
		{"empty url", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := ParseDBURL(tt.url); err == nil {
				t.Errorf("ParseDBURL(%q) = nil error, want error", tt.url)
			}
		})
	}
}

// TestParseDBURLEdgeCases is a single structured table-driven test that
// enumerates every edge case required by Task 2 of the Design Doc: empty
// password, no-password segment, missing user, empty database name,
// URL-encoded password, uppercase query parameters, certificate paths,
// duplicate sslmode, unencoded '@' in userinfo, and non-postgresql
// schemes. Each row pins down the expected parsed fields (and, for
// failures, the expected error substring) so a future regression points
// directly at the broken scenario.
func TestParseDBURLEdgeCases(t *testing.T) {
	cases := []struct {
		name            string
		raw             string
		wantErr         string // substring expected in the error; empty => success expected
		wantUser        string
		wantPassword    string
		wantHasPassword bool
		wantHost        string
		wantPort        int
		wantDatabase    string
		wantSSLMode     string
		wantSSLCert     string
		wantSSLKey      string
		wantSSLRootCert string
		wantIsTLS       bool
	}{
		// --- success: password-shape variants ---
		{
			name:            "empty password after colon",
			raw:             "postgresql://user:@host/db",
			wantUser:        "user",
			wantPassword:    "",
			wantHasPassword: false,
			wantHost:        "host",
			wantPort:        defaultDBPort,
			wantDatabase:    "db",
		},
		{
			name:            "no password segment",
			raw:             "postgresql://user@host/db",
			wantUser:        "user",
			wantPassword:    "",
			wantHasPassword: false,
			wantHost:        "host",
			wantPort:        defaultDBPort,
			wantDatabase:    "db",
		},
		{
			name:            "url-encoded password decoded",
			raw:             "postgresql://user:Kaiwudb%40123@host/db",
			wantUser:        "user",
			wantPassword:    "Kaiwudb@123",
			wantHasPassword: true,
			wantHost:        "host",
			wantPort:        defaultDBPort,
			wantDatabase:    "db",
		},

		// --- success: query parameter handling ---
		{
			name:            "uppercase query parameter is not recognized",
			raw:             "postgresql://user:pass@host/db?SSLMODE=require",
			wantUser:        "user",
			wantPassword:    "pass",
			wantHasPassword: true,
			wantHost:        "host",
			wantPort:        defaultDBPort,
			wantDatabase:    "db",
			wantSSLMode:     "",
			wantIsTLS:       false,
		},
		{
			name:            "certificate paths",
			raw:             "postgresql://user:pass@host/db?sslmode=verify-full&sslcert=/tmp/client.crt&sslkey=/tmp/client.key&sslrootcert=/tmp/ca.crt",
			wantUser:        "user",
			wantPassword:    "pass",
			wantHasPassword: true,
			wantHost:        "host",
			wantPort:        defaultDBPort,
			wantDatabase:    "db",
			wantSSLMode:     "verify-full",
			wantSSLCert:     "/tmp/client.crt",
			wantSSLKey:      "/tmp/client.key",
			wantSSLRootCert: "/tmp/ca.crt",
			wantIsTLS:       true,
		},
		{
			name:            "duplicate sslmode takes first value",
			raw:             "postgresql://user:pass@host/db?sslmode=require&sslmode=disable",
			wantUser:        "user",
			wantPassword:    "pass",
			wantHasPassword: true,
			wantHost:        "host",
			wantPort:        defaultDBPort,
			wantDatabase:    "db",
			wantSSLMode:     "require",
			wantIsTLS:       true,
		},

		// --- failures: error messages must match the documented substrings ---
		{
			name:    "missing user",
			raw:     "postgresql://host:26257/db",
			wantErr: "missing user",
		},
		{
			name:    "empty database name",
			raw:     "postgresql://user:pass@host:26257/",
			wantErr: "empty database name",
		},
		{
			name:    "unencoded multiple @ in userinfo",
			raw:     "postgresql://user:plain@pass@host/db",
			wantErr: "unencoded '@'",
		},
		{
			name:    "non-postgresql scheme rejected",
			raw:     "mysql://user:pass@host/db",
			wantErr: "unsupported scheme",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			info, err := ParseDBURL(tc.raw)

			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("ParseDBURL(%q) = nil error, want error containing %q", tc.raw, tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Errorf("ParseDBURL(%q) error = %q, want substring %q", tc.raw, err.Error(), tc.wantErr)
				}
				return
			}

			if err != nil {
				t.Fatalf("ParseDBURL(%q) returned unexpected error: %v", tc.raw, err)
			}
			if info.User != tc.wantUser {
				t.Errorf("User = %q, want %q", info.User, tc.wantUser)
			}
			if info.Password != tc.wantPassword {
				t.Errorf("Password = %q, want %q", info.Password, tc.wantPassword)
			}
			if info.HasPassword != tc.wantHasPassword {
				t.Errorf("HasPassword = %v, want %v", info.HasPassword, tc.wantHasPassword)
			}
			if info.Host != tc.wantHost {
				t.Errorf("Host = %q, want %q", info.Host, tc.wantHost)
			}
			if info.Port != tc.wantPort {
				t.Errorf("Port = %d, want %d", info.Port, tc.wantPort)
			}
			if info.Database != tc.wantDatabase {
				t.Errorf("Database = %q, want %q", info.Database, tc.wantDatabase)
			}
			if info.SSLMode != tc.wantSSLMode {
				t.Errorf("SSLMode = %q, want %q", info.SSLMode, tc.wantSSLMode)
			}
			if info.SSLCert != tc.wantSSLCert {
				t.Errorf("SSLCert = %q, want %q", info.SSLCert, tc.wantSSLCert)
			}
			if info.SSLKey != tc.wantSSLKey {
				t.Errorf("SSLKey = %q, want %q", info.SSLKey, tc.wantSSLKey)
			}
			if info.SSLRootCert != tc.wantSSLRootCert {
				t.Errorf("SSLRootCert = %q, want %q", info.SSLRootCert, tc.wantSSLRootCert)
			}
			if got := info.IsTLS(); got != tc.wantIsTLS {
				t.Errorf("IsTLS() = %v, want %v", got, tc.wantIsTLS)
			}
		})
	}
}
