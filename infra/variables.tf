variable "region" {
  description = "AWS region for the bucket. CloudFront and Budgets are global."
  type        = string
  default     = "us-east-1"

  validation {
    condition     = can(regex("^[a-z]{2}(-[a-z]+)+-[0-9]$", var.region))
    error_message = "region must look like an AWS region, e.g. us-east-1."
  }
}

variable "budget_email" {
  description = "Where AWS Budgets sends cost alerts. Set it in infra/terraform.tfvars (git-ignored); never commit it, the repo is public."
  type        = string

  validation {
    condition     = can(regex("^[^@[:space:]]+@[^@[:space:]]+\\.[^@[:space:]]+$", var.budget_email))
    error_message = "budget_email must be an email address."
  }
}
