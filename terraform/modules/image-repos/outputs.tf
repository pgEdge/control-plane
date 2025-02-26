output pgedge_repository_url {
  value       = aws_ecrpublic_repository.pgedge.repository_uri
  description = "ECR repository URL for pgedge images"
}
