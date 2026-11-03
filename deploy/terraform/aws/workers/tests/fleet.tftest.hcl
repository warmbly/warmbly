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
    condition     = strcontains(base64decode(aws_launch_template.fleet[0].user_data), "WARMBLY_WORKER_CAPACITY=$CAPACITY") && strcontains(base64decode(aws_launch_template.fleet[0].user_data), "CAPACITY='2000'") && strcontains(base64decode(aws_launch_template.fleet[0].user_data), "INSTANCE_URL='https://app.example.com'") && strcontains(base64decode(aws_launch_template.fleet[0].user_data), "TOKEN_PARAMETER='/warmbly-production/fleet/join-token'")
    error_message = "User data sets the worker capacity and names the join token parameter."
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
    condition     = aws_autoscaling_policy.fleet_utilization[0].target_tracking_configuration[0].disable_scale_in && aws_autoscaling_policy.fleet_cpu[0].target_tracking_configuration[0].disable_scale_in && aws_autoscaling_policy.fleet_scale_in[0].scaling_adjustment == -1 && aws_autoscaling_policy.fleet_scale_in[0].cooldown == 10800
    error_message = "Target tracking only scales out; scale-in removes one worker per three hours."
  }
  assert {
    condition     = aws_autoscaling_policy.fleet_utilization[0].target_tracking_configuration[0].customized_metric_specification[0].namespace == "Warmbly/Fleet" && aws_autoscaling_policy.fleet_utilization[0].target_tracking_configuration[0].customized_metric_specification[0].metric_dimension[0].value == "warmbly-production"
    error_message = "Utilization is read from Warmbly/Fleet with the deployment name as its Fleet dimension."
  }
  assert {
    condition     = aws_cloudwatch_metric_alarm.fleet_scale_in[0].evaluation_periods == 36 && aws_cloudwatch_metric_alarm.fleet_scale_in[0].treat_missing_data == "notBreaching"
    error_message = "Scale-in needs three quiet hours and never fires on missing data."
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

run "fleet_needs_instance_url" {
  command = plan
  variables {
    fleet_enabled = true
  }
  expect_failures = [aws_launch_template.fleet[0]]
}
