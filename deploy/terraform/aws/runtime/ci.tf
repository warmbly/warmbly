variable "github_repository" {
  description = "Repository allowed to deploy through the protected GitHub environment"
  type        = string
  default     = null
}

variable "github_environment" {
  type    = string
  default = "aws-production"
}

resource "aws_iam_openid_connect_provider" "github" {
  count          = var.github_repository == null ? 0 : 1
  url            = "https://token.actions.githubusercontent.com"
  client_id_list = ["sts.amazonaws.com"]
}

data "aws_iam_policy_document" "github_trust" {
  count = var.github_repository == null ? 0 : 1
  statement {
    actions = ["sts:AssumeRoleWithWebIdentity"]
    principals {
      type        = "Federated"
      identifiers = [aws_iam_openid_connect_provider.github[0].arn]
    }
    condition {
      test     = "StringEquals"
      variable = "token.actions.githubusercontent.com:aud"
      values   = ["sts.amazonaws.com"]
    }
    condition {
      test     = "StringEquals"
      variable = "token.actions.githubusercontent.com:sub"
      values   = ["repo:${var.github_repository}:environment:${var.github_environment}"]
    }
  }
}

resource "aws_iam_role" "github_deploy" {
  count                = var.github_repository == null ? 0 : 1
  name                 = "${var.name}-github-deploy"
  assume_role_policy   = data.aws_iam_policy_document.github_trust[0].json
  max_session_duration = 3600
}

locals {
  ecs_arn = "arn:aws:ecs:us-east-1:${data.aws_caller_identity.current.account_id}"
  # One-off tasks a deploy or a rotation runs, and the roles they pass.
  github_tasks = {
    migration  = [aws_iam_role.migration_execution.arn]
    validation = [aws_iam_role.validation_execution.arn, aws_iam_role.application["backend"].arn]
    ops        = [aws_iam_role.execution["backend"].arn, aws_iam_role.application["backend"].arn]
    rotator    = [aws_iam_role.rotator_execution.arn, aws_iam_role.rotator.arn]
  }
  github_log_groups = concat(
    [aws_cloudwatch_log_group.migration.arn, aws_cloudwatch_log_group.validation.arn, aws_cloudwatch_log_group.rotator.arn],
    ["arn:aws:logs:us-east-1:${data.aws_caller_identity.current.account_id}:log-group:/ecs/${var.name}/backend"],
  )
}

data "aws_iam_policy_document" "github_deploy" {
  count = var.github_repository == null ? 0 : 1
  statement {
    actions   = ["ecs:DescribeTaskDefinition", "ecs:RegisterTaskDefinition"]
    resources = ["*"]
  }
  statement {
    actions   = ["ecs:TagResource"]
    resources = ["${local.ecs_arn}:task-definition/${var.name}-*:*"]
    condition {
      test     = "StringEquals"
      variable = "ecs:CreateAction"
      values   = ["RegisterTaskDefinition"]
    }
  }
  statement {
    actions   = ["ecs:ListTagsForResource"]
    resources = ["${local.ecs_arn}:task-definition/${var.name}-*:*"]
  }
  # Describe by prefix so a deploy can tell which services this install runs; only those can be updated.
  statement {
    actions   = ["ecs:DescribeServices"]
    resources = ["${local.ecs_arn}:service/${var.name}/${var.name}-*"]
  }
  statement {
    actions   = ["ecs:UpdateService"]
    resources = [for service in keys(var.services) : "${local.ecs_arn}:service/${var.name}/${var.name}-${service}"]
  }
  statement {
    actions   = ["ecs:DescribeTasks"]
    resources = ["${local.ecs_arn}:task/${var.name}/*"]
  }
  statement {
    actions   = ["ecs:RunTask"]
    resources = [for task in keys(local.github_tasks) : "${local.ecs_arn}:task-definition/${var.name}-${task}:*"]
    condition {
      test     = "ArnEquals"
      variable = "ecs:cluster"
      values   = [local.foundation.cluster_arn]
    }
  }
  statement {
    actions = ["iam:PassRole"]
    resources = distinct(concat(flatten(values(local.github_tasks)), flatten([
      for name in keys(var.services) : [aws_iam_role.execution[name].arn, aws_iam_role.application[name].arn]
    ])))
    condition {
      test     = "StringEquals"
      variable = "iam:PassedToService"
      values   = ["ecs-tasks.amazonaws.com"]
    }
  }
  statement {
    actions   = ["logs:GetLogEvents"]
    resources = [for group in local.github_log_groups : "${group}:log-stream:*"]
  }
  statement {
    actions   = ["cloudwatch:DescribeAlarms"]
    resources = ["*"]
  }
  # Version metadata only: the deploy repins references and never reads a value.
  statement {
    actions   = ["secretsmanager:DescribeSecret", "secretsmanager:ListSecretVersionIds"]
    resources = [for name in keys(var.services) : local.foundation.secret_arns[name]]
  }
}

resource "aws_iam_role_policy" "github_deploy" {
  count  = var.github_repository == null ? 0 : 1
  name   = "migrate-before-deploy"
  role   = aws_iam_role.github_deploy[0].id
  policy = data.aws_iam_policy_document.github_deploy[0].json
}

output "github_deploy_role_arn" {
  value = var.github_repository == null ? null : aws_iam_role.github_deploy[0].arn
}
