package tools

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"gitee.com/kwdb/kwdb-mcp-server/pkg/db"
)

// defaultAdminPort is the port appended to the host when the admin base URL
// is derived from the database URI. KaiwuDB admin REST endpoints always
// listen on 8080 regardless of the SQL port.
const defaultAdminPort = 8080

// resolveAdminBaseURL selects the admin base URL with a three-level
// fallback: an explicit header value wins over an explicit flag value,
// which in turn wins over a URL derived from the DB URI. Explicit values
// are trimmed of surrounding whitespace but otherwise preserved verbatim,
// so callers can pass any scheme they like. When only the DB URI is
// available, the scheme is derived from the sslmode (require/verify-ca/
// verify-full => https; everything else => http) and the port is fixed at
// 8080. Errors from parsing the DB URI are wrapped so callers see the
// header name they need to set.
//
// The bool return is the HTTPS-equivalent of the resolved URL (see
// adminURLIsHTTPS), surfaced independently of dbURL sslmode. Callers OR it
// with the DB URL TLS signal when deciding whether to attach Basic Auth, so
// an https admin URL is always treated as TLS even when the DB URL is
// insecure — the secure-cluster-with-insecure-DB-URL path that would
// otherwise be silently dropped.
func resolveAdminBaseURL(headerValue, flagValue, dbURL string) (string, bool, error) {
	if v := strings.TrimSpace(headerValue); v != "" {
		return v, adminURLIsHTTPS(v), nil
	}
	if v := strings.TrimSpace(flagValue); v != "" {
		return v, adminURLIsHTTPS(v), nil
	}
	if dbURL == "" {
		return "", false, fmt.Errorf("missing admin URL and database URI")
	}
	info, err := db.ParseDBURL(dbURL)
	if err != nil {
		return "", false, fmt.Errorf("invalid X-Database-URI: %w", err)
	}
	scheme := "http"
	isTLS := info.IsTLS()
	if isTLS {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s:%d", scheme, info.Host, defaultAdminPort), isTLS, nil
}

// adminURLIsHTTPS reports whether the resolved admin URL uses the https
// scheme. Inputs are trimmed and the prefix is matched case-insensitively
// so the helper stays robust to operators who paste URLs from shell
// history. Empty strings and bare host:port values (no scheme prefix) are
// treated as http — callers should always pass a full scheme to avoid
// relying on this fallback.
func adminURLIsHTTPS(url string) bool {
	s := strings.ToLower(strings.TrimSpace(url))
	return strings.HasPrefix(s, "https://")
}

// isTLSForAdmin reports whether an admin endpoint connection requires TLS.
// The semantics match db.DBURLInfo.IsTLS: only require, verify-ca and
// verify-full enable TLS; every other sslmode (including disable, allow,
// prefer, unset and unknown values) falls back to insecure. The sslcert,
// sslkey and sslrootcert parameters are accepted so callers can forward the
// full URL state without restructuring their call site, but TLS
// classification depends solely on sslmode.
func isTLSForAdmin(sslmode, sslcert, sslkey, sslrootcert string) bool {
	_ = sslcert
	_ = sslkey
	_ = sslrootcert
	return db.DBURLInfo{SSLMode: sslmode}.IsTLS()
}

// buildAuthHeader returns the value for an HTTP Authorization header using
// HTTP Basic auth. The header value is "Basic " followed by the standard
// Base64 encoding of "user:password". When password is empty the function
// returns an empty string so callers can decide whether to send the header
// at all (the user is silently dropped without a password).
func buildAuthHeader(password, user string) string {
	if password == "" {
		return ""
	}
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+password))
}

// defaultAdminUnknownError is the fallback message returned when a non-zero
// code arrives without a desc. It is exported through the package boundary
// only indirectly via the resulting error string.
const defaultAdminUnknownError = "unknown error"

// formatUnexpectedResponseLimit caps the prefix of a non-JSON body that is
// echoed back in the "unexpected response format" error. 200 bytes is enough
// for an HTML title or a reverse-proxy banner without dumping entire pages
// into the error message.
const formatUnexpectedResponseLimit = 200

// AdminResponse is a parsed KaiwuDB admin REST response. KaiwuDB surfaces
// success and failure through the body's `code` field rather than HTTP
// status codes, so callers must inspect Code instead of relying on the
// transport layer. Raw preserves the full response body so downstream code
// can still reach into extra payload fields the parser does not model.
type AdminResponse struct {
	Code int             `json:"code"`
	Desc string          `json:"desc"`
	Raw  json.RawMessage `json:"-"`
}

// parseAdminResponse decodes a KaiwuDB admin REST response body. Code 0 (or
// missing) is treated as success; code -1 is surfaced as "auth failed: <desc>"
// so the caller can react to authentication problems distinctly from generic
// business errors; any other non-zero code surfaces the desc verbatim. When
// desc is empty a default "unknown error" is used. Bodies that are not valid
// JSON produce an "unexpected response format: <first 200 bytes>" error so
// callers can distinguish transport problems (HTML error pages, plain text
// from a misconfigured proxy, etc.) from protocol-level errors. The full raw
// body is preserved on success for downstream inspection.
func parseAdminResponse(body []byte) (*AdminResponse, error) {
	resp := &AdminResponse{Raw: json.RawMessage(append([]byte(nil), body...))}
	if err := json.Unmarshal(body, resp); err != nil {
		prefix := body
		if len(prefix) > formatUnexpectedResponseLimit {
			prefix = prefix[:formatUnexpectedResponseLimit]
		}
		return nil, fmt.Errorf("unexpected response format: %s", prefix)
	}
	if resp.Code == 0 {
		return resp, nil
	}
	desc := resp.Desc
	if desc == "" {
		desc = defaultAdminUnknownError
	}
	if resp.Code == -1 {
		return nil, fmt.Errorf("auth failed: %s", desc)
	}
	return nil, fmt.Errorf("%s", desc)
}

// adminRequestTimeout bounds a single admin REST call end to end (connect,
// send, receive). No retries are performed: a failed call is surfaced to the
// caller, which decides whether to try again.
const adminRequestTimeout = 30 * time.Second

// adminErrorBodyLimit caps the prefix of a response body echoed back in HTTP
// 4xx/5xx error messages, matching the truncation used for unexpected
// response formats.
const adminErrorBodyLimit = 200

// missingAdminPasswordError is returned verbatim when a TLS admin endpoint is
// addressed without a password. KaiwuDB secure clusters only accept Basic
// Auth, so an anonymous request is guaranteed to fail; failing here avoids
// sending credentials-less traffic to the cluster at all.
const missingAdminPasswordError = "TLS admin endpoint requires credentials, but DB URL has no password"

// adminHTTPClient is shared by every admin REST call. A single client (and
// therefore a single Transport) keeps idle connections pooled instead of
// leaking one never-expiring connection per call, which is what happens when a
// Transport is rebuilt for each request. Certificate verification is
// deliberately disabled: KaiwuDB clusters ship self-signed certs and this
// matches the Python tooling's ssl._create_unverified_context().
var adminHTTPClient = &http.Client{
	Timeout: adminRequestTimeout,
	Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // #nosec G402 -- self-signed KaiwuDB certs, parity with the Python tooling
		IdleConnTimeout: 90 * time.Second,
	},
}

// doAdminRequest performs a single admin REST call and returns the response,
// its fully read body, and an error translated per the design's error matrix.
//
// Behaviour:
//   - When isTLS is true a password is mandatory; without one the function
//     fails fast and no request is put on the wire.
//   - Basic Auth is attached only for TLS endpoints. Insecure deployments have
//     no user concept, so no Authorization header is sent even if credentials
//     were supplied.
//   - TLS certificate verification is intentionally skipped
//     (InsecureSkipVerify), matching the Python tooling's
//     ssl._create_unverified_context(); KaiwuDB clusters ship self-signed certs.
//   - Content-Type: application/json is set only when a body is present.
//   - HTTP 4xx becomes "admin API client error: status=N body=<first 200 bytes>",
//     HTTP 5xx becomes "admin API server error: ..." and any transport failure
//     (including context cancellation and the 30s timeout) becomes
//     "admin request failed: <err>".
//
// The response body is fully read and closed before returning, so callers can
// use the returned bytes directly; the returned *http.Response is provided for
// status and header inspection only. Callers may still call Body.Close()
// defensively — it is idempotent.
func doAdminRequest(ctx context.Context, method, url string, body []byte, user, password string, isTLS bool) (*http.Response, []byte, error) {
	if isTLS && password == "" {
		return nil, nil, fmt.Errorf("%s", missingAdminPasswordError)
	}

	var reader io.Reader
	if len(body) > 0 {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return nil, nil, fmt.Errorf("admin request failed: %w", err)
	}
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	if isTLS {
		if auth := buildAuthHeader(password, user); auth != "" {
			req.Header.Set("Authorization", auth)
		}
	}

	resp, err := adminHTTPClient.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("admin request failed: %w", err)
	}
	respBody, err := io.ReadAll(resp.Body)
	closeErr := resp.Body.Close()
	if err != nil {
		return nil, nil, fmt.Errorf("admin request failed: %w", err)
	}
	if closeErr != nil {
		return nil, nil, fmt.Errorf("admin request failed: %w", closeErr)
	}

	switch {
	case resp.StatusCode >= 500:
		return resp, respBody, fmt.Errorf("admin API server error: status=%d body=%s",
			resp.StatusCode, truncateAdminErrorBody(respBody))
	case resp.StatusCode >= 400:
		return resp, respBody, fmt.Errorf("admin API client error: status=%d body=%s",
			resp.StatusCode, truncateAdminErrorBody(respBody))
	}
	return resp, respBody, nil
}

// truncateAdminErrorBody returns at most the first adminErrorBodyLimit bytes
// of body so error messages stay readable when a server returns a large HTML
// page or stack trace.
func truncateAdminErrorBody(body []byte) []byte {
	if len(body) > adminErrorBodyLimit {
		return body[:adminErrorBodyLimit]
	}
	return body
}
