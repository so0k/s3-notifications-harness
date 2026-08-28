terraform {
  required_providers {
    awscc = {
      source = "hashicorp/awscc"
    }
    # Only for the response bucket's force_destroy -- see main.tf.
    aws = {
      source = "hashicorp/aws"
    }
    cfncompat = {
      source = "cdktn-io/cfncompat"
    }
  }
}
