terraform {
  required_version = ">= 1.11, < 2.0"
  backend "s3" {
    key          = "warmbly/production/workers.tfstate"
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
variable "operator_public_key" { type = string }
variable "operator_ssh_cidrs" {
  type = set(string)
  validation {
    condition     = length(var.operator_ssh_cidrs) > 0 && alltrue([for cidr in var.operator_ssh_cidrs : can(cidrnetmask(cidr)) && cidr != "0.0.0.0/0"])
    error_message = "SSH must be restricted to explicit operator IPv4 CIDRs."
  }
}
variable "cache_security_group_id" { type = string }
variable "workers" {
  type = map(object({
    image      = string
    zone_index = number
  }))
  validation {
    condition     = alltrue([for name, worker in var.workers : can(regex("^[a-z0-9-]+$", name)) && can(regex("@sha256:[a-f0-9]{64}$", worker.image)) && contains([0, 1], worker.zone_index)])
    error_message = "Use safe host names, immutable ARM64 image digests and one of the foundation's two subnet indexes."
  }
}
data "terraform_remote_state" "foundation" {
  backend = "s3"
  config  = { bucket = var.state_bucket, key = "warmbly/production/foundation.tfstate", region = "us-east-1" }
}
data "aws_ssm_parameter" "ami" {
  name = "/aws/service/ami-amazon-linux-latest/al2023-ami-kernel-default-arm64"
}
resource "aws_secretsmanager_secret" "worker" {
  for_each                = var.workers
  name                    = "${var.name}/worker/${each.key}"
  description             = "Worker configuration delivered by an operator; values stay outside Terraform"
  recovery_window_in_days = 30
}
resource "aws_key_pair" "operator" {
  key_name_prefix = "${var.name}-workers-"
  public_key      = var.operator_public_key
}
resource "aws_security_group" "worker" {
  name_prefix = "${var.name}-workers-"
  description = "Outbound mail workers; SSH only from operators"
  vpc_id      = data.terraform_remote_state.foundation.outputs.runtime.vpc_id
}
resource "aws_vpc_security_group_ingress_rule" "ssh" {
  for_each          = var.operator_ssh_cidrs
  security_group_id = aws_security_group.worker.id
  cidr_ipv4         = each.value
  ip_protocol       = "tcp"
  from_port         = 22
  to_port           = 22
}
resource "aws_vpc_security_group_egress_rule" "worker" {
  security_group_id = aws_security_group.worker.id
  ip_protocol       = "-1"
  cidr_ipv4         = "0.0.0.0/0"
}
resource "aws_vpc_security_group_ingress_rule" "cache" {
  security_group_id            = var.cache_security_group_id
  referenced_security_group_id = aws_security_group.worker.id
  ip_protocol                  = "tcp"
  from_port                    = 6379
  to_port                      = 6379
}
resource "aws_instance" "worker" {
  for_each               = var.workers
  ami                    = nonsensitive(data.aws_ssm_parameter.ami.value)
  instance_type          = "t4g.small"
  subnet_id              = data.terraform_remote_state.foundation.outputs.runtime.public_subnet_ids[each.value.zone_index]
  vpc_security_group_ids = [aws_security_group.worker.id]
  key_name               = aws_key_pair.operator.key_name
  monitoring             = false
  metadata_options {
    http_endpoint               = "enabled"
    http_tokens                 = "required"
    http_put_response_hop_limit = 1
    instance_metadata_tags      = "disabled"
  }
  # A fixed worker cannot scale out, so running above baseline bills instead of throttling sync and sends.
  credit_specification { cpu_credits = "unlimited" }
  root_block_device {
    volume_size           = 16
    volume_type           = "gp3"
    encrypted             = true
    delete_on_termination = true
  }
  user_data = templatefile("${path.module}/bootstrap.sh.tftpl", { name = each.key, image = each.value.image })
  tags      = { Name = each.key, Role = "worker" }
  lifecycle {
    prevent_destroy = true
    ignore_changes  = [ami, user_data]
  }
}
resource "aws_eip" "worker" {
  for_each = var.workers
  domain   = "vpc"
  tags     = { Name = each.key }
  lifecycle { prevent_destroy = true }
}
resource "aws_eip_association" "worker" {
  for_each      = var.workers
  allocation_id = aws_eip.worker[each.key].id
  instance_id   = aws_instance.worker[each.key].id
}
output "workers" {
  value = { for name, instance in aws_instance.worker : name => {
    instance_id = instance.id
    public_ip   = aws_eip.worker[name].public_ip
    private_ip  = instance.private_ip
    ssh_user    = "ec2-user"
    secret_arn  = aws_secretsmanager_secret.worker[name].arn
  } }
}
