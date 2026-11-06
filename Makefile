.PHONY: up up-app down dev test test-integration smoke check

up: ## Start local services (S3 emulator)
	docker compose up -d

up-app: ## Build and start the API in Docker too (http://localhost:8091/app/)
	docker compose --profile app up -d --build

down: ## Stop local services (uploads are kept; `docker compose down -v` wipes them)
	docker compose --profile app down

dev: up ## Start services and run the server
	go run ./cmd/tubely

test: ## Run the test suite
	go test ./...

test-integration: up ## Run the test suite, including S3 tests against the emulator
	TUBELY_TEST_S3_ENDPOINT=http://localhost:9090 go test -count=1 ./...

smoke: ## End-to-end check against the running containers (run make up-app first)
	SMOKE_TMP_CMD='docker compose exec -T app sh -c "ls -A /tmp"' scripts/smoke.sh

check: ## Build, vet and test
	go build ./... && go vet ./... && go test ./...
