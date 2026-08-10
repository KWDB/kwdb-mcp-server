package tools

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestIsTLSForAdmin verifies the TLS semantics for admin endpoints match the
// database URL rules: only require, verify-ca, and verify-full enable TLS.
func TestIsTLSForAdmin(t *testing.T) {
	cases := []struct {
		name        string
		sslmode     string
		sslcert     string
		sslkey      string
		sslrootcert string
		want        bool
	}{
		{name: "require", sslmode: "require", want: true},
		{name: "verify-ca", sslmode: "verify-ca", want: true},
		{name: "verify-full", sslmode: "verify-full", want: true},
		{name: "empty", sslmode: "", want: false},
		{name: "disable", sslmode: "disable", want: false},
		{name: "allow", sslmode: "allow", want: false},
		{name: "prefer", sslmode: "prefer", want: false},
		{name: "unknown", sslmode: "unknown", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := isTLSForAdmin(tc.sslmode, tc.sslcert, tc.sslkey, tc.sslrootcert)
			if got != tc.want {
				t.Fatalf("isTLSForAdmin(%q, %q, %q, %q) = %v, want %v",
					tc.sslmode, tc.sslcert, tc.sslkey, tc.sslrootcert, got, tc.want)
			}
		})
	}
}

// TestBuildAuthHeader verifies that buildAuthHeader emits the Basic Auth
// header value using standard Base64, and that an empty password yields an
// empty string so callers can decide whether to send it.
func TestBuildAuthHeader(t *testing.T) {
	t.Run("non-empty password", func(t *testing.T) {
		want := "Basic " + base64.StdEncoding.EncodeToString([]byte("root:p@ss"))
		got := buildAuthHeader("p@ss", "root")
		if got != want {
			t.Fatalf("buildAuthHeader(%q, %q) = %q, want %q", "p@ss", "root", got, want)
		}
	})

	t.Run("empty password", func(t *testing.T) {
		got := buildAuthHeader("", "root")
		if got != "" {
			t.Fatalf("buildAuthHeader(%q, %q) = %q, want empty string", "", "root", got)
		}
	})
}

// TestResolveAdminBaseURL verifies the three-level fallback for resolving the
// admin base URL: explicit header > explicit flag > derived from the DB URL.
// Explicit values are returned as-is (after trimming whitespace), and a
// missing DB URL yields an error. The bool return must reflect whether the
// resolved URL uses https — callers OR it with the DB URL TLS signal when
// deciding whether to attach Basic Auth.
func TestResolveAdminBaseURL(t *testing.T) {
	cases := []struct {
		name        string
		headerValue string
		flagValue   string
		dbURL       string
		wantURL     string
		wantIsTLS   bool
		wantErr     string
	}{
		{
			name:        "header takes precedence over flag and dbURL",
			headerValue: "https://admin.example.com:9999",
			flagValue:   "http://flag.example.com:8080",
			dbURL:       "postgresql://root@db.example.com:26257/kwdb?sslmode=require",
			wantURL:     "https://admin.example.com:9999",
			wantIsTLS:   true,
		},
		{
			name:        "flag used when header empty and dbURL provided",
			headerValue: "",
			flagValue:   "http://flag.example.com:8080",
			dbURL:       "postgresql://root@db.example.com:26257/kwdb?sslmode=require",
			wantURL:     "http://flag.example.com:8080",
			wantIsTLS:   false,
		},
		{
			name:        "dbURL with require sslmode derives https",
			headerValue: "",
			flagValue:   "",
			dbURL:       "postgresql://root@db.example.com:26257/kwdb?sslmode=require",
			wantURL:     "https://db.example.com:8080",
			wantIsTLS:   true,
		},
		{
			name:        "dbURL with disable sslmode derives http",
			headerValue: "",
			flagValue:   "",
			dbURL:       "postgresql://root@db.example.com:26257/kwdb?sslmode=disable",
			wantURL:     "http://db.example.com:8080",
			wantIsTLS:   false,
		},
		{
			name:        "header with whitespace is trimmed",
			headerValue: "   https://admin.example.com:9999\t",
			flagValue:   "",
			dbURL:       "postgresql://root@db.example.com:26257/kwdb?sslmode=require",
			wantURL:     "https://admin.example.com:9999",
			wantIsTLS:   true,
		},
		{
			name:        "flag with whitespace is trimmed",
			headerValue: "",
			flagValue:   "  http://flag.example.com:8080  ",
			dbURL:       "postgresql://root@db.example.com:26257/kwdb?sslmode=require",
			wantURL:     "http://flag.example.com:8080",
			wantIsTLS:   false,
		},
		{
			name:        "explicit header scheme is preserved",
			headerValue: "http://admin.example.com:8080",
			flagValue:   "",
			dbURL:       "postgresql://root@db.example.com:26257/kwdb?sslmode=require",
			wantURL:     "http://admin.example.com:8080",
			wantIsTLS:   false,
		},
		{
			name:        "all missing returns error",
			headerValue: "",
			flagValue:   "",
			dbURL:       "",
			wantErr:     "missing admin URL and database URI",
		},
		{
			name:        "invalid dbURL returns wrapped error",
			headerValue: "",
			flagValue:   "",
			dbURL:       "not-a-valid-uri",
			wantErr:     "invalid X-Database-URI:",
		},
		{
			name:        "dbURL without sslmode defaults to http",
			headerValue: "",
			flagValue:   "",
			dbURL:       "postgresql://root@db.example.com:26257/kwdb",
			wantURL:     "http://db.example.com:8080",
			wantIsTLS:   false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, gotIsTLS, err := resolveAdminBaseURL(tc.headerValue, tc.flagValue, tc.dbURL)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("resolveAdminBaseURL(%q, %q, %q) = %q, want error containing %q",
						tc.headerValue, tc.flagValue, tc.dbURL, got, tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("resolveAdminBaseURL(%q, %q, %q) error = %q, want error containing %q",
						tc.headerValue, tc.flagValue, tc.dbURL, err.Error(), tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveAdminBaseURL(%q, %q, %q) unexpected error: %v",
					tc.headerValue, tc.flagValue, tc.dbURL, err)
			}
			if got != tc.wantURL {
				t.Fatalf("resolveAdminBaseURL(%q, %q, %q) = %q, want %q",
					tc.headerValue, tc.flagValue, tc.dbURL, got, tc.wantURL)
			}
			if gotIsTLS != tc.wantIsTLS {
				t.Fatalf("resolveAdminBaseURL(%q, %q, %q) isTLS = %v, want %v",
					tc.headerValue, tc.flagValue, tc.dbURL, gotIsTLS, tc.wantIsTLS)
			}
		})
	}
}

// TestParseAdminResponse verifies the KaiwuDB admin response parser. KaiwuDB
// admin REST endpoints surface success and failure through the body's `code`
// field rather than HTTP status codes: code 0 (or missing) means success,
// code -1 means authentication failed, and any other code means a business
// error. Non-JSON responses (e.g. HTML error pages from a reverse proxy) must
// surface a "unexpected response format" error so the caller can distinguish
// transport problems from protocol-level errors. Raw bytes are preserved so
// callers can still inspect the original payload.
func TestParseAdminResponse(t *testing.T) {
	cases := []struct {
		name        string
		body        string
		wantCode    int
		wantDesc    string
		wantRawKeep bool
		wantErr     string
	}{
		{
			name:        "code 0 success with payload",
			body:        `{"code":0,"desc":"ok","data":{"k":"v"}}`,
			wantCode:    0,
			wantDesc:    "ok",
			wantRawKeep: true,
		},
		{
			name:        "missing code is treated as success",
			body:        `{"desc":"ok"}`,
			wantCode:    0,
			wantDesc:    "ok",
			wantRawKeep: true,
		},
		{
			name:     "code -1 yields auth failed",
			body:     `{"code":-1,"desc":"unauthorized"}`,
			wantCode: -1,
			wantDesc: "unauthorized",
			wantErr:  "auth failed: unauthorized",
		},
		{
			name:     "code 42 yields desc error",
			body:     `{"code":42,"desc":"bad request"}`,
			wantCode: 42,
			wantDesc: "bad request",
			wantErr:  "bad request",
		},
		{
			name:     "non-zero code without desc falls back to default",
			body:     `{"code":7}`,
			wantCode: 7,
			wantDesc: "",
			wantErr:  "unknown error",
		},
		{
			name:    "HTML response surfaces truncated prefix",
			body:    `<!DOCTYPE html><html><head><title>502 Bad Gateway</title></head><body>lots of additional content that exceeds the truncation window so the caller can see what came back from the reverse proxy without dumping the entire page into the error message</body></html>`,
			wantErr: "unexpected response format: <!DOCTYPE html>",
		},
		{
			name:    "plain text response surfaces truncated prefix",
			body:    `plain text that is definitely not JSON and should be reported back as an unexpected response format so the caller can debug what came back from the wire without dumping the entire body into the error message`,
			wantErr: "unexpected response format: plain text that is definitely not JSON",
		},
		{
			name:    "long non-JSON body is truncated at 200 bytes",
			body:    strings.Repeat("x", 500),
			wantErr: "unexpected response format: " + strings.Repeat("x", 200),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseAdminResponse([]byte(tc.body))
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("parseAdminResponse(%q) = %+v, want error containing %q",
						tc.body, got, tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("parseAdminResponse(%q) error = %q, want error containing %q",
						tc.body, err.Error(), tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseAdminResponse(%q) unexpected error: %v", tc.body, err)
			}
			if got.Code != tc.wantCode {
				t.Fatalf("parseAdminResponse(%q) Code = %d, want %d",
					tc.body, got.Code, tc.wantCode)
			}
			if got.Desc != tc.wantDesc {
				t.Fatalf("parseAdminResponse(%q) Desc = %q, want %q",
					tc.body, got.Desc, tc.wantDesc)
			}
			if tc.wantRawKeep && !bytes.Equal(got.Raw, []byte(tc.body)) {
				t.Fatalf("parseAdminResponse(%q) Raw = %q, want raw bytes preserved",
					tc.body, string(got.Raw))
			}
		})
	}
}

// TestDoAdminRequest verifies the admin HTTP client: TLS requests skip
// certificate verification (self-signed certs from httptest.NewTLSServer are
// accepted) and carry Basic Auth, insecure requests never carry an
// Authorization header, a TLS endpoint without a password fails before any
// request leaves the process, and HTTP 4xx/5xx plus transport failures are
// translated into the error messages from the design's error matrix.
func TestDoAdminRequest(t *testing.T) {
	t.Run("TLS request with credentials succeeds against self-signed cert", func(t *testing.T) {
		var calls atomic.Int32
		var gotAuth, gotMethod, gotContentType string
		var gotBody []byte
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			gotAuth = r.Header.Get("Authorization")
			gotMethod = r.Method
			gotContentType = r.Header.Get("Content-Type")
			gotBody, _ = io.ReadAll(r.Body)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"code":0,"desc":"ok"}`))
		}))
		defer srv.Close()

		resp, body, err := doAdminRequest(context.Background(), http.MethodPost, srv.URL,
			[]byte(`{"q":1}`), "root", "secret", true)
		if err != nil {
			t.Fatalf("doAdminRequest() unexpected error: %v", err)
		}
		if resp == nil {
			t.Fatalf("doAdminRequest() response = nil, want non-nil")
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("doAdminRequest() status = %d, want %d", resp.StatusCode, http.StatusOK)
		}
		if string(body) != `{"code":0,"desc":"ok"}` {
			t.Fatalf("doAdminRequest() body = %q, want %q", string(body), `{"code":0,"desc":"ok"}`)
		}
		if n := calls.Load(); n != 1 {
			t.Fatalf("handler call count = %d, want 1", n)
		}
		if want := buildAuthHeader("secret", "root"); gotAuth != want {
			t.Fatalf("Authorization header = %q, want %q", gotAuth, want)
		}
		if gotMethod != http.MethodPost {
			t.Fatalf("method = %q, want %q", gotMethod, http.MethodPost)
		}
		if gotContentType != "application/json" {
			t.Fatalf("Content-Type = %q, want %q", gotContentType, "application/json")
		}
		if string(gotBody) != `{"q":1}` {
			t.Fatalf("request body = %q, want %q", string(gotBody), `{"q":1}`)
		}
	})

	t.Run("response body is drained and closed by the client", func(t *testing.T) {
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"code":0}`))
		}))
		defer srv.Close()

		resp, _, err := doAdminRequest(context.Background(), http.MethodGet, srv.URL, nil, "root", "secret", true)
		if err != nil {
			t.Fatalf("doAdminRequest() unexpected error: %v", err)
		}
		if _, err := io.ReadAll(resp.Body); err == nil {
			t.Fatalf("reading resp.Body succeeded, want error because body is already closed")
		}
		if err := resp.Body.Close(); err != nil {
			t.Fatalf("second resp.Body.Close() = %v, want nil (close must be idempotent)", err)
		}
	})

	t.Run("insecure request sends no Authorization header", func(t *testing.T) {
		var calls atomic.Int32
		var gotAuth string
		var authPresent bool
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			gotAuth = r.Header.Get("Authorization")
			_, authPresent = r.Header["Authorization"]
			_, _ = w.Write([]byte(`{"code":0}`))
		}))
		defer srv.Close()

		_, _, err := doAdminRequest(context.Background(), http.MethodGet, srv.URL, nil, "root", "secret", false)
		if err != nil {
			t.Fatalf("doAdminRequest() unexpected error: %v", err)
		}
		if n := calls.Load(); n != 1 {
			t.Fatalf("handler call count = %d, want 1", n)
		}
		if authPresent || gotAuth != "" {
			t.Fatalf("Authorization header = %q (present=%v), want absent", gotAuth, authPresent)
		}
	})

	t.Run("TLS without password fails before sending the request", func(t *testing.T) {
		var calls atomic.Int32
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			_, _ = w.Write([]byte(`{"code":0}`))
		}))
		defer srv.Close()

		resp, body, err := doAdminRequest(context.Background(), http.MethodGet, srv.URL, nil, "root", "", true)
		if err == nil {
			t.Fatalf("doAdminRequest() error = nil, want credentials error")
		}
		const want = "TLS admin endpoint requires credentials, but DB URL has no password"
		if err.Error() != want {
			t.Fatalf("doAdminRequest() error = %q, want exactly %q", err.Error(), want)
		}
		if n := calls.Load(); n != 0 {
			t.Fatalf("handler call count = %d, want 0 (fail-fast before sending)", n)
		}
		if resp != nil || body != nil {
			t.Fatalf("doAdminRequest() = (%v, %v), want (nil, nil) on fail-fast", resp, body)
		}
	})

	t.Run("HTTP 4xx is translated to a client error", func(t *testing.T) {
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte("no such endpoint"))
		}))
		defer srv.Close()

		_, _, err := doAdminRequest(context.Background(), http.MethodGet, srv.URL, nil, "root", "secret", true)
		if err == nil {
			t.Fatalf("doAdminRequest() error = nil, want client error")
		}
		const want = "admin API client error: status=404 body=no such endpoint"
		if err.Error() != want {
			t.Fatalf("doAdminRequest() error = %q, want exactly %q", err.Error(), want)
		}
	})

	t.Run("HTTP 5xx is translated to a server error", func(t *testing.T) {
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("boom"))
		}))
		defer srv.Close()

		_, _, err := doAdminRequest(context.Background(), http.MethodGet, srv.URL, nil, "root", "secret", true)
		if err == nil {
			t.Fatalf("doAdminRequest() error = nil, want server error")
		}
		const want = "admin API server error: status=500 body=boom"
		if err.Error() != want {
			t.Fatalf("doAdminRequest() error = %q, want exactly %q", err.Error(), want)
		}
	})

	t.Run("error body is truncated to 200 bytes", func(t *testing.T) {
		long := strings.Repeat("x", 500)
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(long))
		}))
		defer srv.Close()

		_, _, err := doAdminRequest(context.Background(), http.MethodGet, srv.URL, nil, "root", "secret", true)
		if err == nil {
			t.Fatalf("doAdminRequest() error = nil, want client error")
		}
		want := "admin API client error: status=400 body=" + strings.Repeat("x", 200)
		if err.Error() != want {
			t.Fatalf("doAdminRequest() error = %q, want exactly %q", err.Error(), want)
		}
	})

	t.Run("connection failure is translated to a request failure", func(t *testing.T) {
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		url := srv.URL
		srv.Close() // nothing is listening any more

		_, _, err := doAdminRequest(context.Background(), http.MethodGet, url, nil, "root", "secret", true)
		if err == nil {
			t.Fatalf("doAdminRequest() error = nil, want request failure")
		}
		if !strings.HasPrefix(err.Error(), "admin request failed: ") {
			t.Fatalf("doAdminRequest() error = %q, want prefix %q", err.Error(), "admin request failed: ")
		}
	})

	t.Run("cancelled context is translated to a request failure", func(t *testing.T) {
		release := make(chan struct{})
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			<-release
		}))
		defer srv.Close()
		defer close(release)

		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			time.Sleep(50 * time.Millisecond)
			cancel()
		}()
		defer cancel()

		_, _, err := doAdminRequest(ctx, http.MethodGet, srv.URL, nil, "root", "secret", true)
		if err == nil {
			t.Fatalf("doAdminRequest() error = nil, want request failure")
		}
		if !strings.HasPrefix(err.Error(), "admin request failed: ") {
			t.Fatalf("doAdminRequest() error = %q, want prefix %q", err.Error(), "admin request failed: ")
		}
		if !strings.Contains(err.Error(), "context canceled") {
			t.Fatalf("doAdminRequest() error = %q, want it to mention context canceled", err.Error())
		}
	})

	t.Run("request without body sends no Content-Type", func(t *testing.T) {
		var gotContentType string
		var gotBody []byte
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotContentType = r.Header.Get("Content-Type")
			gotBody, _ = io.ReadAll(r.Body)
			_, _ = w.Write([]byte(`{"code":0}`))
		}))
		defer srv.Close()

		if _, _, err := doAdminRequest(context.Background(), http.MethodGet, srv.URL, nil, "root", "secret", true); err != nil {
			t.Fatalf("doAdminRequest() unexpected error: %v", err)
		}
		if gotContentType != "" {
			t.Fatalf("Content-Type = %q, want empty for a body-less request", gotContentType)
		}
		if len(gotBody) != 0 {
			t.Fatalf("request body = %q, want empty", string(gotBody))
		}
	})

	t.Run("connections are reused across calls", func(t *testing.T) {
		var mu sync.Mutex
		remotes := map[string]int{}
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			remotes[r.RemoteAddr]++
			mu.Unlock()
			_, _ = w.Write([]byte(`{"code":0}`))
		}))
		defer srv.Close()

		for i := 0; i < 2; i++ {
			if _, _, err := doAdminRequest(context.Background(), http.MethodGet, srv.URL, nil, "root", "secret", true); err != nil {
				t.Fatalf("doAdminRequest() call %d unexpected error: %v", i, err)
			}
		}
		mu.Lock()
		defer mu.Unlock()
		if len(remotes) != 1 {
			t.Fatalf("distinct client connections = %d (%v), want 1 (transport must be shared, not rebuilt per call)",
				len(remotes), remotes)
		}
	})

	t.Run("client timeout is 30 seconds", func(t *testing.T) {
		if adminRequestTimeout != 30*time.Second {
			t.Fatalf("adminRequestTimeout = %v, want %v", adminRequestTimeout, 30*time.Second)
		}
	})
}

// TestAdminErrorMatrix is the consolidated design error matrix for the admin
// module. Every row stands for one observable behaviour from Design Doc
// Decision 2-6, exercised end-to-end through doAdminRequest followed by
// parseAdminResponse (the call shape used by every admin tool).
//
// Coverage:
//   - Decisions 5/6: HTTP 4xx/5xx → translated error messages
//   - Decision 6:    HTTP 200 + body code != 0 → body code is the error
//     source even when the HTTP status is success
//   - Decision 6:    HTTP 200 + non-JSON body → "unexpected response format"
//   - Decision 5:    error bodies are truncated to 200 bytes
func TestAdminErrorMatrix(t *testing.T) {
	// longBody is 500 bytes; doAdminRequest must truncate to 200 bytes.
	longBody := strings.Repeat("x", 500)
	// longJSON is a 500-byte JSON-encoded but business-error body; the parser
	// must keep the full raw bytes intact (no truncation at the parser layer).
	longJSON := `{"code":42,"desc":"` + strings.Repeat("y", 500) + `"}`

	cases := []struct {
		name        string
		status      int
		contentType string
		body        string
		// wantErrContains is matched against the returned error message; an
		// empty string means the case must succeed.
		wantErrContains string
		// wantExactErr means the returned error message must match exactly.
		wantExactErr string
		// wantCode / wantDesc / wantBodyMatches are only checked on success.
		wantCode        int
		wantDesc        string
		wantBodyMatches string // substring that must be present in the returned body
	}{
		// --- HTTP 4xx branch (Decision 5/6 error matrix) ---
		{
			name:         "HTTP 400 Bad Request",
			status:       http.StatusBadRequest,
			body:         "bad payload",
			wantExactErr: "admin API client error: status=400 body=bad payload",
		},
		{
			name:         "HTTP 401 Unauthorized",
			status:       http.StatusUnauthorized,
			body:         "no creds",
			wantExactErr: "admin API client error: status=401 body=no creds",
		},
		{
			name:         "HTTP 403 Forbidden",
			status:       http.StatusForbidden,
			body:         "denied",
			wantExactErr: "admin API client error: status=403 body=denied",
		},
		{
			name:         "HTTP 404 Not Found",
			status:       http.StatusNotFound,
			body:         "missing",
			wantExactErr: "admin API client error: status=404 body=missing",
		},
		{
			name:         "HTTP 418 I'm a teapot",
			status:       http.StatusTeapot,
			body:         "short and stout",
			wantExactErr: "admin API client error: status=418 body=short and stout",
		},
		{
			name:         "HTTP 429 Too Many Requests",
			status:       http.StatusTooManyRequests,
			body:         "slow down",
			wantExactErr: "admin API client error: status=429 body=slow down",
		},
		// --- HTTP 5xx branch (Decision 5/6 error matrix) ---
		{
			name:         "HTTP 500 Internal Server Error",
			status:       http.StatusInternalServerError,
			body:         "boom",
			wantExactErr: "admin API server error: status=500 body=boom",
		},
		{
			name:         "HTTP 502 Bad Gateway",
			status:       http.StatusBadGateway,
			body:         "upstream gone",
			wantExactErr: "admin API server error: status=502 body=upstream gone",
		},
		{
			name:         "HTTP 503 Service Unavailable",
			status:       http.StatusServiceUnavailable,
			body:         "try again",
			wantExactErr: "admin API server error: status=503 body=try again",
		},
		{
			name:         "HTTP 504 Gateway Timeout",
			status:       http.StatusGatewayTimeout,
			body:         "upstream slow",
			wantExactErr: "admin API server error: status=504 body=upstream slow",
		},
		// --- Decision 5: error body truncation ---
		{
			name:         "HTTP 4xx long body is truncated to 200 bytes",
			status:       http.StatusBadRequest,
			body:         longBody,
			wantExactErr: "admin API client error: status=400 body=" + strings.Repeat("x", 200),
		},
		{
			name:         "HTTP 5xx long body is truncated to 200 bytes",
			status:       http.StatusInternalServerError,
			body:         longBody,
			wantExactErr: "admin API server error: status=500 body=" + strings.Repeat("x", 200),
		},
		// --- Decision 6: HTTP 200 + body code != 0 is the error path ---
		{
			name:            "HTTP 200 with code=-1 yields auth failed",
			status:          http.StatusOK,
			contentType:     "application/json",
			body:            `{"code":-1,"desc":"token expired"}`,
			wantErrContains: "auth failed: token expired",
		},
		{
			name:            "HTTP 200 with code=42 (business error) yields desc",
			status:          http.StatusOK,
			contentType:     "application/json",
			body:            `{"code":42,"desc":"bad request"}`,
			wantErrContains: "bad request",
		},
		{
			name:            "HTTP 200 with code=7 and empty desc falls back to default",
			status:          http.StatusOK,
			contentType:     "application/json",
			body:            `{"code":7}`,
			wantErrContains: "unknown error",
		},
		{
			name:            "HTTP 200 with code=1 generic error preserves desc verbatim",
			status:          http.StatusOK,
			contentType:     "application/json",
			body:            `{"code":1,"desc":"cluster not ready"}`,
			wantErrContains: "cluster not ready",
		},
		// --- Decision 6: HTTP 200 + non-JSON body ---
		{
			name:            "HTTP 200 with HTML body surfaces unexpected response format",
			status:          http.StatusOK,
			contentType:     "text/html",
			body:            "<!DOCTYPE html><html>oops</html>",
			wantErrContains: "unexpected response format: <!DOCTYPE html>",
		},
		{
			name:            "HTTP 200 with plain text body surfaces unexpected response format",
			status:          http.StatusOK,
			contentType:     "text/plain",
			body:            "not json at all",
			wantErrContains: "unexpected response format: not json at all",
		},
		{
			name:            "HTTP 200 with empty body surfaces unexpected response format",
			status:          http.StatusOK,
			contentType:     "application/json",
			body:            "",
			wantErrContains: "unexpected response format:",
		},
		{
			name:         "HTTP 200 with very long non-JSON body is truncated to 200 bytes",
			status:       http.StatusOK,
			contentType:  "text/plain",
			body:         longBody,
			wantExactErr: "unexpected response format: " + strings.Repeat("x", 200),
		},
		// --- Decision 6 happy path: HTTP 200 + code 0 succeeds ---
		{
			name:            "HTTP 200 with code=0 succeeds and preserves parsed fields",
			status:          http.StatusOK,
			contentType:     "application/json",
			body:            `{"code":0,"desc":"ok","data":{"k":"v"}}`,
			wantCode:        0,
			wantDesc:        "ok",
			wantBodyMatches: `{"code":0,"desc":"ok","data":{"k":"v"}}`,
		},
		{
			name:            "HTTP 200 with missing code defaults to success",
			status:          http.StatusOK,
			contentType:     "application/json",
			body:            `{"desc":"ok"}`,
			wantCode:        0,
			wantDesc:        "ok",
			wantBodyMatches: `{"desc":"ok"}`,
		},
		{
			name:            "HTTP 200 with long JSON business-error body preserves full raw bytes",
			status:          http.StatusOK,
			contentType:     "application/json",
			body:            longJSON,
			wantErrContains: strings.Repeat("y", 200),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.contentType != "" {
					w.Header().Set("Content-Type", tc.contentType)
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()

			// doAdminRequest owns the HTTP 4xx/5xx translation; parseAdminResponse
			// owns the body-code translation. Together they cover Decision 6
			// cleanly: HTTP 200 with a non-JSON body must fail at parseAdminResponse,
			// not at doAdminRequest.
			_, rawBody, httpErr := doAdminRequest(context.Background(), http.MethodGet, srv.URL,
				nil, "root", "secret", true)
			var (
				parsed   *AdminResponse
				finalErr error
			)
			if httpErr != nil {
				finalErr = httpErr
			} else {
				parsed, finalErr = parseAdminResponse(rawBody)
			}
			if tc.wantErrContains != "" || tc.wantExactErr != "" {
				if finalErr == nil {
					t.Fatalf("end-to-end error = nil, want error")
				}
				if tc.wantExactErr != "" && finalErr.Error() != tc.wantExactErr {
					t.Fatalf("end-to-end error = %q, want exactly %q", finalErr.Error(), tc.wantExactErr)
				}
				if tc.wantErrContains != "" && !strings.Contains(finalErr.Error(), tc.wantErrContains) {
					t.Fatalf("end-to-end error = %q, want it to contain %q", finalErr.Error(), tc.wantErrContains)
				}
				return
			}
			if finalErr != nil {
				t.Fatalf("end-to-end unexpected error: %v", finalErr)
			}
			if parsed.Code != tc.wantCode {
				t.Fatalf("parsed.Code = %d, want %d", parsed.Code, tc.wantCode)
			}
			if parsed.Desc != tc.wantDesc {
				t.Fatalf("parsed.Desc = %q, want %q", parsed.Desc, tc.wantDesc)
			}
			if tc.wantBodyMatches != "" && !strings.Contains(string(rawBody), tc.wantBodyMatches) {
				t.Fatalf("body = %q, want substring %q", string(rawBody), tc.wantBodyMatches)
			}
		})
	}
}

// TestResolveAdminBaseURLExhaustive is the consolidated regression test for
// Design Doc Decision 2 (Admin URL three-level fallback). It explicitly
// verifies every combination of:
//   - explicit header (any scheme, any port, any path)
//   - explicit flag  (any scheme, any port, any path)
//   - DB URL derived (http for non-TLS, https for TLS)
//
// plus the whitespace-trimming rules: leading/trailing whitespace is stripped,
// and whitespace-only values are treated as empty so the next level wins.
func TestResolveAdminBaseURLExhaustive(t *testing.T) {
	const (
		httpsRequire    = "postgresql://root:pw@db.example.com:26257/kwdb?sslmode=require"
		httpsVerifyCA   = "postgresql://root:pw@db.example.com:26257/kwdb?sslmode=verify-ca"
		httpsVerifyFull = "postgresql://root:pw@db.example.com:26257/kwdb?sslmode=verify-full"
		httpDisable     = "postgresql://root:pw@db.example.com:26257/kwdb?sslmode=disable"
		httpAllow       = "postgresql://root:pw@db.example.com:26257/kwdb?sslmode=allow"
		httpPrefer      = "postgresql://root:pw@db.example.com:26257/kwdb?sslmode=prefer"
		httpNoSSL       = "postgresql://root:pw@db.example.com:26257/kwdb"
	)

	cases := []struct {
		name        string
		headerValue string
		flagValue   string
		dbURL       string
		wantURL     string
		wantErr     string
	}{
		// --- Decision 2: header top precedence ---
		{
			name:        "header http scheme with custom port wins over flag and dbURL",
			headerValue: "http://admin.example.com:9090",
			flagValue:   "https://flag.example.com:8080",
			dbURL:       httpsRequire,
			wantURL:     "http://admin.example.com:9090",
		},
		{
			name:        "header https scheme with custom port wins over flag and dbURL",
			headerValue: "https://admin.example.com:8443",
			flagValue:   "http://flag.example.com:8080",
			dbURL:       httpsRequire,
			wantURL:     "https://admin.example.com:8443",
		},
		{
			name:        "header with non-standard port (26257) is preserved",
			headerValue: "http://admin.example.com:26257",
			flagValue:   "",
			dbURL:       httpsRequire,
			wantURL:     "http://admin.example.com:26257",
		},
		{
			name:        "header with path component is preserved verbatim",
			headerValue: "https://admin.example.com:8443/api/v1",
			flagValue:   "",
			dbURL:       httpsRequire,
			wantURL:     "https://admin.example.com:8443/api/v1",
		},
		{
			name:        "header with trailing slash is preserved verbatim",
			headerValue: "https://admin.example.com:8443/",
			flagValue:   "",
			dbURL:       httpsRequire,
			wantURL:     "https://admin.example.com:8443/",
		},
		{
			name:        "header with IPv4 literal host",
			headerValue: "https://10.0.0.7:8443",
			flagValue:   "",
			dbURL:       httpsRequire,
			wantURL:     "https://10.0.0.7:8443",
		},
		{
			name:        "header with IPv6 literal host is preserved verbatim",
			headerValue: "https://[::1]:8443",
			flagValue:   "",
			dbURL:       httpsRequire,
			wantURL:     "https://[::1]:8443",
		},
		{
			name:        "header with custom high port (65535)",
			headerValue: "http://admin.example.com:65535",
			flagValue:   "",
			dbURL:       httpsRequire,
			wantURL:     "http://admin.example.com:65535",
		},
		// --- Decision 2: flag second precedence ---
		{
			name:        "flag http scheme with custom port used when header empty",
			headerValue: "",
			flagValue:   "http://flag.example.com:9090",
			dbURL:       httpsRequire,
			wantURL:     "http://flag.example.com:9090",
		},
		{
			name:        "flag https scheme with custom port used when header empty",
			headerValue: "",
			flagValue:   "https://flag.example.com:8443",
			dbURL:       httpsRequire,
			wantURL:     "https://flag.example.com:8443",
		},
		{
			name:        "flag with path component is preserved verbatim",
			headerValue: "",
			flagValue:   "https://flag.example.com:8443/admin",
			dbURL:       httpsRequire,
			wantURL:     "https://flag.example.com:8443/admin",
		},
		// --- Decision 2: dbURL derivation rules ---
		{
			name:    "dbURL sslmode=require derives https on admin port 8080",
			dbURL:   httpsRequire,
			wantURL: "https://db.example.com:8080",
		},
		{
			name:    "dbURL sslmode=verify-ca derives https on admin port 8080",
			dbURL:   httpsVerifyCA,
			wantURL: "https://db.example.com:8080",
		},
		{
			name:    "dbURL sslmode=verify-full derives https on admin port 8080",
			dbURL:   httpsVerifyFull,
			wantURL: "https://db.example.com:8080",
		},
		{
			name:    "dbURL sslmode=disable derives http on admin port 8080",
			dbURL:   httpDisable,
			wantURL: "http://db.example.com:8080",
		},
		{
			name:    "dbURL sslmode=allow derives http on admin port 8080 (Decision 3 fallback)",
			dbURL:   httpAllow,
			wantURL: "http://db.example.com:8080",
		},
		{
			name:    "dbURL sslmode=prefer derives http on admin port 8080 (Decision 3 fallback)",
			dbURL:   httpPrefer,
			wantURL: "http://db.example.com:8080",
		},
		{
			name:    "dbURL without sslmode defaults to http on admin port 8080",
			dbURL:   httpNoSSL,
			wantURL: "http://db.example.com:8080",
		},
		{
			name:    "dbURL uses SQL host verbatim regardless of SQL port",
			dbURL:   "postgresql://root:pw@other-host.example.com:12345/kwdb?sslmode=require",
			wantURL: "https://other-host.example.com:8080",
		},
		{
			name:    "dbURL with IPv4 host",
			dbURL:   "postgresql://root:pw@10.0.0.7:26257/kwdb?sslmode=require",
			wantURL: "https://10.0.0.7:8080",
		},
		// --- Decision 2 / whitespace trimming ---
		{
			name:        "header leading and trailing whitespace is trimmed",
			headerValue: "   https://admin.example.com:8443\t",
			flagValue:   "",
			dbURL:       httpsRequire,
			wantURL:     "https://admin.example.com:8443",
		},
		{
			name:        "flag leading and trailing whitespace is trimmed",
			headerValue: "",
			flagValue:   "  http://flag.example.com:9090  ",
			dbURL:       httpsRequire,
			wantURL:     "http://flag.example.com:9090",
		},
		{
			name:        "header whitespace-only is treated as empty so flag wins",
			headerValue: "   \t\n",
			flagValue:   "http://flag.example.com:9090",
			dbURL:       httpsRequire,
			wantURL:     "http://flag.example.com:9090",
		},
		{
			name:        "flag whitespace-only is treated as empty so dbURL wins",
			headerValue: "",
			flagValue:   "   ",
			dbURL:       httpsRequire,
			wantURL:     "https://db.example.com:8080",
		},
		{
			name:        "header whitespace-only and flag whitespace-only falls back to dbURL",
			headerValue: "  ",
			flagValue:   "\t",
			dbURL:       httpDisable,
			wantURL:     "http://db.example.com:8080",
		},
		// --- Decision 2: error paths ---
		{
			name:    "all three empty returns error",
			wantErr: "missing admin URL and database URI",
		},
		{
			name:        "header and flag whitespace-only, dbURL empty returns error",
			headerValue: "  ",
			flagValue:   "\t",
			dbURL:       "",
			wantErr:     "missing admin URL and database URI",
		},
		{
			name:    "invalid dbURL is wrapped with X-Database-URI hint",
			dbURL:   "not-a-valid-uri",
			wantErr: "invalid X-Database-URI:",
		},
		{
			name:    "dbURL with non-postgresql scheme is wrapped with X-Database-URI hint",
			dbURL:   "http://root:pw@db.example.com:26257/kwdb",
			wantErr: "invalid X-Database-URI:",
		},
		{
			name:    "dbURL with missing user is wrapped with X-Database-URI hint",
			dbURL:   "postgresql://db.example.com:26257/kwdb",
			wantErr: "invalid X-Database-URI:",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, gotIsTLS, err := resolveAdminBaseURL(tc.headerValue, tc.flagValue, tc.dbURL)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("resolveAdminBaseURL(%q, %q, %q) = %q, want error containing %q",
						tc.headerValue, tc.flagValue, tc.dbURL, got, tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("resolveAdminBaseURL(%q, %q, %q) error = %q, want error containing %q",
						tc.headerValue, tc.flagValue, tc.dbURL, err.Error(), tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveAdminBaseURL(%q, %q, %q) unexpected error: %v",
					tc.headerValue, tc.flagValue, tc.dbURL, err)
			}
			if got != tc.wantURL {
				t.Fatalf("resolveAdminBaseURL(%q, %q, %q) = %q, want %q",
					tc.headerValue, tc.flagValue, tc.dbURL, got, tc.wantURL)
			}
			// wantIsTLS is derived from the resolved URL's scheme so each
			// case below only needs to pin the URL string; this matches
			// the contract that adminURLIsHTTPS is purely a scheme check.
			wantIsTLS := strings.HasPrefix(strings.ToLower(tc.wantURL), "https://")
			if gotIsTLS != wantIsTLS {
				t.Fatalf("resolveAdminBaseURL(%q, %q, %q) isTLS = %v, want %v",
					tc.headerValue, tc.flagValue, tc.dbURL, gotIsTLS, wantIsTLS)
			}
		})
	}
}

// TestAdminURLIsHTTPS pins down the scheme classifier used by
// resolveAdminBaseURL to surface the admin URL's https-ness to callers.
// Only the explicit "https://" prefix counts; "http://", bare host:port,
// and empty strings must all be treated as non-TLS so an insecure admin
// endpoint never gets Basic Auth attached. Whitespace trimming and
// case-insensitivity are required so operators can paste URLs from shell
// history.
func TestAdminURLIsHTTPS(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"explicit https scheme", "https://admin:8081", true},
		{"uppercase https scheme is case-insensitive", "HTTPS://ADMIN:8081", true},
		{"leading and trailing whitespace is trimmed", "  https://admin:8081  ", true},
		{"explicit http scheme", "http://admin:8081", false},
		{"bare host:port without scheme is not https", "admin:8080", false},
		{"empty string is not https", "", false},
		{"whitespace-only string is not https", "   \t\n  ", false},
		{"https prefix in path is not https", "http://admin/foo?next=https://evil", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := adminURLIsHTTPS(tc.in); got != tc.want {
				t.Fatalf("adminURLIsHTTPS(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// TestParseAdminResponseRawPayloadPreserved pins down the AdminResponse.Raw
// invariant: every successful parse must hand back the original bytes so
// downstream callers can reach into payload fields the parser does not model.
// This is exercised independently of TestParseAdminResponse so a regression
// in the Raw-keep contract cannot be masked by cousins that only check
// Code/Desc.
func TestParseAdminResponseRawPayloadPreserved(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{name: "code 0 with extras", body: `{"code":0,"desc":"ok","data":{"k":"v","n":1}}`},
		{name: "missing code (success)", body: `{"desc":"ok"}`},
		{name: "unicode payload", body: `{"code":0,"desc":"ok","node":"节点-1"}`},
		{
			name: "large payload past truncation window",
			body: `{"code":0,"desc":"ok","data":"` + strings.Repeat("z", 5000) + `"}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseAdminResponse([]byte(tc.body))
			if err != nil {
				t.Fatalf("parseAdminResponse() unexpected error: %v", err)
			}
			if !bytes.Equal(got.Raw, []byte(tc.body)) {
				t.Fatalf("Raw = %q, want exact bytes preserved", string(got.Raw))
			}
		})
	}
}

// TestAdminRawPayloadIsCopy guards the implementation contract that the raw
// payload handed back from parseAdminResponse is a copy of the input slice:
// mutating the body after the call must not affect the returned Raw. This
// protects callers that hand in slices they reuse later.
func TestAdminRawPayloadIsCopy(t *testing.T) {
	body := []byte(`{"code":0,"desc":"ok","data":{"k":"v"}}`)
	got, err := parseAdminResponse(body)
	if err != nil {
		t.Fatalf("parseAdminResponse() unexpected error: %v", err)
	}
	// Mutate the caller's slice in place.
	copy(body, []byte(`XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX`))
	if bytes.Contains(got.Raw, []byte("X")) {
		t.Fatalf("Raw was aliased to caller slice; Raw = %q", string(got.Raw))
	}
	if !bytes.Equal(got.Raw, []byte(`{"code":0,"desc":"ok","data":{"k":"v"}}`)) {
		t.Fatalf("Raw = %q, want original payload bytes", string(got.Raw))
	}
}

// TestAdminHTTPClientInsecureSkipVerify pins down Decision 5: the shared
// adminHTTPClient must skip TLS verification so self-signed KaiwuDB certs
// continue to work. Reading the Transport's TLS config is the only way to
// observe this without hitting the wire.
func TestAdminHTTPClientInsecureSkipVerify(t *testing.T) {
	tr, ok := adminHTTPClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("adminHTTPClient.Transport type = %T, want *http.Transport", adminHTTPClient.Transport)
	}
	if tr.TLSClientConfig == nil {
		t.Fatalf("adminHTTPClient.TLSClientConfig is nil, want non-nil with InsecureSkipVerify=true")
	}
	if !tr.TLSClientConfig.InsecureSkipVerify {
		t.Fatalf("InsecureSkipVerify = false, want true (Decision 5)")
	}
}
