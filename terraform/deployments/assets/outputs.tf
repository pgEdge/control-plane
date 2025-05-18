output pgedge_repository_url {
  value       = aws_ecrpublic_repository.pgedge.repository_uri
  description = "ECR repository URL for pgedge images"
}

output control_plane_repository_url {
  value       = aws_ecrpublic_repository.control_plane.repository_uri
  description = "ECR repository URL for control-plane images"
}
