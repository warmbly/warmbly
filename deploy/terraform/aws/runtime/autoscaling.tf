# Task scaling. Desired count stays out of Terraform's hands (aws_ecs_service ignores it), so these
# bounds and policies are what move it. A service left out keeps the count it was given by hand.
variable "autoscaling" {
  description = "Task count bounds by service. The consumer scales on its worker-event backlog, every other service on average CPU at cpu percent"
  type = map(object({
    min = number
    max = number
    cpu = optional(number, 60)
  }))
  default = {
    consumer = { min = 1, max = 6 }
  }
  validation {
    condition = alltrue([for name, scaling in var.autoscaling :
      contains(["backend", "consumer", "forms", "tracking"], name) &&
      scaling.min >= 1 && scaling.min <= scaling.max && scaling.max <= 20 &&
      floor(scaling.min) == scaling.min && floor(scaling.max) == scaling.max &&
      scaling.cpu >= 20 && scaling.cpu <= 90
    ])
    error_message = "Scale backend, consumer, forms or tracking between integer bounds 1 <= min <= max <= 20 at a cpu target of 20-90%. Realtime tracks presence per task, so it keeps a fixed count."
  }
}

variable "consumer_backlog_scale_out" {
  description = "Worker events waiting, held for two minutes, that add a consumer task; ten times this adds two"
  type        = number
  default     = 1000
  validation {
    condition     = var.consumer_backlog_scale_out >= 1
    error_message = "consumer_backlog_scale_out is at least one event."
  }
}

variable "consumer_quiet_minutes" {
  description = "Minutes with no worker event waiting before one consumer task is removed"
  type        = number
  default     = 30
  validation {
    condition     = var.consumer_quiet_minutes >= 10 && var.consumer_quiet_minutes % 5 == 0
    error_message = "consumer_quiet_minutes is a multiple of 5, at least 10."
  }
}

locals {
  autoscaling     = { for name, scaling in var.autoscaling : name => scaling if contains(keys(var.services), name) }
  cpu_scaling     = { for name, scaling in local.autoscaling : name => scaling if name != "consumer" }
  backlog_scaling = contains(keys(local.autoscaling), "consumer")
  # The contract with internal/app/consumermetrics.
  consumer_metric = {
    namespace  = "Warmbly/Consumer"
    name       = "WorkerEventBacklog"
    dimensions = { Consumer = var.name }
  }
}

resource "aws_appautoscaling_target" "service" {
  for_each           = local.autoscaling
  service_namespace  = "ecs"
  scalable_dimension = "ecs:service:DesiredCount"
  resource_id        = "service/${split("/", local.foundation.cluster_arn)[1]}/${aws_ecs_service.service[each.key].name}"
  min_capacity       = each.value.min
  max_capacity       = each.value.max
}

resource "aws_appautoscaling_policy" "cpu" {
  for_each           = local.cpu_scaling
  name               = "cpu"
  policy_type        = "TargetTrackingScaling"
  service_namespace  = aws_appautoscaling_target.service[each.key].service_namespace
  scalable_dimension = aws_appautoscaling_target.service[each.key].scalable_dimension
  resource_id        = aws_appautoscaling_target.service[each.key].resource_id
  target_tracking_scaling_policy_configuration {
    target_value       = each.value.cpu
    scale_out_cooldown = 60
    scale_in_cooldown  = 300
    predefined_metric_specification {
      predefined_metric_type = "ECSServiceAverageCPUUtilization"
    }
  }
}

# Every consumer task shares one durable, so the backlog is the fleet's, and a waiting event means every lane is full.
resource "aws_appautoscaling_policy" "consumer_scale_out" {
  count              = local.backlog_scaling ? 1 : 0
  name               = "backlog-scale-out"
  policy_type        = "StepScaling"
  service_namespace  = aws_appautoscaling_target.service["consumer"].service_namespace
  scalable_dimension = aws_appautoscaling_target.service["consumer"].scalable_dimension
  resource_id        = aws_appautoscaling_target.service["consumer"].resource_id
  step_scaling_policy_configuration {
    adjustment_type         = "ChangeInCapacity"
    cooldown                = 180
    metric_aggregation_type = "Maximum"
    step_adjustment {
      metric_interval_lower_bound = 0
      metric_interval_upper_bound = var.consumer_backlog_scale_out * 9
      scaling_adjustment          = 1
    }
    step_adjustment {
      metric_interval_lower_bound = var.consumer_backlog_scale_out * 9
      scaling_adjustment          = 2
    }
  }
}

resource "aws_appautoscaling_policy" "consumer_scale_in" {
  count              = local.backlog_scaling ? 1 : 0
  name               = "quiet-scale-in"
  policy_type        = "StepScaling"
  service_namespace  = aws_appautoscaling_target.service["consumer"].service_namespace
  scalable_dimension = aws_appautoscaling_target.service["consumer"].scalable_dimension
  resource_id        = aws_appautoscaling_target.service["consumer"].resource_id
  step_scaling_policy_configuration {
    adjustment_type         = "ChangeInCapacity"
    cooldown                = var.consumer_quiet_minutes * 60
    metric_aggregation_type = "Maximum"
    step_adjustment {
      metric_interval_upper_bound = 0
      scaling_adjustment          = -1
    }
  }
}

# Missing data never counts: no publisher means no scaling either way.
resource "aws_cloudwatch_metric_alarm" "consumer_backlog" {
  count               = local.backlog_scaling ? 1 : 0
  alarm_name          = "${var.name}-consumer-backlog"
  alarm_description   = "Worker events waited for a free consumer lane for two minutes; adds consumer tasks"
  namespace           = local.consumer_metric.namespace
  metric_name         = local.consumer_metric.name
  dimensions          = local.consumer_metric.dimensions
  statistic           = "Maximum"
  period              = 60
  evaluation_periods  = 2
  datapoints_to_alarm = 2
  comparison_operator = "GreaterThanOrEqualToThreshold"
  threshold           = var.consumer_backlog_scale_out
  treat_missing_data  = "notBreaching"
  alarm_actions       = [aws_appautoscaling_policy.consumer_scale_out[0].arn]
}

resource "aws_cloudwatch_metric_alarm" "consumer_quiet" {
  count               = local.backlog_scaling ? 1 : 0
  alarm_name          = "${var.name}-consumer-quiet"
  alarm_description   = "No worker event waited for ${var.consumer_quiet_minutes} minutes; removes one consumer task"
  namespace           = local.consumer_metric.namespace
  metric_name         = local.consumer_metric.name
  dimensions          = local.consumer_metric.dimensions
  statistic           = "Maximum"
  period              = 300
  evaluation_periods  = var.consumer_quiet_minutes / 5
  datapoints_to_alarm = var.consumer_quiet_minutes / 5
  comparison_operator = "LessThanThreshold"
  threshold           = 1
  treat_missing_data  = "notBreaching"
  alarm_actions       = [aws_appautoscaling_policy.consumer_scale_in[0].arn]
}

data "aws_iam_policy_document" "consumer_metrics" {
  count = local.backlog_scaling ? 1 : 0
  statement {
    actions   = ["cloudwatch:PutMetricData"]
    resources = ["*"]
    condition {
      test     = "StringEquals"
      variable = "cloudwatch:namespace"
      values   = [local.consumer_metric.namespace]
    }
  }
}
resource "aws_iam_role_policy" "consumer_metrics" {
  count  = local.backlog_scaling ? 1 : 0
  name   = "publish-backlog"
  role   = aws_iam_role.application["consumer"].id
  policy = data.aws_iam_policy_document.consumer_metrics[0].json
}
