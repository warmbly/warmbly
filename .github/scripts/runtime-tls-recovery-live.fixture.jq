def services: ["backend", "consumer", "forms", "tracking", "realtime"];
def public_services: ["backend", "forms", "tracking", "realtime"];
def arn($kind; $id): "arn:aws:elasticloadbalancing:us-east-1:925323749693:" + $kind + "/" + $id;
def http($name): arn("targetgroup"; "warmbly-production-"+$name+"/0123456789abcdef");
def tls($name): arn("targetgroup"; "warmbly-production-"+$name+"-tls/1123456789abcdef");
def service($name): "arn:aws:ecs:us-east-1:925323749693:service/warmbly-production/warmbly-production-"+$name;
def definition($name): "arn:aws:ecs:us-east-1:925323749693:task-definition/warmbly-production-"+$name+":47";
def image($name): "ghcr.io/warmbly/warmbly/"+$name+"@"+$digest;
"arn:aws:ecs:us-east-1:925323749693:cluster/warmbly-production" as $cluster |
arn("loadbalancer"; "app/warmbly-production/0123456789abcdef") as $lb |
arn("listener"; "app/warmbly-production/0123456789abcdef/0123456789abcdef") as $listener |
"vpc-0123456789abcdef0" as $vpc | "sg-0123456789abcdef0" as $sg |
"-----BEGIN CERTIFICATE-----\nfixture\n-----END CERTIFICATE-----" as $cert |
([range(9) | . as $i | {arn:arn("listener-rule"; "app/warmbly-production/0123456789abcdef/0123456789abcdef/"+($i|tostring)),
  hosts:["host"+($i|tostring)+".example.com"],target:tls(public_services[$i%4])}]) as $routes |
(public_services | map({TargetGroupName:("warmbly-production-"+.),TargetGroupArn:http(.),Protocol:"HTTP",Port:8080,VpcId:$vpc,LoadBalancerArns:[],targets:[]})) as $http |
{baseline:{account:"925323749693",region:"us-east-1",httpGroups:$http,
  state:{lineage:"01234567-89ab-cdef-0123-456789abcdef",serial:64,versionId:"state-version",sha256:("a"*64),httpGroups:$http},
  snapshot:{sha256:("b"*64),releaseTag:"v1.2.3"}},
 vars:{name:"warmbly-production",target_tls:"on",backend_internal_certificate:$cert},
 images:{tag:"v1.2.3",registry:"ghcr.io/warmbly/warmbly",images:(services|map({key:.,value:$digest})|from_entries)},
 state:{version:4,lineage:"01234567-89ab-cdef-0123-456789abcdef",serial:64,resources:[
  {mode:"managed",type:"aws_lb_target_group",name:"service",instances:[$http[]|{
    index_key:(.TargetGroupName|ltrimstr("warmbly-production-")),attributes:{arn:.TargetGroupArn,id:.TargetGroupArn,name:.TargetGroupName,protocol:.Protocol,port:.Port,vpc_id:.VpcId}}]},
  {mode:"managed",type:"aws_lb_target_group",name:"service_tls",instances:[public_services[]|{index_key:.,attributes:{arn:tls(.),name:("warmbly-production-"+.+"-tls"),protocol:"HTTPS",port:8443,vpc_id:$vpc}}]},
  {mode:"managed",type:"aws_ecs_service",name:"service",instances:[services[]|{index_key:.,attributes:{id:service(.),cluster:$cluster,network_configuration:[{security_groups:[$sg]}]}}]},
  {mode:"managed",type:"aws_lb",name:"main",instances:[{attributes:{arn:$lb}}]},
  {mode:"managed",type:"aws_lb_listener",name:"https",instances:[{attributes:{arn:$listener}}]},
  {mode:"managed",type:"aws_lb_listener_rule",name:"service",instances:[$routes[]|{attributes:{arn:.arn,condition:[{host_header:[{values:.hosts}]}],action:[{target_group_arn:.target}]}}]}
 ]},
 live:{account:{Account:"925323749693"},services:{failures:[],services:[services[]|. as $name|{
   serviceName:("warmbly-production-"+.),serviceArn:service(.),clusterArn:$cluster,taskDefinition:definition(.),status:"ACTIVE",desiredCount:1,runningCount:1,pendingCount:0,
   deployments:[{status:"PRIMARY",rolloutState:"COMPLETED",taskDefinition:definition(.)}],
   networkConfiguration:{awsvpcConfiguration:{securityGroups:[$sg]}},
   loadBalancers:(if .=="consumer" then [] else [{containerName:.,containerPort:8443,targetGroupArn:tls(.)}] end)}]},
  definitions:[services[]|. as $name|{taskDefinition:{taskDefinitionArn:definition(.),runtimePlatform:{cpuArchitecture:"X86_64"},containerDefinitions:[{
    name:.,image:image(.),environment:([
      {name:"SERVE_TLS",value:(if .=="backend" then "supplied" else "self-signed" end)},
      {name:"SERVE_TLS_PORT",value:"8443"},{name:"SERVE_HTTP_LOOPBACK_ONLY",value:"true"}]+
      (if .=="backend" then [{name:"SERVE_TLS_CERT",value:$cert}] elif .=="forms" or .=="tracking" then
        [{name:"BACKEND_INTERNAL_URL",value:"https://backend.warmbly.internal:8443"},{name:"BACKEND_INTERNAL_CA",value:$cert}] else [] end))}]},
    tags:[{key:"warmbly-release",value:"v1.2.3"},{key:"warmbly-image-variant",value:"default"}]}],
  tasks:[services[]|{taskArn:("task-"+.),group:("service:warmbly-production-"+.),taskDefinitionArn:definition(.),clusterArn:$cluster,
    lastStatus:"RUNNING",desiredStatus:"RUNNING",healthStatus:(if .=="consumer" then "UNKNOWN" else "HEALTHY" end),overrides:{containerOverrides:[]},
    containers:[{name:.,image:image(.),imageDigest:$digest,lastStatus:"RUNNING",healthStatus:(if .=="consumer" then "UNKNOWN" else "HEALTHY" end),networkInterfaces:[{privateIpv4Address:"10.0.0.1"}]}]}],
  manifests:[services[]|{image:image(.),digest:$digest,document:{schemaVersion:2}}],
  httpsGroups:[public_services[]|{TargetGroupArn:tls(.),TargetGroupName:("warmbly-production-"+.+"-tls"),Protocol:"HTTPS",Port:8443,HealthCheckProtocol:"HTTPS",VpcId:$vpc,LoadBalancerArns:[$lb],
    targets:[{Target:{Id:"10.0.0.1",Port:8443},TargetHealth:{State:"healthy"}}]}],httpGroups:$http,
  listeners:[{ListenerArn:$listener,LoadBalancerArn:$lb,Port:443,Protocol:"HTTPS"}],
  rules:([$routes[]|{RuleArn:.arn,IsDefault:false,Conditions:[{Field:"host-header",HostHeaderConfig:{Values:.hosts}}],Actions:[{Type:"forward",TargetGroupArn:.target}]}]+
    [{IsDefault:true,Actions:[{Type:"fixed-response",Order:1,FixedResponseConfig:{StatusCode:"404",ContentType:"text/plain",MessageBody:"Not found"}}]}]),
  securityGroups:[{GroupId:$sg,VpcId:$vpc,IpPermissions:[{IpProtocol:"tcp",FromPort:8443,ToPort:8443}]}],
  alarms:{CompositeAlarms:[],MetricAlarms:[public_services[] as $name|["target-5xx-rate","unhealthy-hosts"][]|
    {AlarmName:("warmbly-production-"+$name+"-"+.),StateValue:"OK",ActionsEnabled:true,
      Dimensions:[{Name:"TargetGroup",Value:(tls($name)|split(":")[-1])},{Name:"LoadBalancer",Value:($lb|split(":")[-1]|ltrimstr("loadbalancer/"))}]}]},
  public:(["https://api.warmbly.com/health","https://api.warmbly.com/v1/auth/config","https://forms.warmbly.com/health","https://realtime.warmbly.com/health","https://track.monitorably.com/health"]|map({url:.,status:200}))}}
