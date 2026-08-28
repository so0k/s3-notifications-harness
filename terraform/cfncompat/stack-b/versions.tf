terraform {
  required_version = ">= 1.15"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 6.0"
    }
    archive = {
      source = "hashicorp/archive"
    }
    cfncompat = {
      source  = "cdktn-io/cfncompat"
      version = "~> 0.2"
    }
  }
}

provider "aws" {
  region = var.region
}

provider "cfncompat" {
  region = var.region
}
