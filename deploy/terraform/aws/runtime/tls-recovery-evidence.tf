variable "tls_recovery_evidence_writer" {
  description = "Create the hosted-only conditional S3 writer for private runtime TLS recovery evidence"
  type        = bool
  default     = false
  validation {
    condition = !var.tls_recovery_evidence_writer || (
      var.terraform_ci && var.github_repository == "warmbly/warmbly" &&
      var.name == "warmbly-production" && var.state_bucket == "warmbly-terraform-use1-925323749693"
    )
    error_message = "The evidence writer requires the hosted production name, state bucket, repository and terraform_ci."
  }
}

locals {
  tls_recovery_evidence_enabled = var.tls_recovery_evidence_writer && local.terraform_ci
}

resource "aws_iam_role" "tls_recovery_evidence" {
  count                = local.tls_recovery_evidence_enabled ? 1 : 0
  name                 = "warmbly-production-tls-recovery-evidence"
  max_session_duration = 3600
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Sid       = "MainRepositoryRecoveryEnvironments"
      Effect    = "Allow"
      Principal = { Federated = aws_iam_openid_connect_provider.github[0].arn }
      Action    = "sts:AssumeRoleWithWebIdentity"
      Condition = {
        StringEquals = {
          "token.actions.githubusercontent.com:aud" = "sts.amazonaws.com"
          "token.actions.githubusercontent.com:sub" = [
            "repo:warmbly/warmbly:environment:aws-infrastructure-plan",
            "repo:warmbly/warmbly:environment:aws-infrastructure-approval",
          ]
          "token.actions.githubusercontent.com:repository_id"       = "1136203389"
          "token.actions.githubusercontent.com:repository_owner_id" = "216286592"
          "token.actions.githubusercontent.com:ref"                 = "refs/heads/main"
        }
      }
    }]
  })
  lifecycle {
    precondition {
      condition     = data.aws_caller_identity.current.account_id == "925323749693"
      error_message = "The hosted evidence writer is restricted to account 925323749693 in us-east-1."
    }
  }
}

resource "aws_iam_role_policy" "tls_recovery_evidence" {
  count = local.tls_recovery_evidence_enabled ? 1 : 0
  name  = "conditional-private-recovery-archives"
  role  = aws_iam_role.tls_recovery_evidence[0].id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Sid    = "WriteNewEncryptedRecoveryEvidence"
      Effect = "Allow"
      Action = "s3:PutObject"
      Resource = [for phase in ["plan", "apply", "verify"] :
        "arn:aws:s3:::warmbly-terraform-use1-925323749693/warmbly/production/diagnostics/runtime-tls-recovery/*/1/${phase}.tgz"
      ]
      Condition = {
        StringEquals = {
          "s3:x-amz-server-side-encryption" = "AES256"
          "s3:ResourceAccount"              = "925323749693"
        }
        Bool = { "aws:SecureTransport" = "true" }
        Null = { "s3:if-none-match" = "false" }
      }
    }]
  })
}

output "tls_recovery_evidence_role_arn" {
  value = local.tls_recovery_evidence_enabled ? aws_iam_role.tls_recovery_evidence[0].arn : null
}
