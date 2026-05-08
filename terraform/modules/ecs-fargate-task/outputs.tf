output "cluster_name" {
  description = "ECS クラスタ名。"
  value       = aws_ecs_cluster.this.name
}

output "task_definition_arn" {
  description = "登録された task definition ARN ( aws ecs run-task で使う ) 。"
  value       = aws_ecs_task_definition.probe.arn
}
