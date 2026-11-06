# Values `make aws-up` writes into .env.aws for the app.

output "region" {
  description = "Region of the media bucket."
  value       = var.region
}

output "bucket_name" {
  description = "Media bucket (S3_BUCKET)."
  value       = aws_s3_bucket.media.bucket
}
