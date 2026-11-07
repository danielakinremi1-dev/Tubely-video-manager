#!/usr/bin/env bash
# Destroys the AWS demo stack (it shows the plan and asks first), then
# checks that nothing belonging to it is left in the account.
set -euo pipefail
cd "$(dirname "$0")/.."

command -v terraform >/dev/null || { echo "terraform is required (see docs/AWS_DEMO.md)" >&2; exit 1; }

region=$(terraform -chdir=infra output -raw region 2>/dev/null || echo "${AWS_REGION:-us-east-1}")
terraform -chdir=infra plan -destroy -input=false -out=destroy.tfplan
echo
read -r -p "Destroy everything listed above? Type 'yes' to continue: " answer
if [[ $answer != yes ]]; then
  rm -f infra/destroy.tfplan
  echo "Nothing was deleted."
  exit 1
fi
terraform -chdir=infra apply -input=false destroy.tfplan
rm -f infra/destroy.tfplan .env.aws

echo
echo "Checking that nothing is left..."
scripts/aws-verify-clean.sh "$region"
echo
echo "Kept tubely-aws.db (local users and video rows from the demo). Delete it if you like."
