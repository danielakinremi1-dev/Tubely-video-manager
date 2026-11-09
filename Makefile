.PHONY: up up-app down dev test test-integration smoke lint check tf-fmt tf-validate tf-lock tf-scan aws-up dev-aws smoke-aws aws-down

GOLANGCI_LINT_VERSION := v2.15.0
TERRAFORM_VERSION := 1.17.1
TRIVY_VERSION := 0.76.0

# A local terraform if installed; otherwise the pinned image, running as you
# so files it writes in infra/ aren't owned by root. The image is used only
# for offline commands (fmt, validate, lock); aws-up/aws-down need a real
# install and your AWS credentials. TF_USE_DOCKER=1 (set in CI) always uses
# the pinned image, even when the machine has some other terraform.
TERRAFORM_LOCAL := $(if $(TF_USE_DOCKER),,$(shell command -v terraform 2>/dev/null))
TERRAFORM := $(or $(TERRAFORM_LOCAL),docker run --rm -u $(shell id -u):$(shell id -g) -e HOME=/tmp -v "$(CURDIR)/infra":/infra -w /infra hashicorp/terraform:$(TERRAFORM_VERSION))
TF_DIR := $(if $(TERRAFORM_LOCAL),-chdir=infra,)

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

lint: ## golangci-lint (a local install if present, else the pinned Docker image)
	@if command -v golangci-lint >/dev/null 2>&1; then golangci-lint run ./...; \
	else docker run --rm -v "$(CURDIR)":/src:ro -w /src -v tubely-golangci-cache:/root/.cache \
		golangci/golangci-lint:$(GOLANGCI_LINT_VERSION) golangci-lint run ./...; fi

tf-fmt: ## Check Terraform formatting (offline)
	$(TERRAFORM) $(TF_DIR) fmt -check -recursive -diff

tf-validate: ## Validate the Terraform configuration (offline, no AWS credentials)
	$(TERRAFORM) $(TF_DIR) init -backend=false -input=false >/dev/null
	$(TERRAFORM) $(TF_DIR) validate

tf-lock: ## Record provider checksums for Linux and macOS in .terraform.lock.hcl
	$(TERRAFORM) $(TF_DIR) providers lock -platform=linux_amd64 -platform=linux_arm64 -platform=darwin_amd64 -platform=darwin_arm64

tf-scan: ## Security-scan infra/ with Trivy (accepted findings: infra/.trivyignore)
	docker run --rm -v "$(CURDIR)/infra":/infra:ro -w /infra aquasec/trivy:$(TRIVY_VERSION) \
		config --quiet --exit-code 1 --ignorefile .trivyignore .

aws-up: ## Create the AWS demo stack (asks before applying) and write .env.aws
	scripts/aws-up.sh

dev-aws: ## Run the server against the AWS demo stack
	@test -f .env.aws || { echo "No .env.aws: run make aws-up first" >&2; exit 1; }
	ENV_FILE=.env.aws go run ./cmd/tubely

smoke-aws: ## End-to-end check against make dev-aws (bucket audited with your AWS credentials)
	@test -f .env.aws || { echo "No .env.aws: run make aws-up first" >&2; exit 1; }
	set -a && . ./.env.aws && set +a && SMOKE_AWS=1 scripts/smoke.sh

aws-down: ## Destroy the AWS demo stack (asks first), then verify nothing is left
	scripts/aws-down.sh

check: ## Build, vet and test
	go build ./... && go vet ./... && go test ./...
