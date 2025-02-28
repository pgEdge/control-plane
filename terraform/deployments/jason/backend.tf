terraform {
  backend "s3" {
    bucket = "pgedge-terraform-529820047909"
    key    = "states/control-plane/terraform.tfstate"
    region = "us-east-1"
  }
}
