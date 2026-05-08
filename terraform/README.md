# Terraform IaC for go-sunabar-payments

このディレクトリ以下が Terraform のソース。 Go ベストプラクティスに倣い、 関心ごとを小さなモジュールに分割している。

## ディレクトリ構成

```
terraform/
├── envs/
│   └── dev/                # ルートモジュール ( apply の起点 )
│       ├── main.tf         # モジュール組み立て
│       ├── variables.tf    # 入力変数 ( sensitive な値は環境変数で渡す )
│       ├── outputs.tf      # 出力 ( ECR URL / Task ARN など )
│       ├── providers.tf    # AWS provider の default_tags
│       └── versions.tf     # terraform / provider のバージョンピン
└── modules/
    ├── ecr/                # ECR リポジトリ + lifecycle policy
    ├── iam/                # ECS Task Execution Role
    ├── logs/               # CloudWatch Logs ロググループ
    └── ecs-fargate-task/   # ECS cluster + Task Definition ( probe )
```

## 使い方

```bash
export AWS_PROFILE=fintech-apigw

# sunabar アクセストークンは環境変数で渡す ( tfvars には絶対に書かない )
export TF_VAR_sunabar_access_token="${SUNABAR_ACCESS_TOKEN}"
export TF_VAR_sunabar_access_token_corporate="${SUNABAR_ACCESS_TOKEN_CORPORATE:-}"

# 既存タグの ECR push 済みイメージを参照する場合
export TF_VAR_container_image="018356302326.dkr.ecr.ap-northeast-1.amazonaws.com/go-sunabar-payments:20260509-043023"

cd terraform/envs/dev
terraform init
terraform fmt -recursive ..
terraform validate
terraform plan
terraform apply
```

## 既存リソースの import

このプロジェクトでは既に手動 ( aws CLI ) で作成したリソースが存在するため、
初回 apply 前に以下を import して state を整合させる。

```bash
terraform import module.ecr.aws_ecr_repository.this go-sunabar-payments
terraform import module.iam.aws_iam_role.task_execution ecsTaskExecutionRole
terraform import module.iam.aws_iam_role_policy_attachment.task_execution \
  ecsTaskExecutionRole/arn:aws:iam::aws:policy/service-role/AmazonECSTaskExecutionRolePolicy
terraform import module.logs.aws_cloudwatch_log_group.this /ecs/gsp-sunabar-probe
terraform import module.ecs_probe.aws_ecs_cluster.this gsp-default
# task_definition は revision が世代管理なので import 不要 ( apply で新規 revision を作る )
```

## probe を実行する

apply 後、 sunabar との通信を AWS から検証する。

```bash
TASK_DEF_ARN=$(terraform output -raw task_definition_arn)
SUBNET=subnet-01c8b66f7bdf69051

aws ecs run-task \
  --cluster $(terraform output -raw ecs_cluster_name) \
  --task-definition "$TASK_DEF_ARN" \
  --launch-type FARGATE \
  --network-configuration "awsvpcConfiguration={subnets=[${SUBNET}],assignPublicIp=ENABLED}"
```

CloudWatch Logs の出力を `aws logs tail "$(terraform output -raw log_group_name)" --follow` で確認する。

## 機密の扱い

- `terraform.tfvars` には sunabar トークンを書かない ( ファイル自体 `.gitignore` 済み )
- 全て `TF_VAR_*` 環境変数で渡す
- Task Definition の environment に直接埋まるが、 これは AWS API ( ECS DescribeTaskDefinition ) には sensitive=true で参照側を制限可能
- 本番 ( prod ) では Secrets Manager + ECS `secrets` フィールド経由を推奨 ( 別環境ディレクトリで上書き )
