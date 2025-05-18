terraform {
  backend "s3" {
    bucket = "control-plane-terraform-583677930824"
    key    = "states/control-plane/terraform.tfstate"
    region = "us-east-1"
  }
}
