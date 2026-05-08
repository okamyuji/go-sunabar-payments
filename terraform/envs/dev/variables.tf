# variables.tf
# ルートモジュールの入力変数。 sunabar アクセストークンは sensitive で、 値は terraform.tfvars には書かず
# TF_VAR_sunabar_access_token / TF_VAR_sunabar_access_token_corporate などの環境変数で渡す前提。

variable "region" {
  description = "AWS region. デプロイ先は ap-northeast-1 で固定する想定。"
  type        = string
  default     = "ap-northeast-1"
}

variable "environment" {
  description = "環境名。 dev / stg / prod を想定。 default_tags.Environment にも入る。"
  type        = string
  default     = "dev"
}

variable "project_prefix" {
  description = "リソース名のプレフィックス。 ECR / ECS / Log Group / IAM の名前に共通で前置する。"
  type        = string
  default     = "gsp"
}

variable "ecr_repository_name" {
  description = "ECR リポジトリ名。 既存の go-sunabar-payments を指す前提。"
  type        = string
  default     = "go-sunabar-payments"
}

variable "ecs_cluster_name" {
  description = "ECS Fargate クラスタ名。"
  type        = string
  default     = "gsp-default"
}

variable "log_group_name" {
  description = "CloudWatch Logs のロググループ名。"
  type        = string
  default     = "/ecs/gsp-sunabar-probe"
}

variable "log_retention_days" {
  description = "CloudWatch Logs の保持日数。 検証用は短めの 7 日。"
  type        = number
  default     = 7
}

variable "task_execution_role_name" {
  description = "ECS Task Execution Role 名。"
  type        = string
  default     = "ecsTaskExecutionRole"
}

variable "task_family" {
  description = "ECS Task Definition family 名。"
  type        = string
  default     = "gsp-sunabar-probe"
}

variable "container_image" {
  description = "Probe コンテナのイメージ URI ( タグ込み ) 。 デフォルトは ECR レポ + :latest だが apply 時に最新タグを差し込む。"
  type        = string
  default     = ""
}

variable "container_cpu" {
  description = "Fargate タスクに割り当てる vCPU 単位 ( 256 = 0.25 vCPU ) 。"
  type        = string
  default     = "256"
}

variable "container_memory" {
  description = "Fargate タスクに割り当てるメモリ ( MiB ) 。"
  type        = string
  default     = "512"
}

variable "sunabar_base_url" {
  description = "sunabar API のベース URL。 サンドボックスは https://api.sunabar.gmo-aozora.com 。"
  type        = string
  default     = "https://api.sunabar.gmo-aozora.com"
}

variable "sunabar_access_token" {
  description = "sunabar 個人 API のアクセストークン。 TF_VAR_sunabar_access_token で渡す。"
  type        = string
  sensitive   = true
}

variable "sunabar_access_token_corporate" {
  description = "sunabar 法人 API のアクセストークン ( VA / 法人 accounts 系で使用 ) 。 任意。"
  type        = string
  sensitive   = true
  default     = ""
}

variable "sunabar_account_id" {
  description = "probe で使う sunabar 12 桁 accountId 。"
  type        = string
  default     = "301010012966"
}

variable "subnet_ids" {
  description = "Fargate タスクを置くサブネット ID のリスト。 既定は default VPC の public subnet 1 個。"
  type        = list(string)
  default     = ["subnet-01c8b66f7bdf69051"]
}
