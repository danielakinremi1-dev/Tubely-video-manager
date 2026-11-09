#!/usr/bin/env bash
# Creates the AWS demo stack with Terraform and writes .env.aws for
# `make dev-aws`. It shows the plan and waits for you to type "yes".
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
# In an AWS Organization, workloads belong in a member account, not the
# management account (which controls billing and every account's access).
mgmt=$(aws organizations describe-organization --region "$region" \
  --query Organization.MasterAccountId --output text 2>/dev/null || true)
if [[ -n $mgmt && $mgmt == "$account" ]]; then
  echo "Refusing to deploy into the organization's management account ($account). Use a workload member account (see docs/AWS_DEMO.md)." >&2
  exit 1
fi
echo "Deploying to account $account as $arn"
echo

terraform -chdir=infra init -input=false

# Save the plan, review it, then apply exactly that plan: nothing can change
# between what you approved and what gets created.
terraform -chdir=infra plan -input=false -out=tubely.tfplan
echo
read -r -p "Apply this plan? Type 'yes' to continue: " answer
if [[ $answer != yes ]]; then
  rm -f infra/tubely.tfplan
  echo "Nothing was created."
  exit 1
fi
terraform -chdir=infra apply -input=false tubely.tfplan
rm -f infra/tubely.tfplan

scripts/aws-write-env.sh
