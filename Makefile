.PHONY: build build-server build-sentinel run run-standalone run-replication run-ha clean help

# Build both server and sentinel
build: build-server build-sentinel

# Build Redis server
build-server:
	@echo "Building Redis server..."
	@mkdir -p bin
	@go build -o bin/redis-server ./cmd/server
	@echo "✓ Redis server built: bin/redis-server"

# Build Sentinel
build-sentinel:
	@echo "Building Sentinel..."
	@mkdir -p bin
	@go build -o bin/redis-sentinel ./cmd/sentinel
	@echo "✓ Sentinel built: bin/redis-sentinel"

# Alias for run-standalone
run: run-standalone

# Run standalone server (development)
run-standalone: build-server
	@echo "🚀 Starting standalone Redis server..."
	@echo ""
	@./bin/redis-server --port 6379

# Run master + 2 replicas (replication setup)
run-replication: build-server
	@echo "========================================="
	@echo "🔄 Starting Master-Replica Setup"
	@echo "========================================="
	@echo ""
	@pkill redis-server 2>/dev/null || true
	@sleep 1
	@mkdir -p logs
	@rm -f logs/master-6379.log logs/replica-6380.log logs/replica-6381.log
	@echo "[1/3] Starting Master (port 6379)..."
	@./bin/redis-server --port 6379 > logs/master-6379.log 2>&1 &
	@sleep 2
	@echo "[2/3] Starting Replica 1 (port 6380)..."
	@./bin/redis-server --port 6380 \
		--replication-role replica \
		--replication-master-host 127.0.0.1 \
		--replication-master-port 6379 \
		> logs/replica-6380.log 2>&1 &
	@sleep 2
	@echo "[3/3] Starting Replica 2 (port 6381)..."
	@./bin/redis-server --port 6381 \
		--replication-role replica \
		--replication-master-host 127.0.0.1 \
		--replication-master-port 6379 \
		> logs/replica-6381.log 2>&1 &
	@sleep 2
	@echo ""
	@echo "✅ Master-Replica Setup Complete!"
	@echo "========================================="
	@echo "  Master:    localhost:6379 (log: logs/master-6379.log)"
	@echo "  Replica 1: localhost:6380 (log: logs/replica-6380.log)"
	@echo "  Replica 2: localhost:6381 (log: logs/replica-6381.log)"
	@echo ""
	@echo "Test replication:"
	@echo "  redis-cli -p 6379 SET mykey 'Hello'"
	@echo "  redis-cli -p 6380 GET mykey  # Should return 'Hello'"
	@echo ""
	@echo "View logs:"
	@echo "  tail -f logs/master-6379.log"
	@echo ""
	@echo "Stop all: make clean"
	@echo "========================================="

# Run full HA setup (master + replicas + sentinels)
run-ha: build-server build-sentinel
	@echo "========================================="
	@echo "🛡️  Starting High Availability Setup"
	@echo "========================================="
	@echo ""
	@pkill redis-server 2>/dev/null || true
	@pkill redis-sentinel 2>/dev/null || true
	@sleep 1
	@mkdir -p logs
	@rm -f logs/master-6379.log logs/replica-6380.log logs/replica-6381.log
	@rm -f logs/sentinel-26379.log logs/sentinel-26380.log logs/sentinel-26381.log
	@echo "Step 1: Starting Redis Instances"
	@echo "-----------------------------------"
	@echo "[1/3] Master (port 6379)..."
	@./bin/redis-server --port 6379 > logs/master-6379.log 2>&1 &
	@sleep 2
	@echo "[2/3] Replica 1 (port 6380)..."
	@./bin/redis-server --port 6380 \
		--replication-role replica \
		--replication-master-host 127.0.0.1 \
		--replication-master-port 6379 \
		> logs/replica-6380.log 2>&1 &
	@sleep 2
	@echo "[3/3] Replica 2 (port 6381)..."
	@./bin/redis-server --port 6381 \
		--replication-role replica \
		--replication-master-host 127.0.0.1 \
		--replication-master-port 6379 \
		> logs/replica-6381.log 2>&1 &
	@sleep 2
	@echo ""
	@echo "Step 2: Starting Sentinel Cluster"
	@echo "-----------------------------------"
	@echo "[1/3] Sentinel 1 (port 26379)..."
	@./bin/redis-sentinel \
		--port 26379 \
		--master-name mymaster \
		--master-host 127.0.0.1 \
		--master-port 6379 \
		--quorum 2 \
		--sentinel-addrs "127.0.0.1:26380,127.0.0.1:26381" \
		> logs/sentinel-26379.log 2>&1 &
	@sleep 2
	@echo "[2/3] Sentinel 2 (port 26380)..."
	@./bin/redis-sentinel \
		--port 26380 \
		--master-name mymaster \
		--master-host 127.0.0.1 \
		--master-port 6379 \
		--quorum 2 \
		--sentinel-addrs "127.0.0.1:26379,127.0.0.1:26381" \
		> logs/sentinel-26380.log 2>&1 &
	@sleep 2
	@echo "[3/3] Sentinel 3 (port 26381)..."
	@./bin/redis-sentinel \
		--port 26381 \
		--master-name mymaster \
		--master-host 127.0.0.1 \
		--master-port 6379 \
		--quorum 2 \
		--sentinel-addrs "127.0.0.1:26379,127.0.0.1:26380" \
		> logs/sentinel-26381.log 2>&1 &
	@sleep 3
	@echo ""
	@echo "✅ High Availability Setup Complete!"
	@echo "========================================="
	@echo ""
	@echo "📊 Redis Instances:"
	@echo "  Master:    localhost:6379 (log: logs/master-6379.log)"
	@echo "  Replica 1: localhost:6380 (log: logs/replica-6380.log)"
	@echo "  Replica 2: localhost:6381 (log: logs/replica-6381.log)"
	@echo ""
	@echo "🛡️  Sentinel Cluster:"
	@echo "  Sentinel 1: localhost:26379 (log: logs/sentinel-26379.log)"
	@echo "  Sentinel 2: localhost:26380 (log: logs/sentinel-26380.log)"
	@echo "  Sentinel 3: localhost:26381 (log: logs/sentinel-26381.log)"
	@echo ""
	@echo "⚙️  Configuration:"
	@echo "  Master Name: mymaster"
	@echo "  Quorum: 2/3 (2 Sentinels must agree for failover)"
	@echo "  Down After: 30 seconds"
	@echo ""
	@echo "🧪 Test Automatic Failover:"
	@echo "  1. Check current master:"
	@echo "     redis-cli -p 26379 SENTINEL GET-MASTER-ADDR-BY-NAME mymaster"
	@echo ""
	@echo "  2. Kill master to trigger failover:"
	@echo "     pkill -9 -f 'redis-server --port 6379'"
	@echo ""
	@echo "  3. Wait 35 seconds and check new master:"
	@echo "     redis-cli -p 26379 SENTINEL GET-MASTER-ADDR-BY-NAME mymaster"
	@echo ""
	@echo "📋 Monitor Activity:"
	@echo "  Sentinel logs: tail -f logs/sentinel-26379.log"
	@echo "  Sentinel info: redis-cli -p 26379 SENTINEL MASTERS"
	@echo ""
	@echo "🧹 Cleanup: make clean"
	@echo "========================================="

clean:
	@echo "Cleaning build artifacts..."
	@rm -rf bin/
	@rm -rf logs/
	@rm -f *.log *.rdb *.aof
	@pkill redis-sentinel 2>/dev/null || true
	@pkill redis-server 2>/dev/null || true
	@echo "✓ Clean complete"

# Help
help:
	@echo "========================================="
	@echo "🚀 Redis Makefile Commands"
	@echo "========================================="
	@echo ""
	@echo "📦 Build:"
	@echo "  make                  - Build both server and sentinel"
	@echo "  make build-server     - Build Redis server only"
	@echo "  make build-sentinel   - Build Sentinel only"
	@echo ""
	@echo "▶️  Run:"
	@echo "  make run-standalone   - Single server (port 6379)"
	@echo "  make run-replication  - Master + 2 replicas"
	@echo "  make run-ha           - Full HA (master + replicas + sentinels)"
	@echo ""
	@echo "🧪 Testing:"
	@echo "  make test             - Run tests"
	@echo "  make fmt              - Format code"
	@echo "  make vet              - Run go vet"
	@echo "  make lint             - Run golangci-lint"
	@echo ""
	@echo "🛠️  Maintenance:"
	@echo "  make deps             - Download dependencies"
	@echo "  make clean            - Stop all processes & clean artifacts"
	@echo "  make help             - Show this help"
	@echo ""
	@echo "========================================="
	@echo "📖 Quick Start Examples:"
	@echo "========================================="
	@echo ""
	@echo "1️⃣  Development (standalone):"
	@echo "   make run-standalone"
	@echo "   redis-cli -p 6379"
	@echo ""
	@echo "2️⃣  Replication (read scaling):"
	@echo "   make run-replication"
	@echo "   redis-cli -p 6379 SET key value  # Write to master"
	@echo "🛠️  Maintenance:
