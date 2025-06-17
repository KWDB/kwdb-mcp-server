.PHONY: build run clean test test-integration test-sse-integration test-http-integration

# Build the application
build:
	go build -o bin/kwdb-mcp-server cmd/kwdb-mcp-server/main.go

# Run the application
run: build
	./bin/kwdb-mcp-server $(CONNECTION_STRING)

# Run the application in SSE mode
run-sse: build
	./bin/kwdb-mcp-server -t sse -p $(PORT) $(CONNECTION_STRING)

# Run the application in HTTP mode
run-http: build
	./bin/kwdb-mcp-server -t http -p $(PORT) $(CONNECTION_STRING)

# Clean build artifacts
clean:
	rm -rf bin/

# Install dependencies
deps:
	go mod tidy 

install:
	cp bin/kwdb-mcp-server /usr/local/bin/kwdb-mcp-server

# Run all unit tests
test:
	go test -v ./pkg/db ./pkg/tools ./pkg/server ./pkg/prompts 

# Run integration tests
test-integration:
	CONNECTION_STRING=$(CONNECTION_STRING) go test -v ./tests/integration_test.go

# Run SSE integration tests
test-sse-integration:
	BASE_URL=$(BASE_URL) go test -v ./tests/sse_integration_test.go

# Run HTTP integration tests
test-http-integration:
	BASE_URL=$(BASE_URL) go test -v ./tests/http_integration_test.go

