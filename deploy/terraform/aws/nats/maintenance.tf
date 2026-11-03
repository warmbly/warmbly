resource "aws_ssm_patch_baseline" "security" {
  name                                 = "${var.name}-nats-security"
  operating_system                     = "AMAZON_LINUX_2023"
  approved_patches_enable_non_security = false
  approval_rule {
    approve_after_days = 7
    compliance_level   = "CRITICAL"
    patch_filter {
      key    = "CLASSIFICATION"
      values = ["Security"]
    }
    patch_filter {
      key    = "SEVERITY"
      values = ["Critical", "Important"]
    }
  }
}
resource "aws_ssm_patch_group" "security" {
  baseline_id = aws_ssm_patch_baseline.security.id
  patch_group = "${var.name}-nats"
}

data "aws_iam_policy_document" "maintenance_trust" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["ssm.amazonaws.com"]
    }
    condition {
      test     = "StringEquals"
      variable = "aws:SourceAccount"
      values   = [data.aws_caller_identity.current.account_id]
    }
    condition {
      test     = "ArnLike"
      variable = "aws:SourceArn"
      values   = ["arn:aws:ssm:us-east-1:${data.aws_caller_identity.current.account_id}:automation-execution/*"]
    }
  }
}
resource "aws_iam_role" "maintenance" {
  name               = "${var.name}-nats-maintenance"
  assume_role_policy = data.aws_iam_policy_document.maintenance_trust.json
}
data "aws_iam_policy_document" "maintenance" {
  statement {
    actions = ["ssm:SendCommand"]
    resources = concat(
      ["arn:aws:ssm:us-east-1::document/AWS-RunShellScript", "arn:aws:ssm:us-east-1::document/AWS-RunPatchBaseline"],
      [for node in aws_instance.node : node.arn]
    )
  }
  statement {
    actions   = ["ssm:GetCommandInvocation", "ssm:ListCommandInvocations", "ssm:ListCommands", "ssm:CancelCommand", "ssm:DescribeInstanceInformation", "ec2:DescribeInstanceStatus"]
    resources = ["*"]
  }
}
resource "aws_iam_role_policy" "maintenance" {
  name   = "patch-nats-one-broker-at-a-time"
  role   = aws_iam_role.maintenance.id
  policy = data.aws_iam_policy_document.maintenance.json
}
resource "aws_ssm_document" "maintenance" {
  name          = "${var.name}-nats-rolling-security-patches"
  document_type = "Automation"
  content = jsonencode({
    schemaVersion = "0.3"
    description   = "Apply OS security patches serially, aborting before the next broker unless all replicas recover"
    assumeRole    = aws_iam_role.maintenance.arn
    mainSteps = [{
      name = "brokers", action = "aws:loop", onFailure = "Abort"
      inputs = {
        Iterators        = [for key in sort(keys(local.nodes)) : aws_instance.node[key].id]
        IteratorDataType = "String"
        Steps = [
          {
            name = "readyBefore", action = "aws:runCommand", onFailure = "Abort", timeoutSeconds = 900
            inputs = {
              DocumentName = "AWS-RunShellScript"
              InstanceIds  = [for key in sort(keys(local.nodes)) : aws_instance.node[key].id]
              Parameters   = { commands = [file("${path.module}/wait-for-quorum.sh")], executionTimeout = ["600"] }
            }
          },
          {
            name = "patch", action = "aws:runCommand", onFailure = "Abort", timeoutSeconds = 3600
            inputs = {
              DocumentName = "AWS-RunPatchBaseline"
              InstanceIds  = ["{{brokers.CurrentIteratorValue}}"]
              Parameters   = { Operation = ["Install"], RebootOption = ["RebootIfNeeded"] }
            }
          },
          {
            name = "readyAfter", action = "aws:runCommand", onFailure = "Abort", timeoutSeconds = 900
            inputs = {
              DocumentName = "AWS-RunShellScript"
              InstanceIds  = [for key in sort(keys(local.nodes)) : aws_instance.node[key].id]
              Parameters   = { commands = [file("${path.module}/wait-for-quorum.sh")], executionTimeout = ["600"] }
            }
          }
        ]
      }
    }]
  })
  depends_on = [aws_iam_role_policy.maintenance]
}
variable "maintenance_enabled" {
  type    = bool
  default = false
}
resource "aws_cloudwatch_event_rule" "maintenance" {
  name                = "${var.name}-nats-weekly-security-patches"
  schedule_expression = "cron(0 4 ? * SUN *)"
  state               = var.maintenance_enabled ? "ENABLED" : "DISABLED"
}
resource "aws_cloudwatch_event_rule" "maintenance_failure" {
  name = "${var.name}-nats-security-patch-failure"
  event_pattern = jsonencode({
    source        = ["aws.ssm"]
    "detail-type" = ["EC2 Automation Execution Status-change Notification"]
    detail        = { Definition = [aws_ssm_document.maintenance.name], Status = ["Failed", "TimedOut", "Cancelled"] }
  })
}
resource "aws_cloudwatch_event_target" "maintenance_failure" {
  rule       = aws_cloudwatch_event_rule.maintenance_failure.name
  arn        = aws_sns_topic.operations.arn
  depends_on = [aws_sns_topic_policy.operations]
}
data "aws_iam_policy_document" "maintenance_schedule_trust" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["events.amazonaws.com"]
    }
    condition {
      test     = "ArnEquals"
      variable = "aws:SourceArn"
      values   = [aws_cloudwatch_event_rule.maintenance.arn]
    }
    condition {
      test     = "StringEquals"
      variable = "aws:SourceAccount"
      values   = [data.aws_caller_identity.current.account_id]
    }
  }
}
resource "aws_iam_role" "maintenance_schedule" {
  name               = "${var.name}-nats-maintenance-schedule"
  assume_role_policy = data.aws_iam_policy_document.maintenance_schedule_trust.json
}
locals {
  maintenance_arn = "arn:aws:ssm:us-east-1:${data.aws_caller_identity.current.account_id}:automation-definition/${aws_ssm_document.maintenance.name}:$DEFAULT"
}
data "aws_iam_policy_document" "maintenance_schedule" {
  statement {
    actions   = ["ssm:StartAutomationExecution"]
    resources = [local.maintenance_arn]
  }
  statement {
    actions   = ["iam:PassRole"]
    resources = [aws_iam_role.maintenance.arn]
    condition {
      test     = "StringEquals"
      variable = "iam:PassedToService"
      values   = ["ssm.amazonaws.com"]
    }
  }
}
resource "aws_iam_role_policy" "maintenance_schedule" {
  name   = "start-nats-security-patches"
  role   = aws_iam_role.maintenance_schedule.id
  policy = data.aws_iam_policy_document.maintenance_schedule.json
}
resource "aws_cloudwatch_event_target" "maintenance" {
  rule       = aws_cloudwatch_event_rule.maintenance.name
  arn        = local.maintenance_arn
  role_arn   = aws_iam_role.maintenance_schedule.arn
  input      = "{}"
  depends_on = [aws_iam_role_policy.maintenance_schedule, aws_ssm_patch_group.security]
}
output "maintenance_document" { value = aws_ssm_document.maintenance.name }
