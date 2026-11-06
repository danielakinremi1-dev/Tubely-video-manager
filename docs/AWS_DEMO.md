# Running Tubely on AWS (demo stack)

Tubely runs locally against an S3 emulator by default. This runbook brings up the real AWS version (a private S3 bucket behind CloudFront), uses it for a demo, and tears it down. The whole stack is Terraform in [`infra/`](../infra), created with one command and destroyed with one command.

**Nothing here is left running.** The stack exists for the length of a demo, usually under an hour.

## What gets created

| Resource | Purpose |
|---|---|
| S3 bucket `tubely-media-<random>` | Private media storage. All public access is blocked, ACLs are off, objects are encrypted (SSE-S3), non-TLS requests are denied, objects expire after 7 days, and incomplete multipart uploads are aborted after 1 day. |
| CloudFront distribution + Origin Access Control | Serves media over HTTPS from edge caches. It's the only thing allowed to read the bucket. `PriceClass_100` (North America and Europe edges). |
| IAM role `tubely-demo-app-<random>` | The app's identity. It can put, delete and abort uploads in the bucket, and nothing else. Only the identity that ran `make aws-up` can assume it, and it gets 1-hour credentials. No access keys are created. |
| AWS Budgets budget `tubely-demo-<random>` | Emails you at $1 of actual monthly spend and at $5 of forecast spend. It watches the whole account. |

Every resource is tagged `Project=tubely`, `Environment=demo`, `ManagedBy=terraform`. Terraform state stays on your machine in `infra/terraform.tfstate` (git-ignored).

The app itself runs on your machine (`make dev-aws`), not on AWS. Only media storage and delivery are on AWS.

## Cost

A demo (a few uploads and plays, torn down the same day) should cost about **$0**. S3 storage for a few MB for a few hours, a handful of requests, and a little CloudFront transfer come to fractions of a cent, and new accounts' free tiers usually cover them. Check what your account gets.

The real risks are forgetting to tear down and strangers downloading media through the CloudFront URL. These cover them:
- objects expire after 7 days;
- the budget alert emails you;
- `make aws-down` verifies that nothing is left.

Billing data lags by several hours, so the budget is a safety net for a forgotten stack, not a real-time meter.

Deliberately left out to keep the cost at $0: AWS WAF (billed monthly), CloudFront and S3 access logs, a custom domain (Route 53 and ACM), and remote Terraform state.

## Prerequisites

1. **Terraform 1.17** or newer. On Ubuntu or WSL:
   ```bash
   wget -O- https://apt.releases.hashicorp.com/gpg | sudo gpg --dearmor -o /usr/share/keyrings/hashicorp-archive-keyring.gpg
   echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/hashicorp-archive-keyring.gpg] https://apt.releases.hashicorp.com $(lsb_release -cs) main" | sudo tee /etc/apt/sources.list.d/hashicorp.list
   sudo apt update && sudo apt install terraform
   ```
   On macOS: `brew tap hashicorp/tap && brew install hashicorp/tap/terraform`.
2. **AWS CLI v2**, with credentials for an IAM user or an SSO session that can create the resources above (for example, `AdministratorAccess` on a personal account). Root credentials are refused: they can't assume roles, and they shouldn't be used day to day. Check with `aws sts get-caller-identity`. To use a named profile, `export AWS_PROFILE=<name>` before the steps below.
3. Everything for local development (Go, ffmpeg); see the [README](../README.md).

One-time setup:
```bash
cp infra/terraform.tfvars.example infra/terraform.tfvars   # then set budget_email
```
`terraform.tfvars` is git-ignored. Don't commit it, because the repository is public.

## Demo

1. **Create the stack:**
   ```bash
   make aws-up
   ```
   This checks your credentials, shows the Terraform plan and **waits for you to type `yes`**. CloudFront takes about 5–15 minutes to deploy. When it finishes, it writes `.env.aws` (git-ignored) with the bucket, region, role, CloudFront URL and a fresh JWT secret.

2. **Run the app against it:**
   ```bash
   make dev-aws           # ENV_FILE=.env.aws go run ./cmd/tubely
   ```
   Open http://localhost:8091/app/, sign up, create a draft, upload a video from `samples/` and a thumbnail, then play and seek. The video URL is `https://<id>.cloudfront.net/landscape/<uuid>.mp4`. The demo uses its own database, `tubely-aws.db`, so local test data stays separate.

   For a full end-to-end check, run `make smoke-aws` in another terminal. It audits the bucket with your credentials, since the bucket is private.

3. **Record it:** a short GIF of upload and playback for the README, plus screenshots of the CloudFront distribution and the bucket's "Block all public access" settings.

4. **Tear down:**
   ```bash
   make aws-down
   ```
   This shows what will be deleted and waits for `yes`. Disabling and deleting the CloudFront distribution takes several minutes. Then it checks the account for anything left (tagged resources in your region and us-east-1, buckets, IAM roles, budgets and distributions by name) and deletes `.env.aws`.

## Troubleshooting

- **CloudFront returns 403 for a video right after `make aws-up`.** The distribution or the bucket policy is still propagating. Wait a few minutes.
- **A deleted or replaced video returns 403, not 404.** That's expected: without `s3:ListBucket`, a missing object can't be told apart from a forbidden one, so nobody can probe which keys exist.
- **A deleted video still plays for a while.** CloudFront caches media at the edge, and Tubely doesn't send cache invalidations, because keys are unguessable UUIDs that are never reused. The edge copy ages out, and the bucket object is already gone.
- **Uploads fail with `AccessDenied` on `AssumeRole`.** The role trusts only the identity that ran `make aws-up`. Run `make dev-aws` with the same credentials (the same `AWS_PROFILE`).
- **`make aws-down` reports something left right after destroy.** The tagging API can list a just-deleted resource for a few minutes. Run `scripts/aws-verify-clean.sh` again; if it's still there, delete it in the console.
- **Budget emails.** AWS Budgets sends them only when a threshold is crossed, so silence is the normal case.
