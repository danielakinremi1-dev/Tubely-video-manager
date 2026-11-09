#!/usr/bin/env bash
# Writes .env.aws from the Terraform outputs of the AWS demo stack, for
# `make dev-aws` (ENV_FILE=.env.aws). aws-up.sh runs it after the apply.
set -euo pipefail
cd "$(dirname "$0")/.."

out() { terraform -chdir=infra output -raw "$1"; }
bucket=$(out bucket_name) # fails here if the stack doesn't exist
jwt_secret=$(python3 -c 'import secrets; print(secrets.token_hex(32))')

umask 077 # .env.aws holds a JWT secret
cat >.env.aws <<ENV
# Written by scripts/aws-write-env.sh on $(date -u +%Y-%m-%dT%H:%M:%SZ); removed by make aws-down.
# Used by make dev-aws (ENV_FILE=.env.aws). Git-ignored.
DB_PATH="./tubely-aws.db"
JWT_SECRET="$jwt_secret"
PLATFORM="dev"
FILEPATH_ROOT="./app"
PORT="8091"
S3_BUCKET="$bucket"
S3_REGION="$(out region)"
S3_ENDPOINT=""
S3_FORCE_PATH_STYLE="false"
S3_ROLE_ARN="$(out app_role_arn)"
MEDIA_BASE_URL="$(out media_base_url)"
ENV

echo
echo "Wrote .env.aws. Media is served from $(out media_base_url)"
echo "Next: make dev-aws, then open http://localhost:8091/app/"
echo "When you're done: make aws-down"
