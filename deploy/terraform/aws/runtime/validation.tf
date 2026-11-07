locals {
  validation_configuration_names = ["KAFKA_BOOTSTRAP_SERVERS", "SCHEMA_REGISTRY_URL", "BLOB_BUCKET", "EVENTBUS_PROVIDER", "CODEC_PROVIDER", "EVENTBUS_READ_BOTH", "CODEC_READ_BOTH", "NATS_URL", "NATS_TLS_CA_B64", "NATS_STREAM_NAME", "NATS_REPLICAS"]
}

resource "aws_iam_role" "validation_execution" {
  name               = "${var.name}-validation-execution"
  assume_role_policy = data.aws_iam_policy_document.trust.json
}
resource "aws_cloudwatch_log_group" "validation" {
  name              = "/ecs/${var.name}/validation"
  retention_in_days = 1
}
data "aws_iam_policy_document" "validation_execution" {
  statement {
    actions = ["secretsmanager:GetSecretValue"]
    resources = concat([
      for name in ["backend", "consumer", "realtime"] : local.foundation.secret_arns[name]
    ], [data.terraform_remote_state.foundation.outputs.cache.credential_secret])
  }
  statement {
    actions   = ["ssm:GetParameters"]
    resources = [for key in local.validation_configuration_names : local.parameters["backend/${key}"] if contains(keys(local.parameters), "backend/${key}")]
  }
  statement {
    actions   = ["logs:CreateLogStream", "logs:PutLogEvents"]
    resources = ["${aws_cloudwatch_log_group.validation.arn}:*"]
  }
}
resource "aws_iam_role_policy" "validation_execution" {
  name   = "dependency-validation"
  role   = aws_iam_role.validation_execution.id
  policy = data.aws_iam_policy_document.validation_execution.json
}
resource "aws_ecs_task_definition" "validation" {
  skip_destroy             = true
  family                   = "${var.name}-validation"
  requires_compatibilities = ["FARGATE"]
  network_mode             = "awsvpc"
  cpu                      = "512"
  memory                   = "1024"
  execution_role_arn       = aws_iam_role.validation_execution.arn
  task_role_arn            = aws_iam_role.application["backend"].arn
  runtime_platform {
    cpu_architecture        = "ARM64"
    operating_system_family = "LINUX"
  }
  container_definitions = jsonencode([{
    name      = "validation"
    image     = "public.ecr.aws/docker/library/node:22-alpine"
    essential = true
    command   = ["sh", "-c", "npm install --prefix /tmp/check --omit=dev --ignore-scripts --no-audit --no-fund pg@8.23.1 kafkajs@2.2.4 redis@6.3.0 nats@2.29.3 @aws-sdk/client-kms@3.1155.0 @aws-sdk/client-s3@3.1155.0 >/dev/null 2>&1 && NODE_PATH=/tmp/check/node_modules exec node -e \"$CHECK_SOURCE\""]
    environment = concat([
      { name = "AWS_REGION", value = "us-east-1" },
      { name = "CACHE_HOST", value = data.terraform_remote_state.foundation.outputs.cache.endpoint },
      { name = "CHECK_SOURCE", value = join("\n", [file("${path.module}/checks/credentials.cjs"), file("${path.module}/checks/dependencies.cjs")]) }
    ], [for key, value in local.service_environment["backend"] : { name = key, value = value } if contains(local.validation_configuration_names, key)])
    secrets = concat([
      for key in ["PRIMARY_DB", "CREDENTIALS_ENCRYPTION_KEY", "AUTH_SECRET", "REDIS", "KAFKA_SASL_USERNAME", "KAFKA_SASL_PASSWORD", "SCHEMA_REGISTRY_KEY", "SCHEMA_REGISTRY_SECRET", "NATS_CREDS_B64"] : {
        name      = key
        valueFrom = "${local.foundation.secret_arns["backend"]}:${key}::${var.services["backend"].secret_version_id}"
      } if contains(var.services["backend"].secret_keys, key)
      ], [for key in ["CREDENTIALS_ENCRYPTION_READ_KEYS", "TWOFA_SECRET", "TWOFA_READ_SECRETS"] : {
        name      = key
        valueFrom = "${local.foundation.secret_arns["backend"]}:${key}::${var.services["backend"].secret_version_id}"
      } if contains(var.services["backend"].secret_keys, key)], [
      { name = "CONSUMER_DB", valueFrom = "${local.foundation.secret_arns["consumer"]}:PRIMARY_DB::${var.services["consumer"].secret_version_id}" },
      { name = "REALTIME_DB", valueFrom = "${local.foundation.secret_arns["realtime"]}:DATABASE_URL::${var.services["realtime"].secret_version_id}" },
      { name = "NEW_REDIS_PASSWORD", valueFrom = "${data.terraform_remote_state.foundation.outputs.cache.credential_secret}:REDIS_PASSWORD::" }
      ], [for key in local.validation_configuration_names : {
        name      = key
        valueFrom = local.parameters["backend/${key}"]
    } if contains(keys(local.parameters), "backend/${key}") && !contains(keys(local.service_environment["backend"]), key)])
    logConfiguration = {
      logDriver = "awslogs"
      options = {
        awslogs-group         = aws_cloudwatch_log_group.validation.name
        awslogs-region        = "us-east-1"
        awslogs-stream-prefix = "check"
      }
    }
  }])
  depends_on = [aws_iam_role_policy.validation_execution, aws_iam_role_policy.application]
}
output "validation_task_definition" { value = aws_ecs_task_definition.validation.arn }
