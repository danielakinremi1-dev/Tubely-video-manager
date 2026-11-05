.PHONY: up down dev test check

up: ## Start local services (S3 emulator)
	docker compose up -d

down: ## Stop local services (uploads are kept; `docker compose down -v` wipes them)
	docker compose down

dev: up ## Start services and run the server
	go run .

test: ## Run the test suite
	go test ./...

check: ## Build, vet and test
	go build ./... && go vet ./... && go test ./...
