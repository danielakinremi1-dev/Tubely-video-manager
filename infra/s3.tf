# Media bucket: private, encrypted, and self-cleaning. Browsers never reach
# it directly; CloudFront reads it through Origin Access Control
# (cloudfront.tf), and the app writes with a scoped role (iam.tf).

resource "random_id" "bucket_suffix" {
  byte_length = 4
}

resource "aws_s3_bucket" "media" {
  bucket = "tubely-media-${random_id.bucket_suffix.hex}"

  # A demo stack: `make aws-down` must delete uploaded media too, or
  # destroy fails on a non-empty bucket.
  force_destroy = true
}

resource "aws_s3_bucket_public_access_block" "media" {
  bucket = aws_s3_bucket.media.id

  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

# ACLs off: the bucket owner owns every object, and access is decided only by
# policies.
resource "aws_s3_bucket_ownership_controls" "media" {
  bucket = aws_s3_bucket.media.id

  rule {
    object_ownership = "BucketOwnerEnforced"
  }
}

resource "aws_s3_bucket_server_side_encryption_configuration" "media" {
  bucket = aws_s3_bucket.media.id

  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm = "AES256" # SSE-S3: no KMS key, no KMS request charges
    }
  }
}

resource "aws_s3_bucket_lifecycle_configuration" "media" {
  bucket = aws_s3_bucket.media.id

  # Demo media doesn't need to outlive the demo, even if the stack does.
  rule {
    id     = "expire-demo-media"
    status = "Enabled"
    filter {}

    expiration {
      days = 7
    }
  }

  # Parts of a multipart upload that never completed are invisible in the
  # bucket listing but still billed. The app aborts failed uploads; this
  # catches the rest (a crash, a killed process).
  rule {
    id     = "abort-incomplete-multipart-uploads"
    status = "Enabled"
    filter {}

    abort_incomplete_multipart_upload {
      days_after_initiation = 1
    }
  }
}

data "aws_iam_policy_document" "media_bucket" {
  # Only this distribution may read objects. Without s3:ListBucket, a missing
  # key comes back from CloudFront as 403 rather than 404, so the bucket's
  # contents can't be probed.
  statement {
    sid       = "AllowCloudFrontRead"
    effect    = "Allow"
    actions   = ["s3:GetObject"]
    resources = ["${aws_s3_bucket.media.arn}/*"]

    principals {
      type        = "Service"
      identifiers = ["cloudfront.amazonaws.com"]
    }

    condition {
      test     = "StringEquals"
      variable = "AWS:SourceArn"
      values   = [aws_cloudfront_distribution.media.arn]
    }
  }

  statement {
    sid     = "DenyInsecureTransport"
    effect  = "Deny"
    actions = ["s3:*"]
    resources = [
      aws_s3_bucket.media.arn,
      "${aws_s3_bucket.media.arn}/*",
    ]

    principals {
      type        = "*"
      identifiers = ["*"]
    }

    condition {
      test     = "Bool"
      variable = "aws:SecureTransport"
      values   = ["false"]
    }
  }
}

resource "aws_s3_bucket_policy" "media" {
  bucket = aws_s3_bucket.media.id
  policy = data.aws_iam_policy_document.media_bucket.json

  # Apply the policy after public access is blocked, so there's never a
  # moment where a policy could make the bucket public.
  depends_on = [aws_s3_bucket_public_access_block.media]
}
