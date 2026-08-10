package db

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// defaultDBPort is the KaiwuDB SQL port used when the DB URL omits one.
const defaultDBPort = 26257

// DBURLInfo holds the parsed components of a KaiwuDB connection URL of the form
// postgresql://[user[:password]@]host[:port]/dbname[?sslmode=...&sslcert=...&sslkey=...&sslrootcert=...]
type DBURLInfo struct {
	Scheme         string // always "postgresql"
	User, Password string
	Host           string
	Port           int
	Database       string
	SSLMode        string // raw sslmode value, empty when not set
	SSLCert        string
	SSLKey         string
	SSLRootCert    string
	HasPassword    bool
}

// ParseDBURL parses a KaiwuDB connection URL. Only the postgresql scheme is
// accepted, user/host/database are required, and the port defaults to 26257.
// Query parameter names are matched case-sensitively in lower case, so
// SSLMODE=require is not recognized as sslmode.
func ParseDBURL(dbURL string) (DBURLInfo, error) {
	u, err := url.Parse(dbURL)
	if err != nil {
		return DBURLInfo{}, fmt.Errorf("invalid database URI: %w", err)
	}
	if u.Scheme != "postgresql" {
		return DBURLInfo{}, fmt.Errorf("unsupported scheme %q: database URI must start with postgresql://", u.Scheme)
	}
	if err := rejectUnencodedAtInUserinfo(dbURL); err != nil {
		return DBURLInfo{}, err
	}

	info := DBURLInfo{Scheme: u.Scheme}

	if u.User == nil || u.User.Username() == "" {
		return DBURLInfo{}, fmt.Errorf("missing user in database URI")
	}
	info.User = u.User.Username()
	if password, _ := u.User.Password(); password != "" {
		info.Password = password
		info.HasPassword = true
	}

	info.Host = u.Hostname()
	if info.Host == "" {
		return DBURLInfo{}, fmt.Errorf("missing host in database URI")
	}

	info.Port = defaultDBPort
	if port := u.Port(); port != "" {
		info.Port, err = strconv.Atoi(port)
		if err != nil {
			return DBURLInfo{}, fmt.Errorf("invalid port %q in database URI", port)
		}
	}

	// u.Path always starts with "/" for any non-empty URL. We strip it
	// with u.Path[1:] below, so the database name needs at least one
	// character after the slash: len == 1 means a bare "/" (no name),
	// len == 0 means no path component at all.
	if len(u.Path) < 2 {
		return DBURLInfo{}, fmt.Errorf("empty database name in database URI")
	}
	info.Database = u.Path[1:]

	query, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return DBURLInfo{}, fmt.Errorf("invalid query parameters in database URI: %w", err)
	}
	info.SSLMode = query.Get("sslmode")
	info.SSLCert = query.Get("sslcert")
	info.SSLKey = query.Get("sslkey")
	info.SSLRootCert = query.Get("sslrootcert")

	return info, nil
}

// IsTLS reports whether the connection requires TLS. Only require, verify-ca
// and verify-full mandate TLS; every other value (including disable, allow,
// prefer, unset and unknown values) falls back to insecure.
func (d DBURLInfo) IsTLS() bool {
	switch d.SSLMode {
	case "require", "verify-ca", "verify-full":
		return true
	default:
		return false
	}
}

// rejectUnencodedAtInUserinfo rejects inputs that contain an unencoded '@'
// inside the userinfo segment. net/url accepts such inputs (it splits on the
// last '@') but the design spec requires an explicit error so callers do not
// silently get a mangled password.
func rejectUnencodedAtInUserinfo(dbURL string) error {
	schemeEnd := strings.Index(dbURL, "://")
	if schemeEnd < 0 {
		return nil
	}
	rest := dbURL[schemeEnd+3:]
	if i := strings.IndexAny(rest, "/?#"); i >= 0 {
		rest = rest[:i]
	}
	at := strings.LastIndex(rest, "@")
	if at < 0 {
		return nil
	}
	if strings.Contains(rest[:at], "@") {
		return fmt.Errorf("unencoded '@' in password: URL-encode '@' as %%40 in database URI")
	}
	return nil
}
