# providers.tf
# AWS provider の構成。 アクセスキーは環境変数 ( AWS_PROFILE / AWS_REGION ) から拾う前提。
# default_tags で全リソースに共通タグを自動付与する ( コスト・所有権の追跡を平準化 ) 。
provider "aws" {
  region = var.region

  default_tags {
    tags = {
      Project     = "go-sunabar-payments"
      Environment = var.environment
      ManagedBy   = "terraform"
      Owner       = "okamyuji"
    }
  }
}
