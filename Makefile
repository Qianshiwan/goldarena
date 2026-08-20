# =============================================
#  金归子 GoldArena - 黄金模拟交易游戏平台
#  Makefile for development & deployment
# =============================================
.PHONY: help dev dev-backend dev-frontend db-up db-down docker-up docker-down build clean lint test

# Colors for output
GREEN  := \033[0;32m
CYAN   := \033[0;36m
YELLOW := \033[0;33m
NC     := \033[0m

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "$(CYAN)%-20s$(NC) %s\n", $$1, $$2}'

# =============================================
#  Database & Infrastructure
# =============================================
db-up: ## Start PostgreSQL + Redis
	@echo "$(GREEN)Starting database services...$(NC)"
	docker compose up -d postgres redis

db-down: ## Stop database services
	docker compose down postgres redis

db-reset: ## Reset database (WARNING: deletes all data)
	@echo "$(YELLOW)Resetting database...$(NC)"
	docker compose down -v postgres
	docker compose up -d postgres

# =============================================
#  Development
# =============================================
dev: db-up ## Start full dev environment
	@echo "$(GREEN)Starting GoldArena development environment...$(NC)"
	@$(MAKE) dev-backend &
	@sleep 2
	@$(MAKE) dev-frontend &
	@wait

dev-backend: ## Start Go backend (gateway)
	@echo "$(GREEN)Starting Go backend on :8080...$(NC)"
	cd cmd/gateway && go run .

dev-frontend: ## Start React frontend (Vite)
	@echo "$(GREEN)Starting React frontend on :5173...$(NC)"
	cd web && npm run dev

# =============================================
#  Docker
# =============================================
docker-up: ## Start all services via Docker Compose
	docker compose up -d --build

docker-down: ## Stop all Docker services
	docker compose down

docker-logs: ## View Docker logs
	docker compose logs -f

# =============================================
#  Build
# =============================================
build: ## Build production binaries
	@echo "$(GREEN)Building backend...$(NC)"
	CGO_ENABLED=0 GOOS=linux go build -o build/gateway ./cmd/gateway
	@echo "$(GREEN)Building frontend...$(NC)"
	cd web && npm run build
	@echo "$(GREEN)Build complete!$(NC)"

build-backend: ## Build only backend
	CGO_ENABLED=0 GOOS=linux go build -o build/gateway ./cmd/gateway

build-frontend: ## Build only frontend
	cd web && npm run build

# =============================================
#  Quality
# =============================================
lint: ## Run linters
	go vet ./...
	cd web && npx eslint src/

test: ## Run tests
	go test ./...
	cd web && npm test

# =============================================
#  Utilities
# =============================================
clean: ## Clean build artifacts
	rm -rf build/ web/dist/
	go clean

deps: ## Install dependencies
	go mod tidy
	cd web && npm install

init-db: ## Initialize database schema
	@echo "$(GREEN)Initializing database schema...$(NC)"
	docker exec -i goldarena-pg psql -U goldarena -d goldarena < data/init/001_schema.sql

seed: ## Seed demo data
	@echo "$(GREEN)Seeding demo data...$(NC)"
	go run scripts/seed/main.go
