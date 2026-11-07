variable "service_configuration" {
  description = "Non-secret environment values stored as individual Standard String parameters"
  type        = map(map(string))
  default     = {}

  validation {
    condition = alltrue(flatten([
      for service, configuration in var.service_configuration : [
        contains(["backend", "consumer", "forms", "realtime", "tracking"], service),
        alltrue([
          for key, value in configuration : contains([
            "APP_ENV", "APP_URL", "API_PUBLIC_URL", "API_HOST", "FORMS_DOMAIN",
            "TRACKING_DOMAIN", "WEBSOCKET_URL", "CORS_ALLOW_ORIGINS",
            "CHECK_ORIGIN_HOSTS", "PHX_HOST", "PORT", "FORMS_PORT", "TRACKING_HOST",
            "EVENTBUS_PROVIDER", "CODEC_PROVIDER", "KAFKA_BOOTSTRAP_SERVERS",
            "NATS_URL", "NATS_STREAM_NAME", "NATS_SUBJECT_PREFIX", "NATS_MAX_BYTES",
            "NATS_REPLICAS", "NATS_DISCARD_NEW", "NATS_TLS_CA_B64",
            "EVENTBUS_READ_BOTH", "EVENTBUS_HOLD_READS", "CODEC_READ_BOTH",
            "SCHEMA_REGISTRY_URL", "KMS_PROVIDER", "KMS_AWS_KEY_ID",
            "ENCRYPTED_KEYS_PROVIDER", "BLOB_PROVIDER", "BLOB_BUCKET",
            "BLOB_PUBLIC_BASE_URL", "AWS_REGION", "MAIL_TRANSPORT", "EMAIL_NAME",
            "EMAIL_ADDRESS", "BILLING_PROVIDER", "DEPLOYMENT_MODE", "WEBAUTHN_RP_ID",
            "WEBAUTHN_RP_ORIGINS", "INBOX_TAGGING_ENABLED", "RELEASES_ENABLED",
            "GEODB_PATH", "DATABASE_POOL_SIZE", "DATABASE_SSL_CA_FILE",
            "SEARCH_PROVIDER", "AI_PROVIDER", "AI_MODEL"
          ], key) && !can(regex("://[^/@]+@", value)) && !can(regex("(?i)(password|secret|token|api_key)=", value))
        ])
      ]
    ]))
    error_message = "Only approved non-secret configuration names and values may enter Parameter Store. Store credentials and connection strings in Secrets Manager."
  }
}

locals {
  configuration_parameters = merge({}, [
    for service, configuration in var.service_configuration : {
      for key, value in configuration : "${service}/${key}" => value
    }
  ]...)
}

resource "aws_ssm_parameter" "configuration" {
  for_each = local.configuration_parameters
  name     = "/${var.name}/config/${each.key}"
  type     = "String"
  tier     = "Standard"
  value    = each.value
}

output "configuration_parameter_arns" {
  value = { for key, parameter in aws_ssm_parameter.configuration : key => parameter.arn }
}
