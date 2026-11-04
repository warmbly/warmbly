mock_provider "aws" {
  mock_data "aws_iam_policy_document" {
    defaults = { json = "{}" }
  }
  mock_data "aws_caller_identity" {
    defaults = { account_id = "123456789012" }
  }
  mock_data "aws_partition" {
    defaults = { partition = "aws" }
  }
  mock_data "aws_region" {
    defaults = { region = "us-east-1" }
  }
  mock_data "aws_vpc" {
    defaults = { id = "vpc-1", cidr_block = "10.0.0.0/16", ipv6_cidr_block_associations = [{ association_id = "vpc-cidr-assoc-1", ip_source = "amazon", ipv6_address_attribute = "public", ipv6_cidr_block = "2600:1f18:1234:5600::/56", ipv6_pool = "Amazon", network_border_group = "us-east-1", state = "associated" }] }
  }
  mock_data "aws_subnet" {
    defaults = { availability_zone = "us-east-1a", cidr_block = "10.0.0.0/24" }
  }
  mock_resource "aws_iam_role" {
    defaults = { arn = "arn:aws:iam::123456789012:role/mock" }
  }
  mock_resource "aws_iam_instance_profile" {
    defaults = { arn = "arn:aws:iam::123456789012:instance-profile/mock" }
  }
  mock_resource "aws_autoscaling_policy" {
    defaults = { arn = "arn:aws:autoscaling:us-east-1:123456789012:scalingPolicy:1:autoScalingGroupName/mock:policyName/mock" }
  }
  mock_resource "aws_eip" {
    defaults = { arn = "arn:aws:ec2:us-east-1:123456789012:elastic-ip/eipalloc-1", allocation_id = "eipalloc-1", public_ip = "198.51.100.10" }
  }
  mock_resource "aws_launch_template" {
    defaults = { id = "lt-0123456789abcdef0", latest_version = 1 }
  }
  mock_resource "aws_route_table" {
    defaults = { arn = "arn:aws:ec2:us-east-1:123456789012:route-table/rtb-1" }
  }
}

override_data {
  target = data.terraform_remote_state.foundation
  values = {
    outputs = {
      runtime = {
        vpc_id            = "vpc-1"
        public_subnet_ids = ["subnet-a", "subnet-b"]
      }
    }
  }
}

variables {
  state_bucket            = "state"
  operator_public_key     = "ssh-ed25519 AAAA test"
  operator_ssh_cidrs      = ["203.0.113.0/24"]
  cache_security_group_id = "sg-cache"
  workers                 = {}
}

run "fleet_is_opt_in" {
  command = plan
  assert {
    condition     = length(aws_autoscaling_group.fleet) == 0 && length(aws_autoscaling_group.nat) == 0 && length(aws_subnet.fleet) == 0 && output.fleet == null
    error_message = "Nothing of the fleet is created until fleet_enabled is set."
  }
}

run "fleet_without_public_ipv4" {
  command = apply
  variables {
    fleet_enabled          = true
    fleet_instance_url     = "https://app.example.com/"
    nats_security_group_id = "sg-nats"
  }
  assert {
    condition     = aws_launch_template.fleet[0].network_interfaces[0].associate_public_ip_address == "false" && aws_launch_template.fleet[0].network_interfaces[0].ipv6_address_count == 1
    error_message = "Fleet workers take an IPv6 address and no public IPv4 address."
  }
  assert {
    condition     = aws_launch_template.fleet[0].metadata_options[0].http_tokens == "required" && aws_launch_template.fleet[0].instance_type == "t4g.medium"
    error_message = "IMDSv2 is required and the default type is t4g.medium."
  }
  assert {
    condition     = aws_subnet.fleet[0].ipv6_cidr_block == "2600:1f18:1234:5614::/64" && aws_subnet.fleet[1].cidr_block == "10.0.21.0/24" && !aws_subnet.fleet[0].map_public_ip_on_launch
    error_message = "Worker subnets take /64 and /24 blocks at the configured subnet numbers."
  }
  assert {
    condition     = aws_route.fleet_ipv6[0].destination_ipv6_cidr_block == "::/0" && length(aws_vpc_endpoint.fleet_s3) == 1
    error_message = "IPv6 leaves through the egress-only gateway and S3 through a gateway endpoint."
  }
  assert {
    condition     = strcontains(base64decode(aws_launch_template.fleet[0].user_data), "WARMBLY_WORKER_CAPACITY=$CAPACITY") && strcontains(base64decode(aws_launch_template.fleet[0].user_data), "CAPACITY='auto'") && strcontains(base64decode(aws_launch_template.fleet[0].user_data), "GOMEMLIMIT=%sMiB") && strcontains(base64decode(aws_launch_template.fleet[0].user_data), "INSTANCE_URL='https://app.example.com'") && strcontains(base64decode(aws_launch_template.fleet[0].user_data), "TOKEN_PARAMETER='/warmbly-production/fleet/join-token'")
    error_message = "User data sets the worker capacity to auto, a Go memory limit, and names the join token parameter."
  }
  assert {
    condition     = aws_autoscaling_group.fleet[0].min_size == 2 && aws_autoscaling_group.fleet[0].health_check_type == "EC2" && contains(aws_autoscaling_group.fleet[0].suspended_processes, "AZRebalance")
    error_message = "The fleet keeps two workers by default and never rebalances zones."
  }
  assert {
    condition     = one(aws_autoscaling_group.fleet[0].initial_lifecycle_hook).heartbeat_timeout == 900 && aws_autoscaling_group.fleet[0].instance_refresh[0].preferences[0].min_healthy_percentage == 100 && aws_autoscaling_group.fleet[0].instance_refresh[0].preferences[0].max_healthy_percentage == 101
    error_message = "Termination is held 15 minutes and a refresh replaces one worker at a time."
  }
  assert {
    condition     = aws_autoscaling_policy.fleet_memory[0].target_tracking_configuration[0].disable_scale_in && aws_autoscaling_policy.fleet_cpu[0].target_tracking_configuration[0].disable_scale_in && aws_autoscaling_policy.fleet_scale_in[0].scaling_adjustment == -1 && aws_autoscaling_policy.fleet_scale_in[0].cooldown == 10800
    error_message = "Target tracking only scales out; scale-in removes one worker per three hours."
  }
  assert {
    condition     = aws_autoscaling_policy.fleet_memory[0].target_tracking_configuration[0].target_value == 65 && aws_autoscaling_policy.fleet_cpu[0].target_tracking_configuration[0].target_value == 60
    error_message = "The fleet holds average memory at 65% and average CPU at 60%."
  }
  assert {
    condition     = aws_autoscaling_policy.fleet_memory[0].target_tracking_configuration[0].customized_metric_specification[0].namespace == "Warmbly/Fleet" && aws_autoscaling_policy.fleet_memory[0].target_tracking_configuration[0].customized_metric_specification[0].metric_name == "WorkerMemoryPercent" && aws_autoscaling_policy.fleet_memory[0].target_tracking_configuration[0].customized_metric_specification[0].statistic == "Average" && aws_autoscaling_policy.fleet_memory[0].target_tracking_configuration[0].customized_metric_specification[0].metric_dimension[0].value == "warmbly-production"
    error_message = "Memory is read from Warmbly/Fleet WorkerMemoryPercent with the deployment name as its Fleet dimension."
  }
  assert {
    condition     = aws_cloudwatch_metric_alarm.fleet_scale_in[0].evaluation_periods == 36 && aws_cloudwatch_metric_alarm.fleet_scale_in[0].treat_missing_data == "notBreaching"
    error_message = "Scale-in needs three quiet hours and never fires on missing data."
  }
  assert {
    condition     = one([for q in aws_cloudwatch_metric_alarm.fleet_scale_in[0].metric_query : q.expression if q.return_data == true]) == "IF(mem < 35 AND memmax < 60 AND cpu < 30, 1, 0)"
    error_message = "Scale-in is quiet only while fleet memory, the fullest worker's memory and the busiest worker's CPU are all low."
  }
  assert {
    condition     = toset(flatten([for q in aws_cloudwatch_metric_alarm.fleet_scale_in[0].metric_query : [for m in q.metric : "${q.id}=${m.metric_name}/${m.stat}"]])) == toset(["mem=WorkerMemoryPercent/Maximum", "memmax=WorkerMemoryMaxPercent/Maximum", "cpu=CPUUtilization/Maximum"])
    error_message = "Scale-in reads WorkerMemoryPercent, WorkerMemoryMaxPercent and EC2 CPUUtilization at their maximum."
  }
  assert {
    condition     = alltrue([for t in aws_autoscaling_policy.fleet_memory[0].target_tracking_configuration : alltrue([for c in t.customized_metric_specification : c.metric_name != "WorkerUtilizationPercent"])]) && alltrue([for q in aws_cloudwatch_metric_alarm.fleet_scale_in[0].metric_query : alltrue([for m in q.metric : m.metric_name != "WorkerUtilizationPercent"])])
    error_message = "Mailbox utilization is informational; no scaling decision reads it."
  }
  assert {
    condition     = output.fleet.metric.namespace == "Warmbly/Fleet" && output.fleet.metric.names.memory == "WorkerMemoryPercent" && output.fleet.metric.names.memory_max == "WorkerMemoryMaxPercent" && output.fleet.metric.names.utilization == "WorkerUtilizationPercent"
    error_message = "The fleet output documents all three published metric names."
  }
  assert {
    condition     = length(aws_autoscaling_group.nat) == 2 && aws_autoscaling_group.nat[0].max_size == 1 && length(aws_eip.nat) == 2 && length(output.fleet.nat_ipv4_addresses) == 2
    error_message = "Each zone has one self-healing NAT instance and a stable address."
  }
  assert {
    condition     = strcontains(base64decode(aws_launch_template.nat[0].user_data), "CONNTRACK_MAX='524288'") && strcontains(base64decode(aws_launch_template.nat[0].user_data), "TCP_ESTABLISHED='172800'") && strcontains(base64decode(aws_launch_template.nat[0].user_data), "SOURCE_CIDRS='10.0.20.0/24 10.0.21.0/24'")
    error_message = "NAT instances keep idle connections for two days and masquerade only the fleet subnets."
  }
  assert {
    condition     = length(aws_vpc_security_group_ingress_rule.fleet_nats) == 1 && aws_vpc_security_group_ingress_rule.fleet_nats[0].security_group_id == "sg-nats"
    error_message = "The fleet is allowed to reach NATS when the broker group is given."
  }
}

run "fleet_worker_capacity_takes_a_number" {
  command = plan
  variables {
    fleet_enabled         = true
    fleet_instance_url    = "https://app.example.com"
    fleet_worker_capacity = "1500"
  }
  assert {
    condition     = strcontains(base64decode(aws_launch_template.fleet[0].user_data), "CAPACITY='1500'")
    error_message = "A fixed capacity reaches the worker as written."
  }
}

run "fleet_worker_capacity_rejects_garbage" {
  command = plan
  variables {
    fleet_enabled         = true
    fleet_instance_url    = "https://app.example.com"
    fleet_worker_capacity = "0"
  }
  expect_failures = [var.fleet_worker_capacity]
}

run "fleet_worker_capacity_rejects_what_the_worker_discards" {
  command = plan
  variables {
    fleet_enabled         = true
    fleet_instance_url    = "https://app.example.com"
    fleet_worker_capacity = "100000000"
  }
  expect_failures = [var.fleet_worker_capacity]
}

run "fleet_scale_in_sits_under_scale_out" {
  command = plan
  variables {
    fleet_enabled               = true
    fleet_instance_url          = "https://app.example.com"
    fleet_scale_in_memory_below = 70
  }
  expect_failures = [aws_launch_template.fleet[0]]
}

run "fleet_needs_instance_url" {
  command = plan
  variables {
    fleet_enabled = true
  }
  expect_failures = [aws_launch_template.fleet[0]]
}

run "fleet_worker_capacity_takes_the_upper_bound" {
  command = plan
  variables {
    fleet_enabled         = true
    fleet_instance_url    = "https://app.example.com"
    fleet_worker_capacity = "99999999"
  }
  assert {
    condition     = strcontains(base64decode(aws_launch_template.fleet[0].user_data), "CAPACITY='99999999'")
    error_message = "The largest accepted capacity reaches the worker."
  }
}
