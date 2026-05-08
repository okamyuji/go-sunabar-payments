output "repository_url" {
  description = "ECR リポジトリ URL ( docker login / push 先 ) 。"
  value       = aws_ecr_repository.this.repository_url
}

output "repository_arn" {
  description = "ECR リポジトリ ARN。"
  value       = aws_ecr_repository.this.arn
}
