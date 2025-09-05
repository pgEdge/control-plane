/////////////////////////
// control-plane image //
/////////////////////////

variable "CONTROL_PLANE_IMAGE_REPO" {
  default = "host.docker.internal:5000/control-plane"
}

variable "CONTROL_PLANE_VERSION" {}

function "control_plane_tags" {
  params = [repo, version]
  // Exclude the 'latest' tag if this is a prerelease
  result = length(regexall("v\\d+\\.\\d+\\.\\d+$", version)) > 0 ? [
    "${repo}:${version}",
    "${repo}:latest",
    ] : [
    "${repo}:${version}"
  ]
}

target "control_plane" {
  context    = "dist"
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
