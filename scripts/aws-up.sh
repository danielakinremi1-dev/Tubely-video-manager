#!/usr/bin/env bash
# Creates the AWS demo stack with Terraform and writes .env.aws for
# `make dev-aws`. Terraform shows the plan and waits for you to type "yes".
# A short demo costs about $0; run `make aws-down` when you're done.
set -euo pipefail
cd "$(dirname "$0")/.."

need() { command -v "$1" >/dev/null || { echo "$1 is required (see docs/AWS_DEMO.md)" >&2; exit 1; }; }
need terraform
need aws
need python3

if [[ ! -f infra/terraform.tfvars ]]; then
  echo "Create infra/terraform.tfvars first:" >&2
  echo "  cp infra/terraform.tfvars.example infra/terraform.tfvars   # then set budget_email" >&2
  exit 1
fi

region=${AWS_REGION:-us-east-1}
echo "Checking AWS credentials..."
if ! identity=$(aws sts get-caller-identity --region "$region" --output json); then
  echo "No working AWS credentials. Run 'aws configure' (or 'aws sso login'), or set AWS_PROFILE." >&2
  exit 1
fi
arn=$(python3 -c 'import json, sys; print(json.load(sys.stdin)["Arn"])' <<<"$identity")
account=$(python3 -c 'import json, sys; print(json.load(sys.stdin)["Account"])' <<<"$identity")
if [[ $arn == *":root" ]]; then
  echo "Refusing to deploy with the account's root credentials. Use an IAM user or SSO (see docs/AWS_DEMO.md)." >&2
  exit 1
fi
echo "Deploying to account $account as $arn"
echo

terraform -chdir=infra init -input=false
terraform -chdir=infra apply # shows the plan, then asks

out() { terraform -chdir=infra output -raw "$1"; }
jwt_secret=$(python3 -c 'import secrets; print(secrets.token_hex(32))')
umask 077 # .env.aws holds a JWT secret
cat >.env.aws <<EOF
# Written by make aws-up on $(date -u +%Y-%m-%dT%H:%M:%SZ); removed by make aws-down.
# Used by make dev-aws (ENV_FILE=.env.aws). Git-ignored.
DB_PATH="./tubely-aws.db"
JWT_SECRET="$jwt_secret"
PLATFORM="dev"
FILEPATH_ROOT="./app"
PORT="8091"
S3_BUCKET="$(out bucket_name)"
S3_REGION="$(out region)"
S3_ENDPOINT=""
S3_FORCE_PATH_STYLE="false"
S3_ROLE_ARN="$(out app_role_arn)"
MEDIA_BASE_URL="$(out media_base_url)"
EOF

echo
echo "Stack is up. Media is served from $(out media_base_url)"
echo "Next: make dev-aws, then open http://localhost:8091/app/"
echo "When you're done: make aws-down"
