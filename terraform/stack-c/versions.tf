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
  }
}

provider "awscc" {
  region = var.region
}

provider "aws" {
  region = var.region
}
