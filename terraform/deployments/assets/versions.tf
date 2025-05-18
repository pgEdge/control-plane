terraform {
  required_version = ">= 1.3.4"
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.82.2"
    }
  }
}

provider "aws" {
  allowed_account_ids = ["583677930824"]
  region = "us-east-1"
}
