terraform {
  required_version = ">= 1.15"

  required_providers {
    awscc = {
      source  = "hashicorp/awscc"
      version = "~> 1.98"
    }
  }
}

provider "awscc" {
  region = var.region
}
