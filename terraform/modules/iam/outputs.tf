output "role_arn" {
  description = "Task Execution Role の ARN ( タスク定義 executionRoleArn に渡す ) 。"
  value       = aws_iam_role.task_execution.arn
}
