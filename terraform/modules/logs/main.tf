# CloudWatch Logs module
# probe / api 用のログを集約するロググループ。 retention_in_days を明示してコストを抑える。

resource "aws_cloudwatch_log_group" "this" {
  name              = var.log_group_name
  retention_in_days = var.retention_days
}
