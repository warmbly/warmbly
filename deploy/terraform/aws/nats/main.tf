terraform {
  required_version = ">= 1.11, < 2.0"
  backend "s3" {
    key          = "warmbly/production/nats.tfstate"
    region       = "us-east-1"
    encrypt      = true
    use_lockfile = true
  }
  required_providers {
    aws = { source = "hashicorp/aws", version = "~> 6.0" }
  }
}

provider "aws" {
  region = "us-east-1"
  default_tags {
    tags = { Application = "warmbly", Environment = "production", ManagedBy = "terraform" }
  }
}

variable "state_bucket" { type = string }
variable "name" {
  type    = string
  default = "warmbly-production"
}
variable "worker_security_group_id" { type = string }
variable "server_version" {
  type    = string
  default = "2.15.2"
  validation {
    condition     = can(regex("^[0-9]+\\.[0-9]+\\.[0-9]+$", var.server_version))
    error_message = "Pin a stable NATS server version."
  }
}
variable "server_sha256" {
  type = string
  validation {
    condition     = can(regex("^[a-f0-9]{64}$", var.server_sha256))
    error_message = "Supply the published linux-arm64 archive SHA256."
  }
}

data "terraform_remote_state" "foundation" {
  backend = "s3"
  config  = { bucket = var.state_bucket, key = "warmbly/production/foundation.tfstate", region = "us-east-1" }
}
data "aws_vpc" "main" { id = data.terraform_remote_state.foundation.outputs.runtime.vpc_id }
data "aws_subnet" "existing" {
  count = 2
  id    = data.terraform_remote_state.foundation.outputs.runtime.public_subnet_ids[count.index]
}
data "aws_route_table" "public" { subnet_id = data.aws_subnet.existing[0].id }
data "aws_availability_zones" "available" { state = "available" }
data "aws_ssm_parameter" "ami" { name = "/aws/service/ami-amazon-linux-latest/al2023-ami-kernel-default-arm64" }

locals {
  third_zone = sort(tolist(setsubtract(toset(data.aws_availability_zones.available.names), toset(data.aws_subnet.existing[*].availability_zone))))[0]
  nodes = {
    nats1 = { subnet = data.aws_subnet.existing[0].id, address = cidrhost(data.aws_subnet.existing[0].cidr_block, 40) }
    nats2 = { subnet = data.aws_subnet.existing[1].id, address = cidrhost(data.aws_subnet.existing[1].cidr_block, 40) }
    nats3 = { subnet = aws_subnet.third.id, address = cidrhost(aws_subnet.third.cidr_block, 40) }
  }
}

resource "aws_subnet" "third" {
  vpc_id            = data.aws_vpc.main.id
  availability_zone = local.third_zone
  cidr_block        = cidrsubnet(data.aws_vpc.main.cidr_block, 8, 2)
  tags              = { Name = "${var.name}-nats-third-zone" }
}
resource "aws_route_table_association" "third" {
  subnet_id      = aws_subnet.third.id
  route_table_id = data.aws_route_table.public.id
}
resource "aws_route53_zone" "nats" {
  name = "nats.warmbly.internal"
  vpc { vpc_id = data.aws_vpc.main.id }
}
resource "aws_route53_record" "node" {
  for_each = local.nodes
  zone_id  = aws_route53_zone.nats.zone_id
  name     = "${each.key}.${aws_route53_zone.nats.name}"
  type     = "A"
  ttl      = 30
  records  = [each.value.address]
}
resource "aws_security_group" "nats" {
  name_prefix = "${var.name}-nats-"
  description = "Private NATS clients and mutually authenticated cluster routes; no public inbound access"
  vpc_id      = data.aws_vpc.main.id
}
resource "aws_vpc_security_group_ingress_rule" "clients" {
  for_each = {
    tasks   = data.terraform_remote_state.foundation.outputs.runtime.tasks_sg_id
    workers = var.worker_security_group_id
  }
  security_group_id            = aws_security_group.nats.id
  referenced_security_group_id = each.value
  ip_protocol                  = "tcp"
  from_port                    = 4222
  to_port                      = 4222
  lifecycle {
    precondition {
      condition     = var.worker_security_group_id != data.terraform_remote_state.foundation.outputs.runtime.tasks_sg_id
      error_message = "Use distinct worker and task security groups."
    }
  }
}
resource "aws_vpc_security_group_ingress_rule" "routes" {
  security_group_id            = aws_security_group.nats.id
  referenced_security_group_id = aws_security_group.nats.id
  ip_protocol                  = "tcp"
  from_port                    = 6222
  to_port                      = 6222
}
resource "aws_vpc_security_group_egress_rule" "outbound" {
  security_group_id = aws_security_group.nats.id
  ip_protocol       = "-1"
  cidr_ipv4         = "0.0.0.0/0"
}
resource "aws_secretsmanager_secret" "node" {
  for_each                = local.nodes
  name                    = "${var.name}/nats/${each.key}"
  description             = "NATS configuration and private TLS keys; populated outside Terraform"
  recovery_window_in_days = 30
  lifecycle { prevent_destroy = true }
}
resource "aws_secretsmanager_secret" "client" {
  name                    = "${var.name}/nats/client"
  description             = "Application NATS credentials; populated outside Terraform"
  recovery_window_in_days = 30
  lifecycle { prevent_destroy = true }
}
resource "aws_secretsmanager_secret" "authority" {
  name                    = "${var.name}/nats/authority"
  description             = "Offline certificate and account signing keys; inaccessible to broker and application roles"
  recovery_window_in_days = 30
  lifecycle { prevent_destroy = true }
}
data "aws_iam_policy_document" "trust" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["ec2.amazonaws.com"]
    }
  }
}
resource "aws_iam_role" "node" {
  for_each           = local.nodes
  name               = "${var.name}-${each.key}"
  assume_role_policy = data.aws_iam_policy_document.trust.json
}
resource "aws_iam_role_policy_attachment" "ssm" {
  for_each   = local.nodes
  role       = aws_iam_role.node[each.key].name
  policy_arn = "arn:aws:iam::aws:policy/AmazonSSMManagedInstanceCore"
}
data "aws_iam_policy_document" "node" {
  for_each = local.nodes
  statement {
    actions   = ["secretsmanager:GetSecretValue"]
    resources = [aws_secretsmanager_secret.node[each.key].arn]
  }
  statement {
    actions   = ["cloudwatch:PutMetricData"]
    resources = ["*"]
    condition {
      test     = "StringEquals"
      variable = "cloudwatch:namespace"
      values   = ["Warmbly/NATS"]
    }
  }
}
resource "aws_iam_role_policy" "node" {
  for_each = local.nodes
  name     = "nats-bootstrap-and-health"
  role     = aws_iam_role.node[each.key].id
  policy   = data.aws_iam_policy_document.node[each.key].json
}
resource "aws_iam_instance_profile" "node" {
  for_each = local.nodes
  name     = "${var.name}-${each.key}"
  role     = aws_iam_role.node[each.key].name
}
resource "aws_instance" "node" {
  for_each                    = local.nodes
  ami                         = nonsensitive(data.aws_ssm_parameter.ami.value)
  instance_type               = "t4g.small"
  subnet_id                   = each.value.subnet
  private_ip                  = each.value.address
  associate_public_ip_address = true
  vpc_security_group_ids      = [aws_security_group.nats.id]
  iam_instance_profile        = aws_iam_instance_profile.node[each.key].name
  metadata_options {
    http_tokens                 = "required"
    http_put_response_hop_limit = 1
    instance_metadata_tags      = "disabled"
  }
  credit_specification { cpu_credits = "standard" }
  root_block_device {
    volume_size           = each.key == "nats1" ? 192 : 128
    volume_type           = "gp3"
    encrypted             = true
    delete_on_termination = false
    tags                  = { Name = "${var.name}-${each.key}-data", NATSBackup = var.name }
  }
  user_data = templatefile("${path.module}/bootstrap.sh.tftpl", {
    node       = each.key, version = var.server_version, sha256 = var.server_sha256,
    secret_arn = aws_secretsmanager_secret.node[each.key].arn
  })
  tags = { Name = "${var.name}-${each.key}", Role = "nats", PatchGroup = "${var.name}-nats" }
  lifecycle {
    prevent_destroy = true
    ignore_changes  = [ami, user_data]
  }
  depends_on = [aws_route_table_association.third, aws_route53_record.node, aws_iam_role_policy.node, aws_iam_role_policy_attachment.ssm]
}
resource "aws_cloudwatch_metric_alarm" "status" {
  for_each            = local.nodes
  alarm_name          = "${var.name}-${each.key}-instance-failure"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 2
  metric_name         = "StatusCheckFailed"
  namespace           = "AWS/EC2"
  period              = 60
  statistic           = "Maximum"
  threshold           = 0
  treat_missing_data  = "missing"
  dimensions          = { InstanceId = aws_instance.node[each.key].id }
  alarm_actions       = [aws_sns_topic.operations.arn]
  ok_actions          = [aws_sns_topic.operations.arn]
}
resource "aws_cloudwatch_metric_alarm" "health" {
  for_each            = local.nodes
  alarm_name          = "${var.name}-${each.key}-jetstream-unhealthy"
  comparison_operator = "LessThanThreshold"
  evaluation_periods  = 3
  metric_name         = "Healthy"
  namespace           = "Warmbly/NATS"
  period              = 60
  statistic           = "Minimum"
  threshold           = 1
  treat_missing_data  = "breaching"
  dimensions          = { Node = each.key }
  alarm_actions       = [aws_sns_topic.operations.arn]
  ok_actions          = [aws_sns_topic.operations.arn]
}
resource "aws_cloudwatch_metric_alarm" "disk" {
  for_each            = local.nodes
  alarm_name          = "${var.name}-${each.key}-disk-low"
  comparison_operator = "LessThanThreshold"
  evaluation_periods  = 3
  metric_name         = "DiskFreePercent"
  namespace           = "Warmbly/NATS"
  period              = 60
  statistic           = "Minimum"
  threshold           = 20
  treat_missing_data  = "breaching"
  dimensions          = { Node = each.key }
  alarm_actions       = [aws_sns_topic.operations.arn]
  ok_actions          = [aws_sns_topic.operations.arn]
}
output "nodes" {
  value = { for name, node in aws_instance.node : name => {
    instance_id = node.id, address = local.nodes[name].address,
    secret_arn  = aws_secretsmanager_secret.node[name].arn
  } }
}
output "client_secret_arn" { value = aws_secretsmanager_secret.client.arn }
output "urls" { value = join(",", [for name in sort(keys(local.nodes)) : "tls://${name}.nats.warmbly.internal:4222"]) }

data "aws_iam_policy_document" "snapshots_trust" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["dlm.amazonaws.com"]
    }
  }
}
resource "aws_iam_role" "snapshots" {
  name               = "${var.name}-nats-snapshots"
  assume_role_policy = data.aws_iam_policy_document.snapshots_trust.json
}
resource "aws_iam_role_policy_attachment" "snapshots" {
  role       = aws_iam_role.snapshots.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSDataLifecycleManagerServiceRole"
}
resource "aws_dlm_lifecycle_policy" "snapshots" {
  description        = "Daily encrypted NATS volume recovery points retained for seven days"
  execution_role_arn = aws_iam_role.snapshots.arn
  state              = "ENABLED"
  policy_details {
    resource_types = ["VOLUME"]
    target_tags    = { NATSBackup = var.name }
    schedule {
      name      = "daily"
      copy_tags = true
      create_rule {
        interval      = 24
        interval_unit = "HOURS"
        times         = ["03:00"]
      }
      retain_rule { count = 7 }
    }
  }
  depends_on = [aws_iam_role_policy_attachment.snapshots]
}
