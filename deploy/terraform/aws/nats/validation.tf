data "aws_iam_policy_document" "validation_trust" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["ecs-tasks.amazonaws.com"]
    }
  }
}
resource "aws_iam_role" "validation" {
  name               = "${var.name}-nats-validation"
  assume_role_policy = data.aws_iam_policy_document.validation_trust.json
}
resource "aws_cloudwatch_log_group" "validation" {
  name              = "/ecs/${var.name}/nats-validation"
  retention_in_days = 7
}
data "aws_iam_policy_document" "validation" {
  statement {
    actions   = ["secretsmanager:GetSecretValue"]
    resources = [aws_secretsmanager_secret.client.arn]
  }
  statement {
    actions   = ["logs:CreateLogStream", "logs:PutLogEvents"]
    resources = ["${aws_cloudwatch_log_group.validation.arn}:*"]
  }
}
resource "aws_iam_role_policy" "validation" {
  name   = "private-nats-validation"
  role   = aws_iam_role.validation.id
  policy = data.aws_iam_policy_document.validation.json
}
resource "aws_ecs_task_definition" "validation" {
  family                   = "${var.name}-nats-validation"
  skip_destroy             = true
  requires_compatibilities = ["FARGATE"]
  network_mode             = "awsvpc"
  cpu                      = "256"
  memory                   = "512"
  execution_role_arn       = aws_iam_role.validation.arn
  runtime_platform {
    cpu_architecture        = "ARM64"
    operating_system_family = "LINUX"
  }
  container_definitions = jsonencode([{
    name        = "validation"
    image       = "public.ecr.aws/docker/library/node:22-alpine"
    essential   = true
    command     = ["sh", "-c", "npm install --prefix /tmp/check --omit=dev --ignore-scripts --no-audit --no-fund nats@2.29.3 >/dev/null 2>&1 && NODE_PATH=/tmp/check/node_modules exec node -e \"$CHECK_SOURCE\""]
    environment = [{ name = "CHECK_SOURCE", value = file("${path.module}/check.cjs") }]
    secrets = [for key in ["NATS_URL", "NATS_CREDS_B64", "NATS_TLS_CA_B64"] : {
      name      = key
      valueFrom = "${aws_secretsmanager_secret.client.arn}:${key}::"
    }]
    logConfiguration = {
      logDriver = "awslogs"
      options = {
        awslogs-group         = aws_cloudwatch_log_group.validation.name
        awslogs-region        = "us-east-1"
        awslogs-stream-prefix = "check"
      }
    }
  }])
  depends_on = [aws_iam_role_policy.validation]
}
output "validation_task_definition" { value = aws_ecs_task_definition.validation.arn }
