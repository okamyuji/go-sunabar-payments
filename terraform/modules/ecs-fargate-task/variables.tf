variable "cluster_name" {
  description = "ECS クラスタ名。"
  type        = string
}

variable "task_family" {
  description = "ECS Task Definition family 名。"
  type        = string
}

variable "image" {
  description = "コンテナイメージ URI ( タグ込み ) 。"
  type        = string
}

variable "cpu" {
  description = "vCPU 単位 ( 256 = 0.25 vCPU ) 。"
  type        = string
}

variable "memory" {
  description = "メモリ ( MiB ) 。"
  type        = string
}

variable "execution_role_arn" {
  description = "ECS Task Execution Role ARN。"
  type        = string
}

variable "log_group_name" {
  description = "CloudWatch ロググループ名。"
  type        = string
}

variable "region" {
  description = "ロググループのリージョン ( awslogs-region に渡す ) 。"
  type        = string
}

variable "environment" {
  description = "コンテナに渡す環境変数 ( map<string,string> ) 。 sensitive 値も含む。"
  type        = map(string)
  sensitive   = true
}
