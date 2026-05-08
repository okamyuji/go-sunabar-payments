# ECR module
# 既存の go-sunabar-payments リポジトリを Terraform 管理下に取り込む / 新規作成する。
# scan_on_push を有効にし、 image immutability は dev では MUTABLE のまま ( タグ付け替えを許容 ) 。

resource "aws_ecr_repository" "this" {
  name                 = var.repository_name
  image_tag_mutability = "MUTABLE"
  # destroy 時にイメージごと削除する。 dev 限定の挙動 ( prod では別環境ディレクトリで false 推奨 )。
  force_delete = true

  image_scanning_configuration {
    scan_on_push = true
  }
  encryption_configuration {
    encryption_type = "AES256"
  }
}

# 古いイメージは保持しない ( タグなし 7 日、 タグ付き 直近 10 件 ) 。
resource "aws_ecr_lifecycle_policy" "this" {
  repository = aws_ecr_repository.this.name

  policy = jsonencode({
    rules = [
      {
        rulePriority = 1
        description  = "Untagged images expire in 7 days"
        selection = {
          tagStatus   = "untagged"
          countType   = "sinceImagePushed"
          countUnit   = "days"
          countNumber = 7
        }
        action = { type = "expire" }
      },
      {
        rulePriority = 2
        description  = "Keep only the most recent 10 tagged images"
        selection = {
          tagStatus   = "any"
          countType   = "imageCountMoreThan"
          countNumber = 10
        }
        action = { type = "expire" }
      },
    ]
  })
}
