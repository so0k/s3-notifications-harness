terraform {
  required_version = ">= 1.15"

  required_providers {
    awscc = {
      source  = "hashicorp/awscc"
      version = "~> 1.98"
    }
    aws = {
      source  = "hashicorp/aws"
      version = "~> 6.0"
    }
    cfncompat = {
      source  = "cdktn-io/cfncompat"
      version = "~> 0.2"
    }
  }
}

provider "awscc" {
  region = var.region
}

provider "aws" {
  region = var.region
}

provider "cfncompat" {
  region = var.region
}
