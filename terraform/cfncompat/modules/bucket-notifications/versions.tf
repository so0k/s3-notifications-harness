terraform {
  required_providers {
    aws = {
      source = "hashicorp/aws"
    }
    archive = {
      source = "hashicorp/archive"
    }
    cfncompat = {
      source = "cdktn-io/cfncompat"
    }
  }
}
