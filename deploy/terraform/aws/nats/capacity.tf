resource "aws_cloudwatch_metric_alarm" "stream_capacity" {
  for_each            = local.nodes
  alarm_name          = "${var.name}-${each.key}-stream-capacity"
  namespace           = "Warmbly/NATS"
  metric_name         = "StreamUsagePercent"
  dimensions          = { Node = each.key }
  comparison_operator = "GreaterThanThreshold"
  threshold           = 75
  evaluation_periods  = 3
  period              = 60
  statistic           = "Maximum"
  treat_missing_data  = "breaching"
  alarm_description   = "Warmbly stream is above 75 percent of its logical limit. Review retention, backlog and storage headroom before raising the limit."
  alarm_actions       = [aws_sns_topic.operations.arn]
  ok_actions          = [aws_sns_topic.operations.arn]
}
