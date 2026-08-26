.PHONY: all build build-ui dev dev-ui dev-api serve api test clean help install

# Binary targets and paths
BIN_DIR := bin
API_BIN := $(BIN_DIR)/fingerku-api

all: build

help:
	@echo "================================================================"
	@echo "          🛠️  Fingerku REST API & Vite + Tailwind v4 UI          "
	@echo "================================================================"
	@echo "  make dev          Run Backend API & Vite Dev Server concurrently"
	@echo "  make dev-ui       Start Vite Frontend Dev Server with HMR (port 5173)"
	@echo "  make dev-api      Start Backend Go API Server (port 8080)"
	@echo "  make serve        Start production server with embedded UI"
	@echo "  make build        Build standalone binary (Go + Vite UI)"
	@echo "  make install      Install binary to \$$GOPATH/bin"
	@echo "  make test         Execute full test suite"
	@echo "  make clean        Remove build artifacts & temporary files"
	@echo "================================================================"

build-ui:
	@echo "⚡ Bundling Tailwind CSS v4 & Vite assets..."
	@cd web && npm run build
	@echo "✅ UI Assets bundled into api/static/"

build: build-ui
	@mkdir -p $(BIN_DIR)
	@echo "🔨 Building standalone fingerku-api..."
	@go build -o $(API_BIN) ./cmd/fingerku-api
	@echo "✅ Standalone Build complete: $(API_BIN)"

dev-ui:
	@echo "⚡ Starting Vite Dev Server with instant HMR (port 5173)..."
	@cd web && npm run dev

dev-api:
	@go run ./cmd/fingerku-api --port 8080

dev:
	@echo "================================================================"
	@echo " 🚀 Starting Fingerku Fullstack Developer Environment "
	@echo "================================================================"
	@echo " • Backend API : http://localhost:8080"
	@echo " • Frontend UI : http://localhost:5173 (Vite HMR Live Reload)"
	@echo " • Press Ctrl+C to stop all services"
	@echo "================================================================"
	@bash -c 'set -m; trap "kill -- -$$ 2>/dev/null; wait" SIGINT SIGTERM EXIT; (go run ./cmd/fingerku-api --port 8080) & pid1=$$!; (cd web && npm run dev) & pid2=$$!; wait $$pid1 $$pid2'

serve: build-ui
	@go run ./cmd/fingerku-api --port 8080

api: dev-api

install: build-ui
	@echo "📦 Installing fingerku-api..."
	@go install ./cmd/fingerku-api
	@echo "✅ Installed to \$$(go env GOPATH)/bin/fingerku-api"

test:
	@echo "🧪 Running unit tests..."
	@go test -v ./...

clean:
	@echo "🧹 Cleaning up build artifacts..."
	@rm -rf $(BIN_DIR) test_*.db
