variable "IMAGE_REPO_HOST" {
    default = "public.ecr.aws/k8c8c8g7"
}

variable "PGEDGE_REPO_BASE_URL" {
    default = "http://pgedge-529820047909-yum.s3-website.us-east-2.amazonaws.com"
}

variable "PGEDGE_RELEASE_CHANNEL" {
    default = "dev"
}

function "pgedgeTag" {
    params = [repo, pg_version, image_version]
    result = ["${repo}:pg${pg_version}_${image_version}"]
}

target "pgedge" {
  context = "docker/pgedge"
  matrix = {
    pg_version = ["15", "16", "17"],
    image_version = ["4.0.10-3"]
  }
  name = replace("pgedge-${pg_version}-${image_version}", ".", "_")
  tags = pgedgeTag("${IMAGE_REPO_HOST}/pgedge", pg_version, image_version)
  args = {
    REPO_BASE_URL = PGEDGE_REPO_BASE_URL
    RELEASE_CHANNEL = PGEDGE_RELEASE_CHANNEL
    POSTGRES_VERSION = pg_version
    IMAGE_VERSION = image_version
  }
  platforms = [
    "linux/amd64",
    "linux/arm64",
  ]
}
