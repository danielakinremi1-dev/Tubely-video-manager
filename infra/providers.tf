provider "aws" {
  region = var.region

  # Every resource that supports tags gets these, so the whole stack can be
  # found (and its absence verified after `make aws-down`) by Project=tubely.
  default_tags {
    tags = {
      Project     = "tubely"
      Environment = "demo"
      ManagedBy   = "terraform"
      Repository  = "github.com/danielakinremi1-dev/Tubely-video-manager"
    }
  }
}
