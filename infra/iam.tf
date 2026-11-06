# The app's identity on AWS: a role with write access to the media bucket
# and nothing else, assumed for temporary credentials (an hour, refreshed by
# the SDK). No IAM user or access key is created, so there's no long-lived
# secret to leak, and nothing secret lands in Terraform state.

data "aws_caller_identity" "current" {}

# The IAM identity behind the current credentials. For an assumed-role or SSO
# session this resolves to the role's ARN (including its path), which a
# trust policy can name; the raw sts:assumed-role ARN can't be.
data "aws_iam_session_context" "current" {
  arn = data.aws_caller_identity.current.arn
}

data "aws_iam_policy_document" "app_trust" {
  statement {
    sid     = "OnlyTheDeployerMayAssume"
    actions = ["sts:AssumeRole"]

    principals {
      type        = "AWS"
      identifiers = [data.aws_iam_session_context.current.issuer_arn]
    }
  }
}

resource "aws_iam_role" "app" {
  name                 = "tubely-demo-app-${random_id.bucket_suffix.hex}"
  description          = "Tubely app: write and delete media objects in the demo bucket"
  assume_role_policy   = data.aws_iam_policy_document.app_trust.json
  max_session_duration = 3600
}

# What the app actually calls: PutObject (which also covers the multipart
# upload calls), DeleteObject (deletes and re-uploads), and
# AbortMultipartUpload (the transfer manager cleaning up a failed upload).
# No GetObject: CloudFront serves media, the app never reads it. No
# ListBucket: the app always knows its keys.
data "aws_iam_policy_document" "app" {
  statement {
    sid = "WriteMediaObjects"
    actions = [
      "s3:PutObject",
      "s3:DeleteObject",
      "s3:AbortMultipartUpload",
    ]
    resources = ["${aws_s3_bucket.media.arn}/*"]
  }
}

resource "aws_iam_role_policy" "app" {
  name   = "media-objects"
  role   = aws_iam_role.app.id
  policy = data.aws_iam_policy_document.app.json
}
