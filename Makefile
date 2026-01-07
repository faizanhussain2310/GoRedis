.PHONY: build build-server build-sentinel run test clean run-sentinel run-cluster help

# Build both server and sentinel
build: build-server build-sentinel

# Build Redis server
build-server:
	@echo "Building Redis server..."
	@go build -o bin/redis-server ./cmd/server
	@echo "✓ Redis server built: bin/redis-server"

# Build Sentinel
build-sentinel:
	@echo "Building Sentinel..."
	@go build -o bin/redis-sentinel ./cmd/sentinel
	@echo "✓ Sentinel built: bin/redis-sentinel"

run: build-server
	./bin/redis-server

# Run standalone Sentinel
run-sentinel: build-sentinel
	@echo "Starting Sentinel on port 26379..."
	@./bin/redis-sentinel \
		--port 26379 \
		--master-name mymaster \
		--master-host 127.0.0.1 \
		--master-port 6379 \
		--quorum 1

# Run complete cluster (1 master + 2 replicas + 3 sentinels)
run-cluster: build-server build-sentinel
	@echo "Starting complete Sentinel cluster..."
	@chmod +x examples/sentinel_cluster.sh
	@bash examples/sentinel_cluster.sh

test:
	go test -v ./...

clean:
	@echo "Cleaning build artifacts..."
	@rm -rf bin/
	@rm -f *.log *.rdb *.aof
	@echo "✓ Clean complete"

fmt:
	go fmt ./...

vet:
	go vet ./...

# Help
help:
	@echo "Redis Makefile Commands:"
	@echo ""
	@echo "  make                 - Build both server and sentinel"
	@echo "  make build-server    - Build Redis server only"
	@echo "  make build-sentinel  - Build Sentinel only"
	@echo "  make run             - Build and run Redis server"
	@echo "  make run-sentinel    - Build and run standalone Sentinel"
	@echo "  make run-cluster     - Start full cluster (master + replicas + sentinels)"
	@echo "  make test            - Run tests"
	@echo "  make clean           - Remove build artifacts"
	@echo "  make fmt             - Format code"
	@echo "  make vet             - Run go vet"
	@echo "  make help            - Show this help message"
	@echo ""
	@echo "Examples:"
	@echo "  # Build and run single server"
	@echo "  make run"
	@echo ""
	@echo "  # Run standalone Sentinel"
	@echo "  make run-sentinel"
	@echo ""
	@echo "  # Run complete HA cluster"
	@echo "  make run-cluster"


lint:
	golangci-lint run

deps:
	go mod download
	go mod tidy
