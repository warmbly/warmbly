terraform {
  required_version = ">= 1.11, < 2.0"
  backend "s3" {
    key          = "warmbly/production/foundation.tfstate"
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

variable "name" {
  type    = string
  default = "warmbly-production"
}

variable "vpc_cidr" {
  type    = string
  default = "10.42.0.0/16"
}

variable "planetscale_service_name" {
  description = "Private Service Name from the existing PlanetScale Postgres branch. Empty defers endpoint creation."
  type        = string
  default     = ""
}

variable "certificate_domains" {
  description = "Certificate primary names mapped to their additional names. Publish the output validation records with your DNS provider."
  type        = map(list(string))
  default     = {}
}

data "aws_availability_zones" "available" {
  state = "available"
  filter {
    name   = "opt-in-status"
    values = ["opt-in-not-required"]
  }
}

locals {
  zones    = slice(data.aws_availability_zones.available.names, 0, 2)
  services = toset(["backend", "consumer", "forms", "realtime", "tracking"])
}

resource "aws_vpc" "main" {
  cidr_block           = var.vpc_cidr
  enable_dns_support   = true
  enable_dns_hostnames = true
  # Amazon-provided /56; existing subnets stay IPv4-only, the worker fleet's subnets take /64s from it.
  assign_generated_ipv6_cidr_block = true
  tags                             = { Name = var.name }
}

resource "aws_internet_gateway" "main" {
  vpc_id = aws_vpc.main.id
}

resource "aws_subnet" "public" {
  count                   = 2
  vpc_id                  = aws_vpc.main.id
  availability_zone       = local.zones[count.index]
  cidr_block              = cidrsubnet(var.vpc_cidr, 8, count.index)
  map_public_ip_on_launch = false
  tags                    = { Name = "${var.name}-public-${count.index + 1}" }
}

resource "aws_subnet" "private" {
  count             = 2
  vpc_id            = aws_vpc.main.id
  availability_zone = local.zones[count.index]
  cidr_block        = cidrsubnet(var.vpc_cidr, 8, count.index + 10)
  tags              = { Name = "${var.name}-private-${count.index + 1}" }
}

resource "aws_route_table" "public" {
  vpc_id = aws_vpc.main.id
}

resource "aws_route" "internet" {
  route_table_id         = aws_route_table.public.id
  destination_cidr_block = "0.0.0.0/0"
  gateway_id             = aws_internet_gateway.main.id
}

resource "aws_route_table_association" "public" {
  count          = 2
  subnet_id      = aws_subnet.public[count.index].id
  route_table_id = aws_route_table.public.id
}

resource "aws_route_table" "private" {
  vpc_id = aws_vpc.main.id
}

resource "aws_route_table_association" "private" {
  count          = 2
  subnet_id      = aws_subnet.private[count.index].id
  route_table_id = aws_route_table.private.id
}

resource "aws_security_group" "tasks" {
  name_prefix = "${var.name}-tasks-"
  description = "Application tasks; inbound rules are added with the runtime deployment"
  vpc_id      = aws_vpc.main.id
}

resource "aws_vpc_security_group_egress_rule" "tasks" {
  security_group_id = aws_security_group.tasks.id
  ip_protocol       = "-1"
  cidr_ipv4         = "0.0.0.0/0"
}

resource "aws_security_group" "database_endpoint" {
  name_prefix = "${var.name}-database-"
  description = "PlanetScale endpoint, reachable only by application tasks"
  vpc_id      = aws_vpc.main.id
}

resource "aws_vpc_security_group_ingress_rule" "database" {
  for_each                     = toset(["5432", "6432"])
  security_group_id            = aws_security_group.database_endpoint.id
  referenced_security_group_id = aws_security_group.tasks.id
  ip_protocol                  = "tcp"
  from_port                    = tonumber(each.value)
  to_port                      = tonumber(each.value)
}

resource "aws_vpc_endpoint" "s3" {
  vpc_id            = aws_vpc.main.id
  service_name      = "com.amazonaws.us-east-1.s3"
  vpc_endpoint_type = "Gateway"
  route_table_ids   = [aws_route_table.public.id, aws_route_table.private.id]
}

resource "aws_vpc_endpoint" "database" {
  count               = var.planetscale_service_name == "" ? 0 : 1
  vpc_id              = aws_vpc.main.id
  service_name        = var.planetscale_service_name
  vpc_endpoint_type   = "Interface"
  subnet_ids          = aws_subnet.private[*].id
  security_group_ids  = [aws_security_group.database_endpoint.id]
  private_dns_enabled = true
}

resource "aws_ecs_cluster" "main" {
  name = var.name
}

resource "aws_ecs_cluster_capacity_providers" "main" {
  cluster_name       = aws_ecs_cluster.main.name
  capacity_providers = ["FARGATE", "FARGATE_SPOT"]
  default_capacity_provider_strategy {
    capacity_provider = "FARGATE"
    weight            = 1
  }
}

resource "aws_ecr_repository" "service" {
  for_each             = local.services
  name                 = "warmbly/${each.key}"
  image_tag_mutability = "IMMUTABLE"
  image_scanning_configuration {
    scan_on_push = true
  }
}

resource "aws_cloudwatch_log_group" "service" {
  for_each          = local.services
  name              = "/ecs/${var.name}/${each.key}"
  retention_in_days = 14
}

resource "aws_secretsmanager_secret" "service" {
  for_each                = local.services
  name                    = "${var.name}/${each.key}"
  description             = "Runtime configuration for ${each.key}; values are populated outside Terraform"
  recovery_window_in_days = 30
}

resource "aws_secretsmanager_secret" "rotation" {
  name                    = "${var.name}/rotation"
  description             = "Pending key replacements for coordinated application and worker rollout"
  recovery_window_in_days = 30
}

resource "aws_acm_certificate" "service" {
  for_each                  = var.certificate_domains
  domain_name               = each.key
  subject_alternative_names = each.value
  validation_method         = "DNS"
  lifecycle {
    create_before_destroy = true
  }
}

output "runtime" {
  value = {
    cluster_arn         = aws_ecs_cluster.main.arn
    vpc_id              = aws_vpc.main.id
    public_subnet_ids   = aws_subnet.public[*].id
    private_subnet_ids  = aws_subnet.private[*].id
    tasks_sg_id         = aws_security_group.tasks.id
    secret_arns         = { for name, secret in aws_secretsmanager_secret.service : name => secret.arn }
    rotation_secret_arn = aws_secretsmanager_secret.rotation.arn
    repository_urls     = { for name, repo in aws_ecr_repository.service : name => repo.repository_url }
    database_endpoint   = aws_vpc_endpoint.database[*].id
    certificate_arns    = { for name, cert in aws_acm_certificate.service : name => cert.arn }
    certificate_records = { for name, cert in aws_acm_certificate.service : name => cert.domain_validation_options }
  }
}
