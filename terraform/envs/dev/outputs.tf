output "ecr_repository_url" {
  description = "Docker push 先 URL ( docker login の対象 ) 。"
  value       = module.ecr.repository_url
}

output "ecs_cluster_name" {
  description = "Fargate を実行するクラスタ名。"
  value       = module.ecs_probe.cluster_name
}

output "task_definition_arn" {
  description = "登録された Probe Task Definition の ARN 。 aws ecs run-task で参照する。"
  value       = module.ecs_probe.task_definition_arn
}

output "log_group_name" {
  description = "CloudWatch ロググループ名。"
  value       = module.logs.log_group_name
}

output "task_execution_role_arn" {
  description = "ECS Task Execution Role ARN。"
  value       = module.iam.role_arn
}
