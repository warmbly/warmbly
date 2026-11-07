variable "tracking_additional_hosts" {
  description = "Tracking hostnames with validated DNS and TLS at the edge"
  type        = set(string)
  default     = []
  validation {
    condition = alltrue([for host in var.tracking_additional_hosts :
      length(host) <= 128 && can(regex("^[a-zA-Z0-9][a-zA-Z0-9.-]*\\.[a-zA-Z]{2,}$", host))
    ])
    error_message = "Provide DNS hostnames supported by an ALB host-header rule."
  }
}

variable "tracking_serves_unmatched_hosts" {
  description = "Send any host no rule matches to tracking, so customer tracking hosts and redirects registered at the edge need no rule of their own"
  type        = bool
  default     = false
}

resource "aws_lb_listener_rule" "tracking_alias" {
  for_each = {
    for index, hosts in chunklist(sort(tolist(var.tracking_additional_hosts)), 5) : tostring(index) => hosts
  }
  listener_arn = aws_lb_listener.https.arn
  priority     = 200 + tonumber(each.key)
  condition {
    host_header { values = each.value }
  }
  action {
    type             = "forward"
    target_group_arn = contains(["both", "route"], var.target_tls) ? null : local.serving_target_groups["tracking"].arn
    dynamic "forward" {
      for_each = contains(["both", "route"], var.target_tls) ? ["tracking"] : []
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
  depends_on = [aws_vpc_security_group_ingress_rule.service, aws_vpc_security_group_ingress_rule.service_tls]
}
