#!/usr/bin/env bash
# Branch protection for main, as code. Safe to re-run: it sets the whole
# rule each time. Needs `gh auth login` as a repository admin.
#
#   scripts/protect-main.sh                 # this repository
#   REPO=owner/name scripts/protect-main.sh # a fork
#
# What it enforces:
# - Changes reach main only through a pull request. No approvals are
#   required, because a solo maintainer can't approve their own PR.
# - The CI jobs lint, test and terraform must pass, on a branch that's up
#   to date with main. The checks are tied to the GitHub Actions app, so
#   nothing else can post a passing status under the same name.
# - The rule applies to admins too, and main can't be force-pushed or deleted.
set -euo pipefail

repo=${REPO:-danielakinremi1-dev/Tubely-video-manager}
github_actions_app_id=15368

command -v gh >/dev/null || { echo "gh (GitHub CLI) is required" >&2; exit 1; }

gh api --method PUT "repos/$repo/branches/main/protection" --input - >/dev/null <<JSON
{
  "required_status_checks": {
    "strict": true,
    "checks": [
      {"context": "lint", "app_id": $github_actions_app_id},
      {"context": "test", "app_id": $github_actions_app_id},
      {"context": "terraform", "app_id": $github_actions_app_id}
    ]
  },
  "enforce_admins": true,
  "required_pull_request_reviews": {
    "required_approving_review_count": 0
  },
  "restrictions": null,
  "allow_force_pushes": false,
  "allow_deletions": false
}
JSON

echo "main is protected on $repo:"
gh api "repos/$repo/branches/main/protection" --jq '
  "  required checks: \(.required_status_checks.checks | map(.context) | join(", ")) (up to date: \(.required_status_checks.strict))",
  "  pull request required: \(.required_pull_request_reviews != null) (approvals: \(.required_pull_request_reviews.required_approving_review_count))",
  "  applies to admins: \(.enforce_admins.enabled)",
  "  force pushes: \(.allow_force_pushes.enabled), deletion: \(.allow_deletions.enabled)"'
