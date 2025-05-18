resource "aws_ecrpublic_repository" "pgedge" {
  repository_name      = "pgedge"
}

resource "aws_ecrpublic_repository" "control_plane" {
  repository_name      = "control-plane"
}
