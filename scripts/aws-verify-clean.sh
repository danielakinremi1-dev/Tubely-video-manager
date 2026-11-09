#!/usr/bin/env bash
# Checks the AWS account for anything the Tubely demo stack creates. Exits
# non-zero if something is left. Usage: scripts/aws-verify-clean.sh [region]
#
# The Resource Groups Tagging API covers most resources, but not IAM, and it
# can list a just-deleted resource for a few minutes; the name-based checks
# below are authoritative for buckets, roles and budgets.
set -uo pipefail

region=${1:-${AWS_REGION:-us-east-1}}
left=0

check() { # check DESCRIPTION OUTPUT
  if [[ -z $2 || $2 == "None" ]]; then
    printf '  ok    no %s\n' "$1"
  else
    printf '  LEFT  %s: %s\n' "$1" "$2"
    left=1
  fi
}

account=$(aws sts get-caller-identity --region "$region" --query Account --output text) || exit 2

for r in $(printf '%s\n' "$region" us-east-1 | sort -u); do # CloudFront is listed in us-east-1
  check "resources tagged Project=tubely in $r" "$(aws resourcegroupstaggingapi get-resources \
    --region "$r" --tag-filters Key=Project,Values=tubely \
    --query 'ResourceTagMappingList[].ResourceARN' --output text)"
done
check "tubely-media-* buckets" "$(aws s3api list-buckets \
  --query "Buckets[?starts_with(Name, 'tubely-media-')].Name" --output text)"
check "tubely-demo-app-* IAM roles" "$(aws iam list-roles \
  --query "Roles[?starts_with(RoleName, 'tubely-demo-app-')].RoleName" --output text)"
check "tubely-demo-* budgets" "$(aws budgets describe-budgets --region us-east-1 --account-id "$account" \
  --query "Budgets[?starts_with(BudgetName, 'tubely-demo-')].BudgetName" --output text 2>/dev/null)"
check "Tubely CloudFront distributions" "$(aws cloudfront list-distributions \
  --query "DistributionList.Items[?Comment=='Tubely demo media'].Id" --output text)"

if ((left)); then
  echo "Something is left. If it was deleted in the last few minutes, run this again; otherwise delete it in the console." >&2
fi
exit $left
