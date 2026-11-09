terraform {
  required_version = "~> 1.17"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = ">= 6.60, < 7.0"
    }
    random = {
      source  = "hashicorp/random"
      version = "~> 3.9"
    }
  }

  # No backend block: state stays in infra/terraform.tfstate on this machine
  # (git-ignored). This stack is created for a demo and destroyed the same
  # day, so remote state (S3 + locking) would cost more than it protects.
}
