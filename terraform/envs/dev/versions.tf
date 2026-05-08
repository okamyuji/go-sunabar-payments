# versions.tf
# Terraform / Provider のバージョン制約をルートでピン留めする。
# 同一プロバイダのメジャーバージョン更新は plan で破壊的変更が発生し得るため、 ~> ( 同一マイナー以内 ) で固定する。
terraform {
  required_version = ">= 1.6.0, < 2.0.0"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.85"
    }
  }
}
