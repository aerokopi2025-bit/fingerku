.PHONY: all build dev run test clean help install

# Binary targets and paths
BIN_DIR := bin
CLI_BIN := $(BIN_DIR)/fingerku-cli

all: build

help:
	@echo "================================================================"
	@echo "                   🛠️  Fingerku CLI & Library Tools               "
	@echo "================================================================"
	@echo "  make dev          Run background service daemon (live listener)"
	@echo "  make build        Build fingerku-cli binary to bin/fingerku-cli"
	@echo "  make install      Install fingerku-cli binary to \$$GOPATH/bin"
	@echo "  make test         Execute full test suite"
	@echo "  make clean        Remove build artifacts & temporary files"
	@echo "================================================================"

build:
	@mkdir -p $(BIN_DIR)
	@echo "🔨 Building fingerku-cli..."
	@go build -o $(CLI_BIN) ./cmd/fingerku-cli
	@echo "✅ Build complete: $(CLI_BIN)"

dev:
	@echo "================================================================"
	@echo "       🚀 Starting Fingerku Runner Daemon (CLI Mode)            "
	@echo "================================================================"
	@echo " • Config Source : SQLite DB (fingerku.db)"
	@echo " • Press Ctrl+C to stop service"
	@echo "================================================================"
	@go run ./cmd/fingerku-cli run

run: dev

install:
	@echo "📦 Installing fingerku-cli..."
	@go install ./cmd/fingerku-cli
	@echo "✅ Installed to \$$(go env GOPATH)/bin/fingerku-cli"

test:
	@echo "🧪 Running unit tests..."
	@go test -v ./...

clean:
	@echo "🧹 Cleaning up build artifacts..."
	@rm -rf $(BIN_DIR) test_*.db
