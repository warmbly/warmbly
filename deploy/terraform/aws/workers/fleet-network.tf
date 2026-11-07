# Dual-stack worker subnets: IPv6 leaves through an egress-only gateway, IPv4
# through one NAT instance per zone, so no worker holds a public IPv4 address.

variable "fleet_subnet_netnums" {
  description = "Subnet numbers for the fleet's worker subnets, one per foundation zone: /24 of the VPC IPv4 block and /64 of its IPv6 block"
  type        = list(number)
  default     = [20, 21]
  validation {
    condition     = length(var.fleet_subnet_netnums) == 2 && length(distinct(var.fleet_subnet_netnums)) == 2 && alltrue([for n in var.fleet_subnet_netnums : n >= 3 && n <= 255 && n != 10 && n != 11 && floor(n) == n])
    error_message = "Use two distinct subnet numbers between 3 and 255 that the foundation (0, 1, 10, 11) and NATS (2) roots do not use."
  }
}

variable "nat_instance_type" {
  description = "Graviton type for each zone's NAT instance"
  type        = string
  default     = "t4g.micro"
  validation {
    condition     = can(regex("^[a-z]+[0-9]+g[a-z]*\\.[a-z0-9]+$", var.nat_instance_type))
    error_message = "Use a Graviton (arm64) instance type."
  }
}

variable "nat_conntrack_max" {
  description = "Connection-tracking entries on each NAT instance; one per open worker connection to an IPv4 server"
  type        = number
  default     = 524288
  validation {
    condition     = var.nat_conntrack_max >= 65536 && var.nat_conntrack_max <= 4194304 && floor(var.nat_conntrack_max) == var.nat_conntrack_max
    error_message = "Use between 65536 and 4194304 entries."
  }
}

locals {
  fleet_zones = local.fleet ? 2 : 0
  # Kernel default is 5 days; 2 days outlives any idle IMAP session yet still reclaims vanished peers.
  nat_tcp_established_timeout = 172800
  fleet_ipv6_block            = local.fleet ? sort([for a in data.aws_vpc.fleet[0].ipv6_cidr_block_associations : a.ipv6_cidr_block if a.state == "associated"])[0] : null
}

data "aws_vpc" "fleet" {
  count = local.fleet ? 1 : 0
  id    = data.terraform_remote_state.foundation.outputs.runtime.vpc_id
  lifecycle {
    postcondition {
      condition     = anytrue([for a in self.ipv6_cidr_block_associations : a.state == "associated"])
      error_message = "The foundation VPC has no IPv6 block yet. Apply the foundation root first."
    }
  }
}

data "aws_subnet" "nat" {
  count = local.fleet_zones
  id    = data.terraform_remote_state.foundation.outputs.runtime.public_subnet_ids[count.index]
}

resource "aws_subnet" "fleet" {
  count                           = local.fleet_zones
  vpc_id                          = data.aws_vpc.fleet[0].id
  availability_zone               = data.aws_subnet.nat[count.index].availability_zone
  cidr_block                      = cidrsubnet(data.aws_vpc.fleet[0].cidr_block, 8, var.fleet_subnet_netnums[count.index])
  ipv6_cidr_block                 = cidrsubnet(local.fleet_ipv6_block, 8, var.fleet_subnet_netnums[count.index])
  assign_ipv6_address_on_creation = true
  map_public_ip_on_launch         = false
  tags                            = { Name = "${var.name}-fleet-${count.index + 1}" }
}

resource "aws_egress_only_internet_gateway" "fleet" {
  count  = local.fleet ? 1 : 0
  vpc_id = data.aws_vpc.fleet[0].id
  tags   = { Name = "${var.name}-fleet" }
}

# The IPv4 default route is written by the zone's NAT instance when it boots, so a replacement takes it over.
resource "aws_route_table" "fleet" {
  count  = local.fleet_zones
  vpc_id = data.aws_vpc.fleet[0].id
  tags   = { Name = "${var.name}-fleet-${count.index + 1}" }
}

resource "aws_route" "fleet_ipv6" {
  count                       = local.fleet_zones
  route_table_id              = aws_route_table.fleet[count.index].id
  destination_ipv6_cidr_block = "::/0"
  egress_only_gateway_id      = aws_egress_only_internet_gateway.fleet[0].id
}

resource "aws_route_table_association" "fleet" {
  count          = local.fleet_zones
  subnet_id      = aws_subnet.fleet[count.index].id
  route_table_id = aws_route_table.fleet[count.index].id
}

# A second gateway endpoint, because the foundation's lists its route tables authoritatively.
resource "aws_vpc_endpoint" "fleet_s3" {
  count             = local.fleet ? 1 : 0
  vpc_id            = data.aws_vpc.fleet[0].id
  service_name      = "com.amazonaws.${data.aws_region.current.region}.s3"
  vpc_endpoint_type = "Gateway"
  route_table_ids   = aws_route_table.fleet[*].id
  tags              = { Name = "${var.name}-fleet-s3" }
}

resource "aws_security_group" "nat" {
  count       = local.fleet ? 1 : 0
  name_prefix = "${var.name}-fleet-nat-"
  description = "IPv4 NAT for the worker fleet; accepts traffic only from fleet subnets"
  vpc_id      = data.aws_vpc.fleet[0].id
}

resource "aws_vpc_security_group_ingress_rule" "nat" {
  count             = local.fleet_zones
  security_group_id = aws_security_group.nat[0].id
  cidr_ipv4         = aws_subnet.fleet[count.index].cidr_block
  ip_protocol       = "-1"
}

resource "aws_vpc_security_group_egress_rule" "nat" {
  count             = local.fleet ? 1 : 0
  security_group_id = aws_security_group.nat[0].id
  cidr_ipv4         = "0.0.0.0/0"
  ip_protocol       = "-1"
}

# The fleet's IPv4 egress addresses. Customers may allowlist them, so they outlive any NAT instance.
resource "aws_eip" "nat" {
  count  = local.fleet_zones
  domain = "vpc"
  tags   = { Name = "${var.name}-fleet-nat-${count.index + 1}" }
  lifecycle { prevent_destroy = true }
}

data "aws_iam_policy_document" "nat" {
  count = local.fleet_zones
  statement {
    actions   = ["ec2:AssociateAddress"]
    resources = [aws_eip.nat[count.index].arn]
  }
  statement {
    actions = ["ec2:AssociateAddress"]
    resources = [
      "arn:${data.aws_partition.current.partition}:ec2:${data.aws_region.current.region}:${data.aws_caller_identity.current.account_id}:instance/*",
      "arn:${data.aws_partition.current.partition}:ec2:${data.aws_region.current.region}:${data.aws_caller_identity.current.account_id}:network-interface/*",
    ]
  }
  statement {
    actions   = ["ec2:ModifyInstanceAttribute"]
    resources = ["arn:${data.aws_partition.current.partition}:ec2:${data.aws_region.current.region}:${data.aws_caller_identity.current.account_id}:instance/*"]
    condition {
      test     = "StringEquals"
      variable = "aws:ResourceTag/WarmblyRole"
      values   = ["${var.name}-fleet-nat"]
    }
  }
  statement {
    actions = ["ec2:CreateRoute", "ec2:ReplaceRoute"]
    resources = [
      aws_route_table.fleet[count.index].arn,
      "arn:${data.aws_partition.current.partition}:ec2:${data.aws_region.current.region}:${data.aws_caller_identity.current.account_id}:network-interface/*",
    ]
  }
}

resource "aws_iam_role" "nat" {
  count              = local.fleet_zones
  name               = "${var.name}-fleet-nat-${count.index + 1}"
  assume_role_policy = data.aws_iam_policy_document.ec2_trust.json
}

resource "aws_iam_role_policy" "nat" {
  count  = local.fleet_zones
  name   = "claim-address-and-route"
  role   = aws_iam_role.nat[count.index].id
  policy = data.aws_iam_policy_document.nat[count.index].json
}

resource "aws_iam_instance_profile" "nat" {
  count = local.fleet_zones
  name  = "${var.name}-fleet-nat-${count.index + 1}"
  role  = aws_iam_role.nat[count.index].name
}

resource "aws_launch_template" "nat" {
  count                                = local.fleet_zones
  name_prefix                          = "${var.name}-fleet-nat-${count.index + 1}-"
  image_id                             = local.al2023_arm64
  instance_type                        = var.nat_instance_type
  instance_initiated_shutdown_behavior = "terminate"
  update_default_version               = true
  iam_instance_profile { arn = aws_iam_instance_profile.nat[count.index].arn }
  metadata_options {
    http_endpoint               = "enabled"
    http_tokens                 = "required"
    http_put_response_hop_limit = 1
    instance_metadata_tags      = "disabled"
  }
  # Unlimited: a NAT throttled to its CPU baseline would stall every IPv4 connection in its zone.
  dynamic "credit_specification" {
    for_each = startswith(var.nat_instance_type, "t") ? [1] : []
    content { cpu_credits = "unlimited" }
  }
  # A public address is needed only to reach the EC2 API at boot; the Elastic IP then replaces it.
  network_interfaces {
    device_index                = 0
    associate_public_ip_address = true
    delete_on_termination       = true
    security_groups             = [aws_security_group.nat[0].id]
  }
  block_device_mappings {
    device_name = "/dev/xvda"
    ebs {
      volume_size           = 8
      volume_type           = "gp3"
      encrypted             = true
      delete_on_termination = true
    }
  }
  user_data = base64encode(templatefile("${path.module}/nat-bootstrap.sh.tftpl", {
    region          = data.aws_region.current.region
    allocation_id   = aws_eip.nat[count.index].allocation_id
    route_table_id  = aws_route_table.fleet[count.index].id
    source_cidrs    = join(" ", aws_subnet.fleet[*].cidr_block)
    conntrack_max   = var.nat_conntrack_max
    tcp_established = local.nat_tcp_established_timeout
  }))
  tag_specifications {
    resource_type = "instance"
    tags          = { Name = "${var.name}-fleet-nat-${count.index + 1}", WarmblyRole = "${var.name}-fleet-nat" }
  }
  tag_specifications {
    resource_type = "volume"
    tags          = { Name = "${var.name}-fleet-nat-${count.index + 1}" }
  }
}

# One instance per zone, replaced on failure; the replacement claims the same address and route.
resource "aws_autoscaling_group" "nat" {
  count                     = local.fleet_zones
  name                      = "${var.name}-fleet-nat-${count.index + 1}"
  min_size                  = 1
  max_size                  = 1
  desired_capacity          = 1
  vpc_zone_identifier       = [data.aws_subnet.nat[count.index].id]
  health_check_type         = "EC2"
  health_check_grace_period = 120
  launch_template {
    id      = aws_launch_template.nat[count.index].id
    version = aws_launch_template.nat[count.index].latest_version
  }
  tag {
    key                 = "Role"
    value               = "fleet-nat"
    propagate_at_launch = false
  }
}
