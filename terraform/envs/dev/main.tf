# main.tf
# go-sunabar-payments の dev 環境を構成するルートモジュール。
# モジュール責務:
#   - ecr   : Docker イメージ置き場 ( push 先 )
#   - logs  : CloudWatch Logs ( ロググループ + 保持日数 )
#   - iam   : ECS task execution role
#   - ecs-fargate-task : Fargate cluster + task definition ( probe )

# データソース: 現在の caller identity で region / account を確認できるようにしておく ( outputs で表示 )。
data "aws_caller_identity" "current" {}

# 既定で ECR repo URL の `<acct>.dkr.ecr.<region>.amazonaws.com/<repo>:<tag>` を組み立てるためのローカル値。
locals {
  account_id = data.aws_caller_identity.current.account_id
  ecr_uri    = "${local.account_id}.dkr.ecr.${var.region}.amazonaws.com/${var.ecr_repository_name}"
  # var.container_image が空なら ":latest" を組み立てる ( CI からは TF_VAR_container_image でタグ込み URI を渡す ) 。
  resolved_image = var.container_image != "" ? var.container_image : "${local.ecr_uri}:latest"
}

module "ecr" {
  source          = "../../modules/ecr"
  repository_name = var.ecr_repository_name
}

module "logs" {
  source         = "../../modules/logs"
  log_group_name = var.log_group_name
  retention_days = var.log_retention_days
}

module "iam" {
  source    = "../../modules/iam"
  role_name = var.task_execution_role_name
}

module "ecs_probe" {
  source             = "../../modules/ecs-fargate-task"
  cluster_name       = var.ecs_cluster_name
  task_family        = var.task_family
  image              = local.resolved_image
  cpu                = var.container_cpu
  memory             = var.container_memory
  execution_role_arn = module.iam.role_arn
  log_group_name     = module.logs.log_group_name
  region             = var.region

  environment = {
    SUNABAR_BASE_URL               = var.sunabar_base_url
    SUNABAR_ACCESS_TOKEN           = var.sunabar_access_token
    SUNABAR_ACCESS_TOKEN_CORPORATE = var.sunabar_access_token_corporate
    SUNABAR_ACCOUNT_ID             = var.sunabar_account_id
    SUNABAR_PROBE_WRITE            = "transfer"
    SUNABAR_PROBE_AMOUNT           = "1"
  }
}
