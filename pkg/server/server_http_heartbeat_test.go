package server

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

func TestHTTPAnnouncementChannelEmitsHeartbeat(t *testing.T) {
	t.Setenv("KWDB_HTTP_HEARTBEAT_INTERVAL", "100ms")

	s, err := CreateServer("")
	if err != nil {
		t.Fatalf("CreateServer: %v", err)
	}
	t.Cleanup(Cleanup)

	addr := reserveLocalAddress(t)
	go func() {
		if err := ServeHTTP(s, addr, &HTTPTLSConfig{}); err != nil && !strings.Contains(err.Error(), "Server closed") {
			t.Errorf("ServeHTTP: %v", err)
		}
	}()

	baseURL := waitForHTTPServer(t, addr)
	httpURL := baseURL + "/mcp"

	httpTransport, err := transport.NewStreamableHTTP(httpURL)
	if err != nil {
		t.Fatalf("NewStreamableHTTP: %v", err)
	}

	c := client.NewClient(httpTransport)
	t.Cleanup(func() {
		_ = c.Close()
	})

	initReq := mcp.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcp.Implementation{
		Name:    "http-heartbeat-test",
		Version: "1.0.0",
	}

	initCtx, initCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer initCancel()

	if _, err := c.Initialize(initCtx, initReq); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	sessionID := httpTransport.GetSessionId()
	if sessionID == "" {
		t.Fatal("expected session ID after initialize")
	}

	readCtx, readCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer readCancel()

	req, err := http.NewRequestWithContext(readCtx, http.MethodGet, httpURL, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Mcp-Session-Id", sessionID)
	req.Header.Set("Mcp-Protocol-Version", mcp.LATEST_PROTOCOL_VERSION)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /mcp: %v", err)
	}
	defer resp.Body.Close()

	if got := resp.Header.Get("Content-Type"); !strings.Contains(got, "text/event-stream") {
		t.Fatalf("expected text/event-stream response, got %q", got)
	}

	reader := bufio.NewReader(resp.Body)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("expected heartbeat on announcement channel before timeout: %v", err)
		}
		if strings.Contains(line, `"method":"ping"`) {
			return
		}
	}
}

func TestHTTPHeartbeatResponsesAreNotMisclassifiedAsSampling(t *testing.T) {
	s, err := CreateServer("")
	if err != nil {
		t.Fatalf("CreateServer: %v", err)
	}
	t.Cleanup(Cleanup)

	addr := reserveLocalAddress(t)

	var serverLogs testLogger
	var httpErrLogs bytes.Buffer

	baseHTTPServer := &http.Server{
		Addr:              addr,
		ErrorLog:          log.New(&httpErrLogs, "", 0),
		ReadHeaderTimeout: time.Second,
		IdleTimeout:       30 * time.Second,
	}

	streamable := mcpserver.NewStreamableHTTPServer(
		s,
		mcpserver.WithStreamableHTTPServer(baseHTTPServer),
		mcpserver.WithHeartbeatInterval(50*time.Millisecond),
		mcpserver.WithLogger(&serverLogs),
	)

	mux := http.NewServeMux()
	mux.Handle("/mcp", streamable)
	baseHTTPServer.Handler = mux

	go func() {
		if err := streamable.Start(addr); err != nil && !strings.Contains(err.Error(), "Server closed") {
			t.Errorf("StreamableHTTPServer.Start: %v", err)
		}
	}()

	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = streamable.Shutdown(shutdownCtx)
	})

	baseURL := waitForHTTPServer(t, addr)
	httpURL := baseURL + "/mcp"

	clientTransportSpy := &requestSpyRoundTripper{base: http.DefaultTransport}
	httpTransport, err := transport.NewStreamableHTTP(
		httpURL,
		transport.WithContinuousListening(),
		transport.WithHTTPBasicClient(&http.Client{Transport: clientTransportSpy}),
	)
	if err != nil {
		t.Fatalf("NewStreamableHTTP: %v", err)
	}

	c := client.NewClient(httpTransport)
	startCtx, startCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer startCancel()
	if err := c.Start(startCtx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		_ = c.Close()
	})

	initReq := mcp.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcp.Implementation{
		Name:    "http-heartbeat-regression-test",
		Version: "1.0.0",
	}

	initCtx, initCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer initCancel()

	if _, err := c.Initialize(initCtx, initReq); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if clientTransportSpy.ServerResponseCount() > 0 {
			serverLogOutput := serverLogs.String()
			if strings.Contains(serverLogOutput, "Failed to deliver sampling response") {
				t.Fatalf("unexpected sampling delivery error log: %s", serverLogOutput)
			}
			if strings.Contains(serverLogOutput, "Failed to handle sampling response") {
				t.Fatalf("unexpected sampling handling error log: %s", serverLogOutput)
			}
			if strings.Contains(httpErrLogs.String(), "superfluous response.WriteHeader") {
				t.Fatalf("unexpected duplicate WriteHeader log: %s", httpErrLogs.String())
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}

	t.Fatalf("did not observe a heartbeat ping response from the client before timeout")
}

type testLogger struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *testLogger) Infof(format string, v ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	_, _ = io.WriteString(&l.buf, "INFO: "+sprintf(format, v...)+"\n")
}

func (l *testLogger) Errorf(format string, v ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	_, _ = io.WriteString(&l.buf, "ERROR: "+sprintf(format, v...)+"\n")
}

func (l *testLogger) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}

type requestSpyRoundTripper struct {
	base          http.RoundTripper
	mu            sync.Mutex
	serverReplies int
}

func (r *requestSpyRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	isServerResponse := false
	if req.Method == http.MethodPost && req.Body != nil {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		req.Body = io.NopCloser(bytes.NewReader(body))

		isServerResponse = !bytes.Contains(body, []byte(`"method"`)) &&
			(bytes.Contains(body, []byte(`"result"`)) || bytes.Contains(body, []byte(`"error"`)))
	}

	base := r.base
	if base == nil {
		base = http.DefaultTransport
	}
	resp, err := base.RoundTrip(req)
	if err == nil && isServerResponse {
		r.mu.Lock()
		r.serverReplies++
		r.mu.Unlock()
	}
	return resp, err
}

func (r *requestSpyRoundTripper) ServerResponseCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.serverReplies
}

func sprintf(format string, v ...any) string {
	return strings.TrimSpace(strings.ReplaceAll(fmt.Sprintf(format, v...), "\n", " "))
}

func reserveLocalAddress(t *testing.T) string {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	defer ln.Close()

	return ln.Addr().String()
}

func waitForHTTPServer(t *testing.T, addr string) string {
	t.Helper()

	baseURL := "http://" + addr
	deadline := time.Now().Add(3 * time.Second)

	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/mcp", nil)
		if err != nil {
			cancel()
			t.Fatalf("NewRequest: %v", err)
		}

		resp, err := http.DefaultClient.Do(req)
		cancel()
		if err == nil {
			resp.Body.Close()
			return baseURL
		}

		time.Sleep(50 * time.Millisecond)
	}

	t.Fatalf("server at %s did not become ready", baseURL)
	return ""
}
