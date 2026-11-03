data "aws_caller_identity" "current" {}

variable "notification_email" {
  type     = string
  default  = null
  nullable = true
}
resource "aws_sns_topic" "operations" {
  name = "${var.name}-nats-operations"
}
resource "aws_sns_topic_subscription" "operations" {
  count     = var.notification_email == null ? 0 : 1
  topic_arn = aws_sns_topic.operations.arn
  protocol  = "email"
  endpoint  = var.notification_email
}
data "aws_iam_policy_document" "notifications" {
  statement {
    sid       = "MaintenanceFailures"
    actions   = ["sns:Publish"]
    resources = [aws_sns_topic.operations.arn]
    principals {
      type        = "Service"
      identifiers = ["events.amazonaws.com"]
    }
    condition {
      test     = "StringEquals"
      variable = "aws:SourceAccount"
      values   = [data.aws_caller_identity.current.account_id]
    }
    condition {
      test     = "ArnEquals"
      variable = "aws:SourceArn"
      values   = [aws_cloudwatch_event_rule.maintenance_failure.arn]
    }
  }
  statement {
    sid       = "CloudWatchAlarms"
    actions   = ["sns:Publish"]
    resources = [aws_sns_topic.operations.arn]
    principals {
      type        = "Service"
      identifiers = ["cloudwatch.amazonaws.com"]
    }
    condition {
      test     = "StringEquals"
      variable = "aws:SourceAccount"
      values   = [data.aws_caller_identity.current.account_id]
    }
    condition {
      test     = "ArnLike"
      variable = "aws:SourceArn"
      values   = ["arn:aws:cloudwatch:us-east-1:${data.aws_caller_identity.current.account_id}:alarm:${var.name}-nats*"]
    }
  }
}
resource "aws_sns_topic_policy" "operations" {
  arn    = aws_sns_topic.operations.arn
  policy = data.aws_iam_policy_document.notifications.json
}

resource "aws_s3_bucket" "backups" {
  bucket = "${var.name}-nats-backups-${data.aws_caller_identity.current.account_id}"
  lifecycle { prevent_destroy = true }
}
resource "aws_s3_bucket_public_access_block" "backups" {
  bucket                  = aws_s3_bucket.backups.id
  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}
resource "aws_s3_bucket_server_side_encryption_configuration" "backups" {
  bucket = aws_s3_bucket.backups.id
  rule {
    apply_server_side_encryption_by_default { sse_algorithm = "AES256" }
  }
}
resource "aws_s3_bucket_versioning" "backups" {
  bucket = aws_s3_bucket.backups.id
  versioning_configuration { status = "Enabled" }
}
resource "aws_s3_bucket_lifecycle_configuration" "backups" {
  bucket = aws_s3_bucket.backups.id
  rule {
    id     = "seven-daily-recovery-points"
    status = "Enabled"
    filter { prefix = "warmbly/" }
    expiration { days = 7 }
    noncurrent_version_expiration { noncurrent_days = 7 }
    abort_incomplete_multipart_upload { days_after_initiation = 1 }
  }
  depends_on = [aws_s3_bucket_versioning.backups]
}
data "aws_iam_policy_document" "backup_bucket" {
  statement {
    effect    = "Deny"
    actions   = ["s3:*"]
    resources = [aws_s3_bucket.backups.arn, "${aws_s3_bucket.backups.arn}/*"]
    principals {
      type        = "*"
      identifiers = ["*"]
    }
    condition {
      test     = "Bool"
      variable = "aws:SecureTransport"
      values   = ["false"]
    }
  }
}
resource "aws_s3_bucket_policy" "backups" {
  bucket = aws_s3_bucket.backups.id
  policy = data.aws_iam_policy_document.backup_bucket.json
}
resource "aws_secretsmanager_secret" "backup" {
  name                    = "${var.name}/nats/backup"
  description             = "Snapshot-only NATS credential; populated outside Terraform"
  recovery_window_in_days = 30
  lifecycle { prevent_destroy = true }
}
data "aws_iam_policy_document" "backup" {
  statement {
    actions   = ["secretsmanager:GetSecretValue"]
    resources = [aws_secretsmanager_secret.backup.arn]
  }
  statement {
    actions   = ["s3:PutObject", "s3:AbortMultipartUpload"]
    resources = ["${aws_s3_bucket.backups.arn}/warmbly/*"]
  }
}
resource "aws_iam_role_policy" "backup" {
  name   = "nats-snapshot-upload"
  role   = aws_iam_role.node["nats1"].id
  policy = data.aws_iam_policy_document.backup.json
}

resource "aws_ssm_document" "operations" {
  name          = "${var.name}-nats-operations"
  document_type = "Command"
  content = jsonencode({
    schemaVersion = "2.2"
    description   = "Install verified NATS backup tooling and monitoring without restarting brokers"
    mainSteps = [{
      action = "aws:runShellScript", name = "configure"
      inputs = {
        timeoutSeconds = "600"
        runCommand = [templatefile("${path.module}/operations.sh.tftpl", {
          bucket = aws_s3_bucket.backups.id, secret_arn = aws_secretsmanager_secret.backup.arn
        })]
      }
    }]
  })
}
resource "aws_ssm_association" "operations" {
  name             = aws_ssm_document.operations.name
  document_version = aws_ssm_document.operations.latest_version
  association_name = "${var.name}-nats-operations"
  targets {
    key    = "InstanceIds"
    values = [for key in sort(keys(local.nodes)) : aws_instance.node[key].id]
  }
  max_concurrency = "1"
  max_errors      = "0"
  depends_on      = [aws_iam_role_policy.backup]
}

resource "aws_cloudwatch_metric_alarm" "backup" {
  alarm_name          = "${var.name}-nats-backup-overdue"
  namespace           = "Warmbly/NATS"
  metric_name         = "BackupAgeSeconds"
  dimensions          = { Node = "nats1" }
  comparison_operator = "GreaterThanThreshold"
  threshold           = 30 * 3600
  evaluation_periods  = 3
  period              = 60
  statistic           = "Maximum"
  treat_missing_data  = "breaching"
  alarm_actions       = [aws_sns_topic.operations.arn]
  ok_actions          = [aws_sns_topic.operations.arn]
}
resource "aws_cloudwatch_metric_alarm" "certificate" {
  for_each            = local.nodes
  alarm_name          = "${var.name}-${each.key}-certificate-expiring"
  namespace           = "Warmbly/NATS"
  metric_name         = "CertificateDaysRemaining"
  dimensions          = { Node = each.key }
  comparison_operator = "LessThanThreshold"
  threshold           = 30
  evaluation_periods  = 3
  period              = 60
  statistic           = "Minimum"
  treat_missing_data  = "breaching"
  alarm_actions       = [aws_sns_topic.operations.arn]
  ok_actions          = [aws_sns_topic.operations.arn]
}
output "backup_secret_arn" { value = aws_secretsmanager_secret.backup.arn }
output "backup_bucket" { value = aws_s3_bucket.backups.id }
output "operations_topic_arn" { value = aws_sns_topic.operations.arn }
