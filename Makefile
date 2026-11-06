.PHONY: up down dev test test-integration check

up: ## Start local services (S3 emulator)
	docker compose up -d

down: ## Stop local services (uploads are kept; `docker compose down -v` wipes them)
	docker compose down

dev: up ## Start services and run the server
	go run ./cmd/tubely

test: ## Run the test suite
	go test ./...

test-integration: up ## Run the test suite, including S3 tests against the emulator
	TUBELY_TEST_S3_ENDPOINT=http://localhost:9090 go test -count=1 ./...

check: ## Build, vet and test
	go build ./... && go vet ./... && go test ./...
