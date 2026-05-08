# ECS Fargate task definition + cluster module
# probe を含む Fargate one-shot タスクを動かすための最小構成。
# - cluster は名前指定で作成 / 既存取得
# - task definition は cmd ( /app/api or /app/sunabar-probe ) を切り替え可能

resource "aws_ecs_cluster" "this" {
  name = var.cluster_name
}

resource "aws_ecs_task_definition" "probe" {
  family                   = var.task_family
  network_mode             = "awsvpc"
  requires_compatibilities = ["FARGATE"]
  cpu                      = var.cpu
  memory                   = var.memory
  execution_role_arn       = var.execution_role_arn

  container_definitions = jsonencode([
    {
      name       = "probe"
      image      = var.image
      essential  = true
      entryPoint = ["/app/sunabar-probe"]
      environment = [
        for k, v in var.environment : { name = k, value = v }
      ]
      logConfiguration = {
        logDriver = "awslogs"
        options = {
          awslogs-group         = var.log_group_name
          awslogs-region        = var.region
          awslogs-stream-prefix = "probe"
        }
      }
    }
  ])
}
