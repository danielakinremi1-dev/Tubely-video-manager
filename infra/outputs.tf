# Values `make aws-up` writes into .env.aws for the app.

output "region" {
  description = "Region of the media bucket."
  value       = var.region
}

output "bucket_name" {
  description = "Media bucket (S3_BUCKET)."
  value       = aws_s3_bucket.media.bucket
}

output "cloudfront_domain" {
  description = "CloudFront domain that serves media."
  value       = aws_cloudfront_distribution.media.domain_name
}

output "media_base_url" {
  description = "Public base URL for object keys (MEDIA_BASE_URL)."
  value       = "https://${aws_cloudfront_distribution.media.domain_name}"
}
