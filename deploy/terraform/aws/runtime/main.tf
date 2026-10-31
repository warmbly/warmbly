terraform {
  required_version = ">= 1.11, < 2.0"
  backend "s3" {
    key          = "warmbly/production/runtime.tfstate"
    region       = "us-east-1"
    encrypt      = true
    use_lockfile = true
  }
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 6.0"
    }
  }
}

provider "aws" {
  region = "us-east-1"
  default_tags {
    tags = {
      Application = "warmbly"
      Environment = "production"
      ManagedBy   = "terraform"
    }
  }
}

variable "state_bucket" { type = string }
variable "name" {
  type    = string
  default = "warmbly-production"
}
variable "namespace" {
  type    = string
  default = "warmbly.internal"
}
variable "certificate_primary_domain" { type = string }
variable "origin_ipv4_cidrs" {
  type = set(string)
  validation {
    condition     = length(var.origin_ipv4_cidrs) > 0 && alltrue([for cidr in var.origin_ipv4_cidrs : can(cidrnetmask(cidr)) && cidr != "0.0.0.0/0"])
    error_message = "Provide explicit origin caller CIDRs, such as Cloudflare's published IPv4 ranges."
  }
}
variable "application_policies" {
  description = "Reviewed runtime policy JSON by service, supplied outside the checkout"
  type        = map(string)
  default     = {}
}
variable "services" {
  description = "Digest-pinned images and secret metadata; secret values never enter Terraform"
  type = map(object({
    image             = string
    cpu               = number
    memory            = number
    desired_count     = optional(number, 0)
    port              = optional(number)
    host              = optional(string)
    health_path       = optional(string, "/health")
    secret_version_id = string
    secret_keys       = set(string)
    environment       = optional(map(string), {})
    # kafka when the service runs the -kafka build; the deploy reads it off the task definition tag.
    image_variant = optional(string, "default")
  }))
  validation {
    condition = (var.github_repository == null || alltrue([for name in ["backend", "consumer"] : contains(keys(var.services), name)])) && alltrue([for name, service in var.services :
      contains(["backend", "consumer", "forms", "realtime", "tracking"], name) &&
      contains(["default", "kafka"], service.image_variant) &&
      can(regex("@sha256:[a-f0-9]{64}$", service.image)) &&
      ((service.cpu == 256 && contains([512, 1024, 2048], service.memory)) ||
        (service.cpu == 512 && contains([1024, 2048, 3072, 4096], service.memory)) ||
        (service.cpu == 1024 && service.memory >= 2048 && service.memory <= 8192 && service.memory % 1024 == 0) ||
      (service.cpu == 2048 && service.memory >= 4096 && service.memory <= 16384 && service.memory % 1024 == 0)) &&
      service.desired_count >= 0 && service.desired_count == floor(service.desired_count) &&
      !contains(service.secret_keys, "AWS_ACCESS_KEY_ID") && !contains(service.secret_keys, "AWS_SECRET_ACCESS_KEY") &&
      alltrue([for key, value in service.environment :
        !can(regex("(?i)(passwords?|secrets?|tokens?|private_keys?|access_keys?|master_keys?|encryption_keys?|tls_keys?|read_keys|primary_db(_direct)?|database_url|redis_url)$", key)) &&
        !can(regex("://[^/@]+:[^/@]+@", value))
      ])
    ])
    error_message = "Use supported Fargate CPU/memory pairs, known services, immutable image digests, integer counts and secret references rather than inline credentials. GitHub deployment requires backend and consumer services."
  }
}

variable "target_tls" {
  description = "Stage of the move to TLS behind the load balancer: off, serve, both, route, drain, on. Advance one stage per apply and let each redeploy finish"
  type        = string
  default     = "off"
  validation {
    condition     = contains(["off", "serve", "both", "route", "drain", "on"], var.target_tls)
    error_message = "target_tls is off, serve, both, route, drain or on."
  }
}
variable "target_tls_port" {
  description = "Port every load-balanced task serves TLS on"
  type        = number
  default     = 8443
  validation {
    condition     = var.target_tls_port >= 1024 && var.target_tls_port <= 65535 && floor(var.target_tls_port) == var.target_tls_port && alltrue([for service in var.services : service.port != var.target_tls_port])
    error_message = "target_tls_port is an unprivileged port no service already listens on."
  }
}
variable "backend_internal_certificate" {
  description = "PEM certificate the backend presents on its TLS port, naming backend.<namespace>; its key is SERVE_TLS_KEY in the backend secret"
  type        = string
  default     = null
  validation {
    condition     = var.backend_internal_certificate == null || can(regex("^-----BEGIN CERTIFICATE-----", trimspace(var.backend_internal_certificate)))
    error_message = "backend_internal_certificate is one PEM certificate."
  }
}

data "terraform_remote_state" "foundation" {
  backend = "s3"
  config = {
    bucket = var.state_bucket
    key    = "warmbly/production/foundation.tfstate"
    region = "us-east-1"
  }
}
data "aws_caller_identity" "current" {}
locals {
  foundation = data.terraform_remote_state.foundation.outputs.runtime
  parameters = data.terraform_remote_state.foundation.outputs.configuration_parameter_arns
  public_services = {
    for name, service in var.services : name => service if service.port != null && service.host != null
  }
  # Each stage keeps the previous one's listeners, so a rolling deploy never meets a task that cannot answer.
  tls_serving  = var.target_tls != "off"
  http_targets = var.target_tls != "on"
  tls_targets  = contains(["both", "route", "drain", "on"], var.target_tls)
  tls_services = {
    for name, service in var.services : name => service if service.port != null && (contains(keys(local.public_services), name) || name == "backend")
  }
  backend_internal_url = local.tls_targets ? "https://backend.${var.namespace}:${var.target_tls_port}" : "http://backend.${var.namespace}:8080"
  tls_environment = {
    for name, service in var.services : name => merge(
      local.tls_serving && contains(keys(local.tls_services), name) ? {
        SERVE_TLS      = name == "backend" ? "supplied" : "self-signed"
        SERVE_TLS_PORT = tostring(var.target_tls_port)
      } : {},
      local.tls_serving && name == "backend" ? { SERVE_TLS_CERT = var.backend_internal_certificate } : {},
      var.target_tls == "on" && contains(keys(local.tls_services), name) ? { SERVE_HTTP_LOOPBACK_ONLY = "true" } : {},
      local.tls_targets && contains(["forms", "tracking"], name) ? {
        BACKEND_INTERNAL_URL = local.backend_internal_url
        BACKEND_INTERNAL_CA  = var.backend_internal_certificate
      } : {},
    )
  }
  service_environment = {
    for name, service in var.services : name => merge(service.environment, name == "backend" ? { DB_MIGRATIONS_ON_START = "false" } : {}, local.tls_environment[name])
  }
  parameter_secrets = {
    for name, service in var.services : name => [
      for key, arn in local.parameters : {
        name      = split("/", key)[1]
        valueFrom = arn
      } if startswith(key, "${name}/") && !contains(keys(local.service_environment[name]), split("/", key)[1]) && !contains(service.secret_keys, split("/", key)[1])
    ]
  }
}

data "aws_iam_policy_document" "trust" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["ecs-tasks.amazonaws.com"]
    }
    condition {
      test     = "StringEquals"
      variable = "aws:SourceAccount"
      values   = [data.aws_caller_identity.current.account_id]
    }
    condition {
      test     = "ArnLike"
      variable = "aws:SourceArn"
      values   = ["arn:aws:ecs:us-east-1:${data.aws_caller_identity.current.account_id}:*"]
    }
  }
}
resource "aws_iam_role" "execution" {
  for_each           = var.services
  name               = "${var.name}-${each.key}-execution"
  assume_role_policy = data.aws_iam_policy_document.trust.json
}
resource "aws_iam_role" "application" {
  for_each           = var.services
  name               = "${var.name}-${each.key}-task"
  assume_role_policy = data.aws_iam_policy_document.trust.json
}
data "aws_iam_policy_document" "execution" {
  for_each = var.services
  statement {
    actions   = ["secretsmanager:GetSecretValue"]
    resources = [local.foundation.secret_arns[each.key]]
  }
  statement {
    actions   = ["ssm:GetParameters"]
    resources = ["arn:aws:ssm:us-east-1:${data.aws_caller_identity.current.account_id}:parameter/${var.name}/config/${each.key}/*"]
  }
  statement {
    actions   = ["logs:CreateLogStream", "logs:PutLogEvents"]
    resources = ["arn:aws:logs:us-east-1:${data.aws_caller_identity.current.account_id}:log-group:/ecs/${var.name}/${each.key}:*"]
  }
  statement {
    actions   = ["ecr:GetAuthorizationToken"]
    resources = ["*"]
  }
  statement {
    actions   = ["ecr:BatchCheckLayerAvailability", "ecr:GetDownloadUrlForLayer", "ecr:BatchGetImage"]
    resources = ["arn:aws:ecr:us-east-1:${data.aws_caller_identity.current.account_id}:repository/${split(".amazonaws.com/", local.foundation.repository_urls[each.key])[1]}"]
  }
}
resource "aws_iam_role_policy" "execution" {
  for_each = var.services
  name     = "runtime-configuration-and-logs"
  role     = aws_iam_role.execution[each.key].id
  policy   = data.aws_iam_policy_document.execution[each.key].json
}
resource "aws_iam_role_policy" "application" {
  for_each = var.application_policies
  name     = "application-services"
  role     = aws_iam_role.application[each.key].id
  policy   = each.value
}

resource "aws_security_group" "ingress" {
  name_prefix = "${var.name}-ingress-"
  description = "HTTPS origin access from approved callers"
  vpc_id      = local.foundation.vpc_id
}
resource "aws_vpc_security_group_ingress_rule" "https" {
  for_each          = var.origin_ipv4_cidrs
  security_group_id = aws_security_group.ingress.id
  cidr_ipv4         = each.value
  ip_protocol       = "tcp"
  from_port         = 443
  to_port           = 443
}
resource "aws_vpc_security_group_ingress_rule" "internal_https" {
  security_group_id            = aws_security_group.ingress.id
  referenced_security_group_id = local.foundation.tasks_sg_id
  ip_protocol                  = "tcp"
  from_port                    = 443
  to_port                      = 443
}
resource "aws_vpc_security_group_egress_rule" "ingress" {
  security_group_id            = aws_security_group.ingress.id
  referenced_security_group_id = local.foundation.tasks_sg_id
  ip_protocol                  = "tcp"
  from_port                    = 1
  to_port                      = 65535
}
resource "aws_vpc_security_group_ingress_rule" "service" {
  for_each                     = local.http_targets ? local.public_services : {}
  security_group_id            = local.foundation.tasks_sg_id
  referenced_security_group_id = aws_security_group.ingress.id
  ip_protocol                  = "tcp"
  from_port                    = each.value.port
  to_port                      = each.value.port
}
resource "aws_vpc_security_group_ingress_rule" "service_tls" {
  count                        = local.tls_serving && length(local.public_services) > 0 ? 1 : 0
  security_group_id            = local.foundation.tasks_sg_id
  referenced_security_group_id = aws_security_group.ingress.id
  ip_protocol                  = "tcp"
  from_port                    = var.target_tls_port
  to_port                      = var.target_tls_port
}
moved {
  from = aws_vpc_security_group_ingress_rule.internal_backend
  to   = aws_vpc_security_group_ingress_rule.internal_backend[0]
}
resource "aws_vpc_security_group_ingress_rule" "internal_backend" {
  count                        = local.http_targets ? 1 : 0
  security_group_id            = local.foundation.tasks_sg_id
  referenced_security_group_id = local.foundation.tasks_sg_id
  ip_protocol                  = "tcp"
  from_port                    = 8080
  to_port                      = 8080
}
resource "aws_vpc_security_group_ingress_rule" "internal_backend_tls" {
  count                        = local.tls_serving ? 1 : 0
  security_group_id            = local.foundation.tasks_sg_id
  referenced_security_group_id = local.foundation.tasks_sg_id
  ip_protocol                  = "tcp"
  from_port                    = var.target_tls_port
  to_port                      = var.target_tls_port
}
resource "aws_lb" "main" {
  name                       = "${var.name}-ingress"
  load_balancer_type         = "application"
  internal                   = false
  security_groups            = [aws_security_group.ingress.id]
  subnets                    = local.foundation.public_subnet_ids
  idle_timeout               = 120
  drop_invalid_header_fields = true
}
resource "aws_lb_target_group" "service" {
  for_each             = local.http_targets ? local.public_services : {}
  name                 = "${var.name}-${each.key}"
  port                 = each.value.port
  protocol             = "HTTP"
  target_type          = "ip"
  vpc_id               = local.foundation.vpc_id
  deregistration_delay = 60
  health_check {
    path                = each.value.health_path
    matcher             = "200"
    interval            = 30
    timeout             = 5
    healthy_threshold   = 2
    unhealthy_threshold = 3
  }
}
# A separate group per protocol, so traffic moves between two populated groups and never to an empty one.
resource "aws_lb_target_group" "service_tls" {
  for_each             = local.tls_targets ? local.public_services : {}
  name                 = "${var.name}-${each.key}-tls"
  port                 = var.target_tls_port
  protocol             = "HTTPS"
  target_type          = "ip"
  vpc_id               = local.foundation.vpc_id
  deregistration_delay = 60
  health_check {
    protocol            = "HTTPS"
    path                = each.value.health_path
    matcher             = "200"
    interval            = 30
    timeout             = 5
    healthy_threshold   = 2
    unhealthy_threshold = 3
  }
  lifecycle {
    precondition {
      condition     = length("${var.name}-${each.key}-tls") <= 32
      error_message = "Target group names are at most 32 characters; shorten name."
    }
  }
}
locals {
  # The group the listener sends traffic to; in both, the TLS group is attached at weight 0 while it fills.
  serving_target_groups = contains(["route", "drain", "on"], var.target_tls) ? aws_lb_target_group.service_tls : aws_lb_target_group.service
}
resource "aws_lb_listener" "https" {
  load_balancer_arn = aws_lb.main.arn
  port              = 443
  protocol          = "HTTPS"
  ssl_policy        = "ELBSecurityPolicy-TLS13-1-2-2021-06"
  certificate_arn   = local.foundation.certificate_arns[var.certificate_primary_domain]
  default_action {
    type = "fixed-response"
    fixed_response {
      content_type = "text/plain"
      status_code  = "404"
      message_body = "Not found"
    }
  }
}
resource "aws_lb_listener_certificate" "additional" {
  for_each        = { for domain, arn in local.foundation.certificate_arns : domain => arn if domain != var.certificate_primary_domain }
  listener_arn    = aws_lb_listener.https.arn
  certificate_arn = each.value
}
resource "aws_lb_listener_rule" "service" {
  for_each     = local.public_services
  listener_arn = aws_lb_listener.https.arn
  priority     = 100 + index(sort(keys(local.public_services)), each.key)
  condition {
    host_header { values = [each.value.host] }
  }
  action {
    type             = "forward"
    target_group_arn = contains(["both", "route"], var.target_tls) ? null : local.serving_target_groups[each.key].arn
    dynamic "forward" {
      for_each = contains(["both", "route"], var.target_tls) ? [each.key] : []
      content {
        target_group {
          arn    = aws_lb_target_group.service[forward.value].arn
          weight = var.target_tls == "both" ? 1 : 0
        }
        target_group {
          arn    = aws_lb_target_group.service_tls[forward.value].arn
          weight = var.target_tls == "route" ? 1 : 0
        }
      }
    }
  }
  # A port's security group rule is removed only after no rule forwards to it.
  depends_on = [aws_vpc_security_group_ingress_rule.service, aws_vpc_security_group_ingress_rule.service_tls]
}

resource "aws_service_discovery_private_dns_namespace" "main" {
  name = var.namespace
  vpc  = local.foundation.vpc_id
}
resource "aws_service_discovery_service" "backend" {
  name = "backend"
  dns_config {
    namespace_id = aws_service_discovery_private_dns_namespace.main.id
    dns_records {
      ttl  = 10
      type = "A"
    }
    routing_policy = "MULTIVALUE"
  }
  health_check_custom_config {}
  lifecycle {
    ignore_changes = [health_check_custom_config]
  }
}

locals {
  service_containers = {
    for name, service in var.services : name => merge({
      name            = name
      image           = service.image
      essential       = true
      stopTimeout     = 120
      linuxParameters = { initProcessEnabled = true }
      environment     = [for key, value in local.service_environment[name] : { name = key, value = value }]
      secrets = concat(local.parameter_secrets[name], [for key in sort(tolist(service.secret_keys)) : {
        name      = key
        valueFrom = "${local.foundation.secret_arns[name]}:${key}::${service.secret_version_id}"
      }])
      logConfiguration = {
        logDriver = "awslogs"
        options = {
          awslogs-group         = "/ecs/${var.name}/${name}"
          awslogs-region        = "us-east-1"
          awslogs-stream-prefix = "service"
        }
      }
      }, jsondecode(service.port == null ? "{}" : jsonencode({
        portMappings = concat([{ containerPort = service.port, protocol = "tcp" }], local.tls_serving && contains(keys(local.tls_services), name) ? [{ containerPort = var.target_tls_port, protocol = "tcp" }] : [])
        healthCheck = {
          command     = ["CMD-SHELL", "wget -q --spider http://127.0.0.1:${service.port}${service.health_path} || exit 1"]
          interval    = 30
          timeout     = 5
          retries     = 3
          startPeriod = 60
        }
    })))
  }
}

resource "aws_ecs_task_definition" "service" {
  for_each                 = var.services
  skip_destroy             = true
  family                   = "${var.name}-${each.key}"
  requires_compatibilities = ["FARGATE"]
  network_mode             = "awsvpc"
  cpu                      = each.value.cpu
  memory                   = each.value.memory
  execution_role_arn       = aws_iam_role.execution[each.key].arn
  task_role_arn            = aws_iam_role.application[each.key].arn
  runtime_platform {
    cpu_architecture        = "ARM64"
    operating_system_family = "LINUX"
  }
  container_definitions = jsonencode([local.service_containers[each.key]])
  tags                  = { "warmbly-image-variant" = each.value.image_variant }
  depends_on            = [aws_iam_role_policy.execution, aws_iam_role_policy.application]
  lifecycle {
    precondition {
      condition     = each.key != "backend" || !local.tls_serving || (var.backend_internal_certificate != null && contains(each.value.secret_keys, "SERVE_TLS_KEY"))
      error_message = "target_tls needs backend_internal_certificate and SERVE_TLS_KEY in the backend secret_keys."
    }
    precondition {
      condition     = !local.tls_serving || length(setintersection(each.value.secret_keys, ["BACKEND_INTERNAL_URL", "BACKEND_INTERNAL_CA", "SERVE_TLS", "SERVE_TLS_PORT", "SERVE_TLS_CERT"])) == 0
      error_message = "target_tls sets BACKEND_INTERNAL_URL, BACKEND_INTERNAL_CA and the SERVE_TLS settings itself; remove them from secret_keys."
    }
  }
}
resource "aws_ecs_service" "service" {
  for_each                           = var.services
  name                               = "${var.name}-${each.key}"
  cluster                            = local.foundation.cluster_arn
  task_definition                    = aws_ecs_task_definition.service[each.key].arn
  desired_count                      = each.value.desired_count
  launch_type                        = "FARGATE"
  platform_version                   = "1.4.0"
  deployment_minimum_healthy_percent = 100
  deployment_maximum_percent         = 200
  health_check_grace_period_seconds  = contains(keys(local.public_services), each.key) ? 120 : null
  # A target group change finishes rolling before the next resource relies on it.
  wait_for_steady_state   = local.tls_targets
  enable_ecs_managed_tags = true
  propagate_tags          = "SERVICE"
  lifecycle {
    ignore_changes = [task_definition, desired_count]
  }
  deployment_circuit_breaker {
    enable   = true
    rollback = true
  }
  dynamic "alarms" {
    for_each = contains(keys(local.public_services), each.key) ? [1] : []
    content {
      alarm_names = [aws_cloudwatch_metric_alarm.target_5xx[each.key].alarm_name, aws_cloudwatch_metric_alarm.unhealthy_hosts[each.key].alarm_name]
      enable      = true
      rollback    = true
    }
  }
  network_configuration {
    subnets          = local.foundation.public_subnet_ids
    security_groups  = [local.foundation.tasks_sg_id]
    assign_public_ip = true
  }
  dynamic "load_balancer" {
    for_each = contains(keys(local.public_services), each.key) && local.http_targets && var.target_tls != "drain" ? [each.value.port] : []
    content {
      target_group_arn = aws_lb_target_group.service[each.key].arn
      container_name   = each.key
      container_port   = load_balancer.value
    }
  }
  dynamic "load_balancer" {
    for_each = contains(keys(local.public_services), each.key) && local.tls_targets ? [var.target_tls_port] : []
    content {
      target_group_arn = aws_lb_target_group.service_tls[each.key].arn
      container_name   = each.key
      container_port   = load_balancer.value
    }
  }
  dynamic "service_registries" {
    for_each = each.key == "backend" ? [1] : []
    content { registry_arn = aws_service_discovery_service.backend.arn }
  }
  depends_on = [aws_lb_listener_rule.service]
}

# Named ${var.name}-<service>-*: the deploy bakes each service against every alarm with that prefix.
resource "aws_cloudwatch_metric_alarm" "target_5xx" {
  for_each            = local.public_services
  alarm_name          = "${var.name}-${each.key}-target-5xx-rate"
  alarm_description   = "More than 5% of requests to ${each.key} answered 5xx"
  comparison_operator = "GreaterThanThreshold"
  threshold           = 5
  evaluation_periods  = 3
  datapoints_to_alarm = 2
  treat_missing_data  = "notBreaching"
  metric_query {
    id          = "rate"
    expression  = "IF(requests >= 20, 100 * errors / requests, 0)"
    label       = "5xx percent"
    return_data = true
  }
  metric_query {
    id = "errors"
    metric {
      namespace   = "AWS/ApplicationELB"
      metric_name = "HTTPCode_Target_5XX_Count"
      period      = 60
      stat        = "Sum"
      dimensions  = { LoadBalancer = aws_lb.main.arn_suffix, TargetGroup = local.serving_target_groups[each.key].arn_suffix }
    }
  }
  metric_query {
    id = "requests"
    metric {
      namespace   = "AWS/ApplicationELB"
      metric_name = "RequestCount"
      period      = 60
      stat        = "Sum"
      dimensions  = { LoadBalancer = aws_lb.main.arn_suffix, TargetGroup = local.serving_target_groups[each.key].arn_suffix }
    }
  }
}
resource "aws_cloudwatch_metric_alarm" "unhealthy_hosts" {
  for_each            = local.public_services
  alarm_name          = "${var.name}-${each.key}-unhealthy-hosts"
  alarm_description   = "A ${each.key} target has failed its load balancer health check for three minutes"
  namespace           = "AWS/ApplicationELB"
  metric_name         = "UnHealthyHostCount"
  dimensions          = { LoadBalancer = aws_lb.main.arn_suffix, TargetGroup = local.serving_target_groups[each.key].arn_suffix }
  statistic           = "Maximum"
  period              = 60
  evaluation_periods  = 3
  comparison_operator = "GreaterThanOrEqualToThreshold"
  threshold           = 1
  treat_missing_data  = "notBreaching"
}

output "runtime" {
  value = {
    cluster_arn       = local.foundation.cluster_arn
    load_balancer_dns = aws_lb.main.dns_name
    load_balancer_arn = aws_lb.main.arn
    target_group_arns = { for name, group in local.serving_target_groups : name => group.arn }
    task_definitions  = { for name, task in aws_ecs_task_definition.service : name => task.arn }
    task_roles        = { for name, role in aws_iam_role.application : name => role.arn }
    execution_roles   = { for name, role in aws_iam_role.execution : name => role.arn }
    backend_internal  = local.backend_internal_url
  }
}
