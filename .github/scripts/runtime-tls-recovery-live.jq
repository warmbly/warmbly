def public_services: ["backend", "forms", "tracking", "realtime"];
def services: ["backend", "consumer", "forms", "tracking", "realtime"];
def instances($type; $name):
  [$state[0].resources[] | select(.mode == "managed" and .module == null and .type == $type and .name == $name) | .instances[]];
def identity: {TargetGroupName,TargetGroupArn,Protocol,Port,VpcId};
def environment: .environment | map({key:.name,value:.value}) | from_entries;
def targets:
  [.Actions[] | select(.Type == "forward") |
    .TargetGroupArn?, .ForwardConfig.TargetGroups[]?.TargetGroupArn | select(. != null)] | unique;

$baseline[0] as $b | $vars[0] as $v | $images[0] as $images |
instances("aws_ecs_service"; "service") as $expectedServices |
instances("aws_lb_target_group"; "service_tls") as $tls |
(instances("aws_lb"; "main") | map(.attributes.arn)) as $lbs |
(instances("aws_lb_listener"; "https") | map(.attributes.arn)) as $listeners |
([instances("aws_lb_listener_rule"; "service")[], instances("aws_lb_listener_rule"; "tracking_alias")[]] |
  map({arn:.attributes.arn, hosts:([.attributes.condition[].host_header[].values[]] | sort),
    target:.attributes.action[0].target_group_arn})) as $routes |
($expectedServices | map(.attributes.network_configuration[].security_groups[]) | unique) as $sgs |
($v.target_tls_port // 8443) as $port |
. as $live |
.account.Account == "925323749693" and
($images.tag == $b.snapshot.releaseTag and $images.registry == "ghcr.io/warmbly/warmbly") and
($expectedServices | map(.index_key) | sort) == (services | sort) and
($tls | map(.index_key) | sort) == (public_services | sort) and
($lbs | length == 1) and ($listeners | length == 1) and
($routes | length == 9) and ($routes | map(.arn) | unique | length == 9) and
all($routes[]; .hosts | length > 0) and
all($tls[]; .attributes.protocol == "HTTPS" and .attributes.port == $port and
  .attributes.vpc_id == $b.httpGroups[0].VpcId) and
all($routes[]; .target as $target | any($tls[]; .attributes.arn == $target)) and
(.services.failures | length == 0) and
(.services.services | map(.serviceName) | sort) == (services | map("warmbly-production-" + .) | sort) and
all(.services.services[];
  . as $s | (.serviceName | ltrimstr("warmbly-production-")) as $name |
  any($expectedServices[]; .index_key == $name and .attributes.id == $s.serviceArn and .attributes.cluster == $s.clusterArn) and
  .status == "ACTIVE" and .desiredCount > 0 and .runningCount == .desiredCount and .pendingCount == 0 and
  (.deployments | length == 1) and .deployments[0].status == "PRIMARY" and
  .deployments[0].rolloutState == "COMPLETED" and .deployments[0].taskDefinition == .taskDefinition and
  (.networkConfiguration.awsvpcConfiguration.securityGroups | sort) == $sgs and
  (if $name == "consumer" then (.loadBalancers | length == 0) else
    (.loadBalancers | length == 1) and .loadBalancers[0].containerName == $name and
    .loadBalancers[0].containerPort == $port and
    any($tls[]; .index_key == $name and .attributes.arn == $s.loadBalancers[0].targetGroupArn) end) and
  ([$live.definitions[] | select(.taskDefinition.taskDefinitionArn == $s.taskDefinition)] | length == 1) and
  ([$live.definitions[] | select(.taskDefinition.taskDefinitionArn == $s.taskDefinition)][0] as $d |
    ([$d.tags[] | select(.key == "warmbly-image-variant") | .value]) as $variant |
    ($name + (if $variant == ["kafka"] then "-kafka" else "" end)) as $key |
    ($images.registry + "/" + $name + "@" + $images.images[$key]) as $image |
    (if $name == "consumer" and $d.taskDefinition.containerDefinitions[0].healthCheck == null then "UNKNOWN" else "HEALTHY" end) as $health |
    ([$d.tags[] | select(.key == "warmbly-release") | .value] == [$images.tag]) and
    ($variant == ["default"] or $variant == ["kafka"]) and
    ($images.images[$key] | type == "string" and test("^sha256:[a-f0-9]{64}$")) and
    ($d.taskDefinition.containerDefinitions | length == 1) and
    (["X86_64", "ARM64"] | index($d.taskDefinition.runtimePlatform.cpuArchitecture // "X86_64") != null) and
    ($d.taskDefinition.runtimePlatform.operatingSystemFamily // "LINUX") == "LINUX" and
    ($d.taskDefinition.containerDefinitions[0] as $c |
      ($c | environment) as $env |
      $c.name == $name and $c.image == $image and
      ($c.environment | map(.name) | unique | length) == ($c.environment | length) and
      ($c.environmentFiles // [] | length == 0) and
      all($c.secrets[]?; .name | test("^(SERVE_TLS|SERVE_TLS_PORT|SERVE_TLS_CERT|SERVE_HTTP_LOOPBACK_ONLY|BACKEND_INTERNAL_URL|BACKEND_INTERNAL_CA)$") | not) and
      (if $name == "consumer" then true else
        $env.SERVE_TLS == (if $name == "backend" then "supplied" else "self-signed" end) and
        $env.SERVE_TLS_PORT == ($port | tostring) and $env.SERVE_HTTP_LOOPBACK_ONLY == "true" and
        (if $name == "backend" then $env.SERVE_TLS_CERT == $v.backend_internal_certificate else true end) and
        (if $name == "forms" or $name == "tracking" then
          $env.BACKEND_INTERNAL_URL == ("https://backend." + ($v.namespace // "warmbly.internal") + ":" + ($port | tostring)) and
          $env.BACKEND_INTERNAL_CA == $v.backend_internal_certificate else true end) end)) and
    ([$live.tasks[] | select(.group == ("service:" + $s.serviceName))] as $tasks |
      ($tasks | length) == $s.desiredCount and
      all($tasks[]; .taskDefinitionArn == $s.taskDefinition and .clusterArn == $s.clusterArn and
        .lastStatus == "RUNNING" and .desiredStatus == "RUNNING" and .healthStatus == $health and
        (.overrides.containerOverrides | all(.[]; (.environment // [] | length == 0) and (.command // [] | length == 0) and (.environmentFiles // [] | length == 0))) and
        (.containers | length == 1) and .containers[0].name == $name and .containers[0].image == $image and
        .containers[0].lastStatus == "RUNNING" and .containers[0].healthStatus == $health and
        (.containers[0].imageDigest as $digest |
          any($live.manifests[]; .image == $image and .digest == ($image | split("@")[1]) and
            (if .document.manifests then
              .digest as $index |
              any(.document.manifests[]; (.digest == $digest or $index == $digest) and .platform.os == "linux" and
                .platform.architecture == (if ($d.taskDefinition.runtimePlatform.cpuArchitecture // "X86_64") == "ARM64" then "arm64" else "amd64" end))
             else .digest == $digest end))))))) and
(.tasks | map(.taskArn) | unique | length) == (.tasks | length) and
(.tasks | length) == ([.services.services[].desiredCount] | add) and
(.definitions | length == 5) and
(.httpsGroups | map(.TargetGroupArn) | sort) == ($tls | map(.attributes.arn) | sort) and
all(.httpsGroups[];
  . as $g | .Protocol == "HTTPS" and .Port == $port and .HealthCheckProtocol == "HTTPS" and
  .VpcId == $b.httpGroups[0].VpcId and .LoadBalancerArns == $lbs and
  (.targets | length > 0) and all(.targets[]; .TargetHealth.State == "healthy" and .Target.Port == $port) and
  ([.targets[].Target.Id] | sort) ==
    ([$live.tasks[] | select(.group == ("service:" + ($g.TargetGroupName | rtrimstr("-tls")))) |
      .containers[0].networkInterfaces[].privateIpv4Address] | sort) and
  any($tls[]; .attributes.arn == $g.TargetGroupArn and .attributes.name == $g.TargetGroupName)) and
(if $phase == "after" then .httpGroups == [] else
  (.httpGroups | map(identity) | sort_by(.TargetGroupArn)) == ($b.httpGroups | map(identity) | sort_by(.TargetGroupArn)) end) and
all(.httpGroups[]; .LoadBalancerArns == [] and .targets == []) and
(.listeners | length == 1) and .listeners[0].ListenerArn == $listeners[0] and
.listeners[0].LoadBalancerArn == $lbs[0] and .listeners[0].Port == 443 and .listeners[0].Protocol == "HTTPS" and
(.rules | length == 10) and
([.rules[] | select(.IsDefault == true)] | length == 1) and
all(.rules[] | select(.IsDefault == true); .Actions == [{Type:"fixed-response",Order:1,FixedResponseConfig:{StatusCode:"404",ContentType:"text/plain",MessageBody:"Not found"}}]) and
([.rules[] | select(.IsDefault == false) | .RuleArn] | sort) == ($routes | map(.arn) | sort) and
all(.rules[] | select(.IsDefault == false);
  . as $rule | (.Transforms // [] | length == 0) and (.Conditions | length == 1) and
  .Conditions[0].Field == "host-header" and (.Actions | length == 1) and .Actions[0].Type == "forward" and
  all(.Actions[0].ForwardConfig.TargetGroups[]?; (.Weight // 1) > 0) and
  any($routes[]; .arn == $rule.RuleArn and
    .hosts == ($rule.Conditions[0].HostHeaderConfig.Values | sort) and ($rule | targets) == [.target])) and
(.securityGroups | map(.GroupId) | sort) == ($sgs | sort) and ($sgs | length > 0) and
all(.securityGroups[]; .VpcId == $b.httpGroups[0].VpcId and
  all(.IpPermissions[]; . as $permission |
    .IpProtocol != "-1" and
    (if .IpProtocol == "tcp" or .IpProtocol == "6" then
      (.FromPort | type == "number") and (.ToPort | type == "number") and
      all($b.httpGroups[]; .Port < $permission.FromPort or .Port > $permission.ToPort) else true end))) and
(.alarms.CompositeAlarms | length == 0) and
(.alarms.MetricAlarms | map(.AlarmName) | sort) ==
  ([public_services[] as $name | ["target-5xx-rate", "unhealthy-hosts"][] | "warmbly-production-" + $name + "-" + .] | sort) and
all(.alarms.MetricAlarms[];
  . as $alarm | .StateValue == "OK" and .ActionsEnabled == true and
  any($tls[];
    . as $group | ($alarm.AlarmName | startswith("warmbly-production-" + $group.index_key + "-")) and
    ([$alarm.Dimensions[]?, $alarm.Metrics[]?.MetricStat.Metric.Dimensions[]? | select(.Name == "TargetGroup") | .Value] | unique) ==
      [($group.attributes.arn | split(":")[-1])] and
    ([$alarm.Dimensions[]?, $alarm.Metrics[]?.MetricStat.Metric.Dimensions[]? | select(.Name == "LoadBalancer") | .Value] | unique) ==
      [($lbs[0] | split(":")[-1] | ltrimstr("loadbalancer/"))])) and
(.public | map(.url) | sort) == (["https://api.warmbly.com/health", "https://api.warmbly.com/v1/auth/config",
  "https://forms.warmbly.com/health", "https://realtime.warmbly.com/health", "https://track.monitorably.com/health"] | sort) and
all(.public[]; .status == 200)
