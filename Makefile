.PHONY: all build dev run serve api test clean help install

# Binary targets and paths
BIN_DIR := bin
CLI_BIN := $(BIN_DIR)/fingerku-cli
API_BIN := $(BIN_DIR)/fingerku-api

all: build

help:
	@echo "================================================================"
	@echo "                   🛠️  Fingerku CLI & REST API Tools              "
	@echo "================================================================"
	@echo "  make dev          Run background service daemon (live listener)"
	@echo "  make serve        Start REST API server with Chi (port 8080)"
	@echo "  make build        Build all binaries (fingerku-cli & fingerku-api)"
	@echo "  make install      Install binaries to \$$GOPATH/bin"
	@echo "  make test         Execute full test suite"
	@echo "  make clean        Remove build artifacts & temporary files"
	@echo "================================================================"

build:
	@mkdir -p $(BIN_DIR)
	@echo "🔨 Building fingerku-cli..."
	@go build -o $(CLI_BIN) ./cmd/fingerku-cli
	@echo "🔨 Building fingerku-api..."
	@go build -o $(API_BIN) ./cmd/fingerku-api
	@echo "✅ Build complete: $(CLI_BIN), $(API_BIN)"

dev:
	@echo "================================================================"
	@echo "       🚀 Starting Fingerku Runner Daemon (CLI Mode)            "
	@echo "================================================================"
	@echo " • Config Source : SQLite DB (fingerku.db)"
	@echo " • Press Ctrl+C to stop service"
	@echo "================================================================"
	@go run ./cmd/fingerku-cli run

serve:
	@echo "================================================================"
	@echo "       🚀 Starting Fingerku REST API Server (Powered by Chi)    "
	@echo "================================================================"
	@go run ./cmd/fingerku-api --port 8080

api: serve

run: dev

install:
	@echo "📦 Installing fingerku-cli & fingerku-api..."
	@go install ./cmd/fingerku-cli ./cmd/fingerku-api
	@echo "✅ Installed to \$$(go env GOPATH)/bin"

test:
	@echo "🧪 Running unit tests..."
	@go test -v ./...

clean:
	@echo "🧹 Cleaning up build artifacts..."
	@rm -rf $(BIN_DIR) test_*.db
