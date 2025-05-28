/////////////////////////
// control-plane image //
/////////////////////////

variable "CONTROL_PLANE_IMAGE_REPO" {
  default = "host.docker.internal:5000/control-plane"
}

variable "CONTROL_PLANE_VERSION" {}

function "control_plane_tags" {
  params = [repo, version]
  result = [
      "${repo}:${version}",
    ]
}

target "control_plane" {
  context = "dist"
  dockerfile = "../docker/control-plane/Dockerfile"
  args = {
    ARCHIVE_VERSION = trimprefix(CONTROL_PLANE_VERSION, "v")
  }
  tags = control_plane_tags(
    CONTROL_PLANE_IMAGE_REPO,
    CONTROL_PLANE_VERSION,
  )
  platforms = [
    "linux/amd64",
    "linux/arm64",
  ]
  attest = [
    "type=provenance,mode=min",
    "type=sbom",
  ]
}

//////////////////
// pgedge image //
//////////////////

variable "PGEDGE_IMAGE_REPO" {
  default = "host.docker.internal:5000/pgedge"
}

variable "PACKAGE_REPO_BASE_URL" {
  default = "http://pgedge-529820047909-yum.s3-website.us-east-2.amazonaws.com"
}

variable "PACKAGE_RELEASE_CHANNEL" {
  default = "dev"
}

function "pgedge_tags" {
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
  tags = pgedge_tags(PGEDGE_IMAGE_REPO, pg_version, image_version)
  args = {
    REPO_BASE_URL = PACKAGE_REPO_BASE_URL
    RELEASE_CHANNEL = PACKAGE_RELEASE_CHANNEL
    POSTGRES_VERSION = pg_version
    IMAGE_VERSION = image_version
  }
  platforms = [
    "linux/amd64",
    "linux/arm64",
  ]
  attest = [
    "type=provenance,mode=min",
    "type=sbom",
  ]
}
