# The autoscaling worker fleet. Opt-in, and independent of the fixed hosts in
# main.tf, which keep running until an operator retires them.

variable "fleet_enabled" {
  description = "Create the autoscaling worker fleet and its network; the backend also needs FLEET_METRICS_CLOUDWATCH set to name, or the memory metrics it scales on are never published"
  type        = bool
  default     = false
}

variable "fleet_instance_url" {
  description = "HTTPS address of the Warmbly instance the fleet joins, the --url given to join.sh"
  type        = string
  default     = null
  validation {
    condition     = var.fleet_instance_url == null || can(regex("^https://[a-z0-9.-]+(:[0-9]+)?/?$", var.fleet_instance_url))
    error_message = "Use the instance's https:// origin, with no path."
  }
}

variable "fleet_join_token_parameter" {
  description = "SecureString parameter holding the fleet join token. Null uses /<name>/fleet/join-token"
  type        = string
  default     = null
  validation {
    condition     = var.fleet_join_token_parameter == null || can(regex("^/[A-Za-z0-9_./-]+$", var.fleet_join_token_parameter))
    error_message = "Use a fully qualified parameter name starting with a slash."
  }
}

variable "fleet_join_token_kms_key_arn" {
  description = "Customer managed KMS key encrypting the join token parameter. Null means the AWS managed aws/ssm key"
  type        = string
  default     = null
}

variable "fleet_instance_type" {
  description = "Graviton instance type for fleet workers"
  type        = string
  default     = "t4g.medium"
  validation {
    condition     = can(regex("^[a-z]+[0-9]+g[a-z]*\\.[a-z0-9]+$", var.fleet_instance_type))
    error_message = "Use a Graviton (arm64) instance type."
  }
}

variable "fleet_cpu_credits" {
  description = "CPU credit mode for burstable fleet types"
  type        = string
  default     = "standard"
  validation {
    condition     = contains(["standard", "unlimited"], var.fleet_cpu_credits)
    error_message = "Use standard or unlimited."
  }
}

variable "fleet_root_volume_gb" {
  type    = number
  default = 20
}

variable "fleet_min_size" {
  type    = number
  default = 2
}

variable "fleet_max_size" {
  type    = number
  default = 10
}

variable "fleet_desired_capacity" {
  description = "Initial size; scaling owns it afterwards. Null starts at fleet_min_size"
  type        = number
  default     = null
}

variable "fleet_worker_capacity" {
  description = "WARMBLY_WORKER_CAPACITY on every fleet worker: auto sizes the mailbox target from the machine's memory, a whole number fixes it"
  type        = string
  default     = "auto"
  validation {
    condition     = var.fleet_worker_capacity == "auto" || can(regex("^[1-9][0-9]{0,7}$", var.fleet_worker_capacity))
    error_message = "Use auto or a whole number from 1 to 99999999, the largest the worker accepts."
  }
}

variable "fleet_region_label" {
  description = "Optional --region label the fleet's nodes report for placement"
  type        = string
  default     = ""
  validation {
    condition     = can(regex("^[a-z0-9-]*$", var.fleet_region_label))
    error_message = "Use lowercase letters, digits and dashes."
  }
}

variable "fleet_image_repository" {
  description = "Image repository join.sh pulls the worker image from"
  type        = string
  default     = "ghcr.io/warmbly/warmbly"
  validation {
    condition     = can(regex("^[a-z0-9.-]+(:[0-9]+)?(/[a-z0-9._-]+)+$", var.fleet_image_repository))
    error_message = "Use a registry repository path without a tag."
  }
}

variable "fleet_target_memory" {
  description = "Target for WorkerMemoryPercent, the fleet's memory in use; the fleet scales out above it"
  type        = number
  default     = 65
  validation {
    condition     = var.fleet_target_memory > 0 && var.fleet_target_memory <= 100
    error_message = "Use a percentage."
  }
}

variable "fleet_target_cpu" {
  description = "Target average CPU percent across the fleet; the second scale-out signal"
  type        = number
  default     = 60
  validation {
    condition     = var.fleet_target_cpu > 0 && var.fleet_target_cpu <= 100
    error_message = "Use a percentage."
  }
}

variable "fleet_scale_in_memory_below" {
  description = "Scale in only while WorkerMemoryPercent stays below this"
  type        = number
  default     = 35
  validation {
    condition     = var.fleet_scale_in_memory_below > 0 && var.fleet_scale_in_memory_below <= 100
    error_message = "Use a percentage."
  }
}

variable "fleet_scale_in_memory_max_below" {
  description = "Scale in only while the fullest worker's memory percent also stays below this"
  type        = number
  default     = 60
  validation {
    condition     = var.fleet_scale_in_memory_max_below > 0 && var.fleet_scale_in_memory_max_below <= 100
    error_message = "Use a percentage."
  }
}

variable "fleet_scale_in_cpu_below" {
  description = "Scale in only while the busiest worker's CPU percent also stays below this"
  type        = number
  default     = 30
  validation {
    condition     = var.fleet_scale_in_cpu_below > 0 && var.fleet_scale_in_cpu_below <= 100
    error_message = "Use a percentage."
  }
}

variable "fleet_scale_in_after_hours" {
  description = "How long all scale-in conditions must hold before one worker is removed, and the pause before the next"
  type        = number
  default     = 3
  validation {
    condition     = var.fleet_scale_in_after_hours >= 1 && var.fleet_scale_in_after_hours <= 24 && floor(var.fleet_scale_in_after_hours) == var.fleet_scale_in_after_hours
    error_message = "Use 1 to 24 whole hours; a CloudWatch alarm cannot evaluate more than a day."
  }
}

variable "fleet_termination_hook_seconds" {
  description = "How long a terminating worker is held after its node stops, so its mailboxes re-place first"
  type        = number
  default     = 900
  validation {
    condition     = var.fleet_termination_hook_seconds >= 30 && var.fleet_termination_hook_seconds <= 7200
    error_message = "Use 30 to 7200 seconds."
  }
}

variable "fleet_instance_warmup_seconds" {
  description = "Seconds before a new worker's metrics count toward scaling"
  type        = number
  default     = 900
}

variable "fleet_refresh_warmup_seconds" {
  description = "Seconds an instance refresh waits after each replacement before the next"
  type        = number
  default     = 1800
}

variable "nats_security_group_id" {
  description = "Security group of the NATS root's brokers; null skips the fleet's client rule"
  type        = string
  default     = null
}

locals {
  fleet                 = var.fleet_enabled
  fleet_join_token_name = coalesce(var.fleet_join_token_parameter, "/${var.name}/fleet/join-token")
  # Resolved at each launch, so new workers boot a current AMI without a template change refreshing the fleet.
  al2023_arm64 = "resolve:ssm:/aws/service/ami-amazon-linux-latest/al2023-ami-kernel-default-arm64"
  fleet_metric = {
    namespace = "Warmbly/Fleet"
    dimension = { Fleet = var.name }
    # Memory drives scaling; utilization is mailboxes over planned capacity and is published for reading only.
    names = { memory = "WorkerMemoryPercent", memory_max = "WorkerMemoryMaxPercent", utilization = "WorkerUtilizationPercent" }
  }
  fleet_scale_in_points = var.fleet_scale_in_after_hours * 12
}

data "aws_region" "current" {}
data "aws_partition" "current" {}
data "aws_caller_identity" "current" {}

data "aws_iam_policy_document" "ec2_trust" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["ec2.amazonaws.com"]
    }
  }
}

resource "aws_security_group" "fleet" {
  count       = local.fleet ? 1 : 0
  name_prefix = "${var.name}-fleet-"
  description = "Autoscaling mail workers; no inbound access"
  vpc_id      = data.aws_vpc.fleet[0].id
}

resource "aws_vpc_security_group_egress_rule" "fleet_ipv4" {
  count             = local.fleet ? 1 : 0
  security_group_id = aws_security_group.fleet[0].id
  ip_protocol       = "-1"
  cidr_ipv4         = "0.0.0.0/0"
}

resource "aws_vpc_security_group_egress_rule" "fleet_ipv6" {
  count             = local.fleet ? 1 : 0
  security_group_id = aws_security_group.fleet[0].id
  ip_protocol       = "-1"
  cidr_ipv6         = "::/0"
}

resource "aws_vpc_security_group_ingress_rule" "fleet_cache" {
  count                        = local.fleet ? 1 : 0
  security_group_id            = var.cache_security_group_id
  referenced_security_group_id = aws_security_group.fleet[0].id
  ip_protocol                  = "tcp"
  from_port                    = 6379
  to_port                      = 6379
}

resource "aws_vpc_security_group_ingress_rule" "fleet_nats" {
  count                        = local.fleet && var.nats_security_group_id != null ? 1 : 0
  security_group_id            = var.nats_security_group_id
  referenced_security_group_id = aws_security_group.fleet[0].id
  ip_protocol                  = "tcp"
  from_port                    = 4222
  to_port                      = 4222
}

# Reads the join token and nothing else: KMS and blobs are brokered through the backend.
data "aws_iam_policy_document" "fleet" {
  statement {
    actions   = ["ssm:GetParameter"]
    resources = ["arn:${data.aws_partition.current.partition}:ssm:${data.aws_region.current.region}:${data.aws_caller_identity.current.account_id}:parameter/${trimprefix(local.fleet_join_token_name, "/")}"]
  }
  dynamic "statement" {
    for_each = var.fleet_join_token_kms_key_arn == null ? [] : [var.fleet_join_token_kms_key_arn]
    content {
      actions   = ["kms:Decrypt"]
      resources = [statement.value]
      condition {
        test     = "StringEquals"
        variable = "kms:ViaService"
        values   = ["ssm.${data.aws_region.current.region}.amazonaws.com"]
      }
    }
  }
}

resource "aws_iam_role" "fleet" {
  count              = local.fleet ? 1 : 0
  name               = "${var.name}-fleet-worker"
  assume_role_policy = data.aws_iam_policy_document.ec2_trust.json
}

resource "aws_iam_role_policy" "fleet" {
  count  = local.fleet ? 1 : 0
  name   = "read-join-token"
  role   = aws_iam_role.fleet[0].id
  policy = data.aws_iam_policy_document.fleet.json
}

resource "aws_iam_instance_profile" "fleet" {
  count = local.fleet ? 1 : 0
  name  = "${var.name}-fleet-worker"
  role  = aws_iam_role.fleet[0].name
}

resource "aws_launch_template" "fleet" {
  count                                = local.fleet ? 1 : 0
  name_prefix                          = "${var.name}-fleet-"
  image_id                             = local.al2023_arm64
  instance_type                        = var.fleet_instance_type
  instance_initiated_shutdown_behavior = "terminate"
  update_default_version               = true
  iam_instance_profile { arn = aws_iam_instance_profile.fleet[0].arn }
  metadata_options {
    http_endpoint               = "enabled"
    http_tokens                 = "required"
    http_put_response_hop_limit = 1
    instance_metadata_tags      = "disabled"
  }
  dynamic "credit_specification" {
    for_each = startswith(var.fleet_instance_type, "t") ? [1] : []
    content { cpu_credits = var.fleet_cpu_credits }
  }
  monitoring { enabled = false }
  network_interfaces {
    device_index                = 0
    associate_public_ip_address = false
    ipv6_address_count          = 1
    delete_on_termination       = true
    security_groups             = [aws_security_group.fleet[0].id]
  }
  block_device_mappings {
    device_name = "/dev/xvda"
    ebs {
      volume_size           = var.fleet_root_volume_gb
      volume_type           = "gp3"
      encrypted             = true
      delete_on_termination = true
    }
  }
  user_data = base64encode(templatefile("${path.module}/fleet-bootstrap.sh.tftpl", {
    region          = data.aws_region.current.region
    instance_url    = trimsuffix(coalesce(var.fleet_instance_url, "https://unset.invalid"), "/")
    token_parameter = local.fleet_join_token_name
    name_prefix     = "${var.name}-fleet"
    region_label    = var.fleet_region_label
    image_repo      = var.fleet_image_repository
    capacity        = var.fleet_worker_capacity
  }))
  tag_specifications {
    resource_type = "instance"
    tags          = { Name = "${var.name}-fleet-worker", Role = "worker" }
  }
  tag_specifications {
    resource_type = "volume"
    tags          = { Name = "${var.name}-fleet-worker" }
  }
  lifecycle {
    precondition {
      condition     = var.fleet_instance_url != null
      error_message = "fleet_enabled needs fleet_instance_url."
    }
    precondition {
      condition     = var.fleet_min_size >= 1 && var.fleet_max_size >= var.fleet_min_size && (var.fleet_desired_capacity == null || (var.fleet_desired_capacity >= var.fleet_min_size && var.fleet_desired_capacity <= var.fleet_max_size))
      error_message = "Use 1 <= fleet_min_size <= fleet_desired_capacity <= fleet_max_size."
    }
    precondition {
      condition     = var.fleet_scale_in_memory_below < var.fleet_target_memory && var.fleet_scale_in_cpu_below < var.fleet_target_cpu
      error_message = "Each scale-in threshold must sit under its scale-out counterpart."
    }
  }
}

resource "aws_autoscaling_group" "fleet" {
  count                     = local.fleet ? 1 : 0
  name                      = "${var.name}-fleet"
  min_size                  = var.fleet_min_size
  max_size                  = var.fleet_max_size
  desired_capacity          = var.fleet_desired_capacity
  vpc_zone_identifier       = aws_subnet.fleet[*].id
  health_check_type         = "EC2"
  health_check_grace_period = 900
  default_instance_warmup   = var.fleet_instance_warmup_seconds
  # Scale in the newest worker, which has the fewest mailboxes to move.
  termination_policies = ["NewestInstance"]
  # Zone rebalancing would move mailboxes for symmetry alone.
  suspended_processes = ["AZRebalance"]
  enabled_metrics     = ["GroupDesiredCapacity", "GroupInServiceInstances", "GroupPendingInstances", "GroupTerminatingInstances"]
  launch_template {
    id      = aws_launch_template.fleet[0].id
    version = aws_launch_template.fleet[0].latest_version
  }
  # The node stops itself on seeing this state, then the hook's timeout lets termination proceed.
  initial_lifecycle_hook {
    name                 = "drain"
    lifecycle_transition = "autoscaling:EC2_INSTANCE_TERMINATING"
    heartbeat_timeout    = var.fleet_termination_hook_seconds
    default_result       = "CONTINUE"
  }
  # Launch before terminate, one worker at a time while the fleet is under 100 instances.
  instance_refresh {
    strategy = "Rolling"
    preferences {
      min_healthy_percentage = 100
      max_healthy_percentage = 101
      instance_warmup        = var.fleet_refresh_warmup_seconds
      skip_matching          = true
    }
  }
  tag {
    key                 = "Name"
    value               = "${var.name}-fleet-worker"
    propagate_at_launch = false
  }
  depends_on = [aws_autoscaling_group.nat, aws_route_table_association.fleet, aws_vpc_security_group_egress_rule.fleet_ipv4]
  lifecycle {
    ignore_changes = [desired_capacity]
  }
}

resource "aws_autoscaling_policy" "fleet_memory" {
  count                  = local.fleet ? 1 : 0
  name                   = "memory"
  autoscaling_group_name = aws_autoscaling_group.fleet[0].name
  policy_type            = "TargetTrackingScaling"
  target_tracking_configuration {
    target_value     = var.fleet_target_memory
    disable_scale_in = true
    customized_metric_specification {
      namespace   = local.fleet_metric.namespace
      metric_name = local.fleet_metric.names.memory
      statistic   = "Average"
      metric_dimension {
        name  = "Fleet"
        value = local.fleet_metric.dimension.Fleet
      }
    }
  }
}

resource "aws_autoscaling_policy" "fleet_cpu" {
  count                  = local.fleet ? 1 : 0
  name                   = "cpu"
  autoscaling_group_name = aws_autoscaling_group.fleet[0].name
  policy_type            = "TargetTrackingScaling"
  target_tracking_configuration {
    target_value     = var.fleet_target_cpu
    disable_scale_in = true
    predefined_metric_specification {
      predefined_metric_type = "ASGAverageCPUUtilization"
    }
  }
}

# The only way the fleet shrinks: one worker, then a pause as long as the quiet period.
resource "aws_autoscaling_policy" "fleet_scale_in" {
  count                  = local.fleet ? 1 : 0
  name                   = "gentle-scale-in"
  autoscaling_group_name = aws_autoscaling_group.fleet[0].name
  policy_type            = "SimpleScaling"
  adjustment_type        = "ChangeInCapacity"
  scaling_adjustment     = -1
  cooldown               = var.fleet_scale_in_after_hours * 3600
}

# Missing data never counts as quiet, so the fleet cannot shrink while the publisher is down.
resource "aws_cloudwatch_metric_alarm" "fleet_scale_in" {
  count               = local.fleet ? 1 : 0
  alarm_name          = "${var.name}-fleet-scale-in"
  alarm_description   = "Fleet memory, the fullest worker's memory and the busiest worker's CPU stayed low for ${var.fleet_scale_in_after_hours}h; removes one worker"
  comparison_operator = "GreaterThanOrEqualToThreshold"
  threshold           = 1
  evaluation_periods  = local.fleet_scale_in_points
  datapoints_to_alarm = local.fleet_scale_in_points
  treat_missing_data  = "notBreaching"
  alarm_actions       = [aws_autoscaling_policy.fleet_scale_in[0].arn]
  metric_query {
    id          = "quiet"
    expression  = "IF(mem < ${var.fleet_scale_in_memory_below} AND memmax < ${var.fleet_scale_in_memory_max_below} AND cpu < ${var.fleet_scale_in_cpu_below}, 1, 0)"
    label       = "Quiet"
    return_data = true
  }
  metric_query {
    id = "mem"
    metric {
      namespace   = local.fleet_metric.namespace
      metric_name = local.fleet_metric.names.memory
      dimensions  = local.fleet_metric.dimension
      period      = 300
      stat        = "Maximum"
    }
  }
  metric_query {
    id = "memmax"
    metric {
      namespace   = local.fleet_metric.namespace
      metric_name = local.fleet_metric.names.memory_max
      dimensions  = local.fleet_metric.dimension
      period      = 300
      stat        = "Maximum"
    }
  }
  metric_query {
    id = "cpu"
    metric {
      namespace   = "AWS/EC2"
      metric_name = "CPUUtilization"
      dimensions  = { AutoScalingGroupName = aws_autoscaling_group.fleet[0].name }
      period      = 300
      stat        = "Maximum"
    }
  }
}

output "fleet" {
  value = local.fleet ? {
    autoscaling_group    = aws_autoscaling_group.fleet[0].name
    security_group_id    = aws_security_group.fleet[0].id
    subnet_ids           = aws_subnet.fleet[*].id
    join_token_parameter = local.fleet_join_token_name
    nat_ipv4_addresses   = aws_eip.nat[*].public_ip
    metric               = local.fleet_metric
  } : null
}
