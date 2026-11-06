variable "region" {
  description = "AWS region for the bucket. CloudFront and Budgets are global."
  type        = string
  default     = "us-east-1"

  validation {
    condition     = can(regex("^[a-z]{2}(-[a-z]+)+-[0-9]$", var.region))
    error_message = "region must look like an AWS region, e.g. us-east-1."
  }
}
