mock_provider "aws" {
  mock_data "aws_iam_policy_document" {
    defaults = { json = "{}" }
  }
  mock_resource "aws_iam_role" {
    defaults = { arn = "arn:aws:iam::123456789012:role/mock" }
  }
  mock_resource "aws_lb" {
    defaults = { arn = "arn:aws:elasticloadbalancing:us-east-1:123456789012:loadbalancer/app/mock/1", arn_suffix = "app/mock/1" }
  }
  mock_resource "aws_lb_listener" {
    defaults = { arn = "arn:aws:elasticloadbalancing:us-east-1:123456789012:listener/app/mock/1/1" }
  }
  mock_resource "aws_lb_target_group" {
    defaults = { arn = "arn:aws:elasticloadbalancing:us-east-1:123456789012:targetgroup/mock/1", arn_suffix = "targetgroup/mock/1" }
  }
  mock_resource "aws_ecs_task_definition" {
    defaults = { arn = "arn:aws:ecs:us-east-1:123456789012:task-definition/mock:1" }
  }
  mock_resource "aws_service_discovery_service" {
    defaults = { arn = "arn:aws:servicediscovery:us-east-1:123456789012:service/srv-1" }
  }
  mock_resource "aws_cloudwatch_log_group" {
    defaults = { arn = "arn:aws:logs:us-east-1:123456789012:log-group:mock" }
  }
  mock_data "aws_caller_identity" {
    defaults = { account_id = "123456789012" }
  }
}

override_data {
  target = data.terraform_remote_state.foundation
  values = {
    outputs = {
      runtime = {
        vpc_id              = "vpc-1"
        tasks_sg_id         = "sg-tasks"
        public_subnet_ids   = ["subnet-1"]
        cluster_arn         = "arn:aws:ecs:us-east-1:123456789012:cluster/warmbly-production"
        rotation_secret_arn = "arn:aws:secretsmanager:us-east-1:123456789012:secret:rotation"
        certificate_arns    = { "warmbly.com" = "arn:aws:acm:us-east-1:123456789012:certificate/1" }
        secret_arns         = { for name in ["backend", "consumer", "forms", "realtime", "tracking"] : name => "arn:aws:secretsmanager:us-east-1:123456789012:secret:${name}" }
        repository_urls     = { for name in ["backend", "consumer", "forms", "realtime", "tracking"] : name => "123456789012.dkr.ecr.us-east-1.amazonaws.com/warmbly/${name}" }
      }
      configuration_parameter_arns = {
        "backend/KAFKA_BOOTSTRAP_SERVERS" = "arn:aws:ssm:us-east-1:123456789012:parameter/a"
        "backend/SCHEMA_REGISTRY_URL"     = "arn:aws:ssm:us-east-1:123456789012:parameter/b"
        "backend/BLOB_BUCKET"             = "arn:aws:ssm:us-east-1:123456789012:parameter/c"
      }
      cache = { credential_secret = "arn:aws:secretsmanager:us-east-1:123456789012:secret:cache", endpoint = "cache" }
    }
  }
}

variables {
  state_bucket                 = "state"
  certificate_primary_domain   = "warmbly.com"
  origin_ipv4_cidrs            = ["173.245.48.0/20"]
  backend_internal_certificate = "-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n"
  services = {
    backend  = { image = "r/backend@sha256:0000000000000000000000000000000000000000000000000000000000000000", cpu = 256, memory = 512, port = 8080, host = "api.warmbly.com", secret_version_id = "v", secret_keys = ["SERVE_TLS_KEY"] }
    consumer = { image = "r/consumer@sha256:0000000000000000000000000000000000000000000000000000000000000000", cpu = 256, memory = 512, secret_version_id = "v", secret_keys = [] }
    tracking = { image = "r/tracking@sha256:0000000000000000000000000000000000000000000000000000000000000000", cpu = 256, memory = 512, port = 3000, host = "t.warmbly.com", secret_version_id = "v", secret_keys = [], environment = { BACKEND_INTERNAL_URL = "http://backend.warmbly.internal:8080" } }
    forms    = { image = "r/forms@sha256:0000000000000000000000000000000000000000000000000000000000000000", cpu = 256, memory = 512, port = 8090, host = "f.warmbly.com", secret_version_id = "v", secret_keys = [] }
    realtime = { image = "r/realtime@sha256:0000000000000000000000000000000000000000000000000000000000000000", cpu = 256, memory = 512, port = 4000, host = "ws.warmbly.com", secret_version_id = "v", secret_keys = [] }
  }
}

run "off_is_unchanged" {
  command = apply
  variables {
    target_tls = "off"
  }
  assert {
    condition     = aws_lb_target_group.service["backend"].protocol == "HTTP" && length(aws_lb_target_group.service_tls) == 0
    error_message = "off keeps the HTTP target groups only"
  }
  assert {
    condition     = !strcontains(aws_ecs_task_definition.service["backend"].container_definitions, "\"SERVE_TLS\"")
    error_message = "off sets no TLS listener"
  }
  assert {
    condition     = strcontains(aws_ecs_task_definition.service["tracking"].container_definitions, "http://backend.warmbly.internal:8080")
    error_message = "off leaves the internal hop as configured"
  }
}

run "serve_adds_listeners_only" {
  command = apply
  variables {
    target_tls = "serve"
  }
  assert {
    condition     = strcontains(aws_ecs_task_definition.service["backend"].container_definitions, "\"supplied\"") && strcontains(aws_ecs_task_definition.service["tracking"].container_definitions, "\"self-signed\"")
    error_message = "serve turns the TLS listeners on"
  }
  assert {
    condition     = strcontains(aws_ecs_task_definition.service["tracking"].container_definitions, "http://backend.warmbly.internal:8080")
    error_message = "serve leaves callers on the plain hop"
  }
  assert {
    condition     = length(aws_lb_target_group.service_tls) == 0 && aws_lb_listener_rule.service["tracking"].action[0].target_group_arn != null
    error_message = "serve does not touch load balancing"
  }
}

run "both_attaches_the_tls_group_at_weight_zero" {
  command = apply
  variables {
    target_tls = "both"
  }
  assert {
    condition     = aws_lb_target_group.service_tls["tracking"].protocol == "HTTPS" && aws_lb_target_group.service_tls["tracking"].health_check[0].protocol == "HTTPS"
    error_message = "both creates HTTPS target groups with HTTPS health checks"
  }
  assert {
    condition     = length(aws_ecs_service.service["tracking"].load_balancer) == 2
    error_message = "both registers tasks in both groups"
  }
  assert {
    condition     = [for group in aws_lb_listener_rule.service["tracking"].action[0].forward[0].target_group : group.weight] == [1, 0] || [for group in aws_lb_listener_rule.service["tracking"].action[0].forward[0].target_group : group.weight] == [0, 1]
    error_message = "both keeps the TLS group attached with no traffic"
  }
  assert {
    condition     = strcontains(aws_ecs_task_definition.service["tracking"].container_definitions, "https://backend.warmbly.internal:8443")
    error_message = "both moves the internal hop to TLS"
  }
}

run "on_forwards_tls_and_drops_plain" {
  command = apply
  variables {
    target_tls = "on"
  }
  assert {
    condition     = length(aws_lb_target_group.service) == 0 && length(aws_vpc_security_group_ingress_rule.service) == 0 && length(aws_vpc_security_group_ingress_rule.internal_backend) == 0
    error_message = "on removes the plain path"
  }
  assert {
    condition     = length(aws_ecs_service.service["backend"].load_balancer) == 1 && strcontains(aws_ecs_task_definition.service["backend"].container_definitions, "SERVE_HTTP_LOOPBACK_ONLY")
    error_message = "on keeps only the TLS group and confines plain to loopback"
  }
}

run "tls_needs_the_backend_key" {
  command = plan
  variables {
    target_tls = "serve"
    services = merge(var.services, {
      backend = { image = "r/backend@sha256:0000000000000000000000000000000000000000000000000000000000000000", cpu = 256, memory = 512, port = 8080, host = "api.warmbly.com", secret_version_id = "v", secret_keys = [] }
    })
  }
  expect_failures = [aws_ecs_task_definition.service]
}
