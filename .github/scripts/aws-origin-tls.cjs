const { spawnSync } = require("node:child_process");
const { appendFileSync, mkdirSync, readFileSync, writeFileSync, existsSync } = require("node:fs");
const { join } = require("node:path");
const { ACCOUNT, BUCKET, ROLE, LB, LISTENER, DESIRED, hash, canonical } = require("./aws-origin-alb.cjs");
const { CBC, GCM, TLS13, classify, validTargets } = require("./origin-tls-client.cjs");

const REGION = "us-east-1";
const ECS = `arn:aws:ecs:${REGION}:${ACCOUNT}`;
const CLUSTER = `${ECS}:cluster/warmbly-production`;
const SERVICE = `${ECS}:service/warmbly-production/warmbly-production-backend`;
const EXECUTION = `arn:aws:iam::${ACCOUNT}:role/warmbly-production-validation-execution`;
const VPC = "vpc-038e4f0058030800b";
const SG = "sg-0ddd1eb3234dad9e7";
const ALB_SG = "sg-03a4f6f8010daff8d";
const LOG = "/ecs/warmbly-production/validation";
const PREFIX = "warmbly/production/diagnostics/origin-tls";
// Official Node 22 bookworm ARM64 manifest, including OpenSSL and Debian public CAs.
const IMAGE = "docker.io/library/node@sha256:a288699fc0f9676950742fae6efcd015180aa7ef2bf8a6477e4b0a8cc0ab1b3f";
const requireThat = (ok, message) => { if (!ok) throw new Error(message); };
const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));
const originIdentity = (enis) => canonical(enis.map((e) =>
  [e.NetworkInterfaceId, e.PrivateIpAddress, e.AvailabilityZone, e.SubnetId]).sort());

function configuration(env) {
  requireThat(env.GITHUB_ACTIONS === "true" && env.GITHUB_REPOSITORY === "warmbly/warmbly" &&
    env.GITHUB_REF === "refs/heads/main" && env.GITHUB_EVENT_NAME === "workflow_dispatch" &&
    env.GITHUB_WORKFLOW_REF === "warmbly/warmbly/.github/workflows/probe-origin-tls-aws.yml@refs/heads/main" &&
    env.GITHUB_RUN_ATTEMPT === "1" && /^[1-9]\d*$/.test(env.GITHUB_RUN_ID || "") &&
    /^[a-f0-9]{40}$/.test(env.GITHUB_SHA || ""), "A new main workflow dispatch is required; reruns are refused");
  requireThat(env.STATE_BUCKET === BUCKET && env.APPLY_ROLE === ROLE && env.WORK &&
    env.GITHUB_OUTPUT && env.GITHUB_STEP_SUMMARY, "Normal aws-infrastructure role, bucket and private paths required");
  return { run: env.GITHUB_RUN_ID, source: env.GITHUB_SHA, work: env.WORK,
    family: `warmbly-production-origin-tls-${env.GITHUB_RUN_ID}`, startedBy: `origin-tls-${env.GITHUB_RUN_ID}` };
}

function sessionPolicy(config, phase) {
  requireThat(phase === "probe" || phase === "cleanup", "Explicit diagnostic credential phase required");
  const statements = phase === "probe" ? [
    { Effect: "Allow", Action: ["elasticloadbalancing:DescribeLoadBalancers", "elasticloadbalancing:DescribeListeners",
      "ec2:DescribeNetworkInterfaces", "ec2:DescribeSubnets", "ecs:RegisterTaskDefinition"], Resource: "*" },
    { Effect: "Allow", Action: "ecs:DescribeServices", Resource: SERVICE },
    { Effect: "Allow", Action: "ecs:RunTask", Resource: `${ECS}:task-definition/${config.family}:*`,
      Condition: { ArnEquals: { "ecs:cluster": CLUSTER } } },
    { Effect: "Allow", Action: "ecs:DescribeTasks", Resource: `${ECS}:task/warmbly-production/*` },
    { Effect: "Allow", Action: "iam:PassRole", Resource: EXECUTION,
      Condition: { StringEquals: { "iam:PassedToService": "ecs-tasks.amazonaws.com" } } },
  ] : [
    { Effect: "Allow", Action: ["ecs:ListTaskDefinitions", "ecs:ListTasks"], Resource: "*" },
    { Effect: "Allow", Action: "ecs:DeregisterTaskDefinition", Resource: "*" },
    { Effect: "Allow", Action: ["ecs:DescribeTasks", "ecs:StopTask"], Resource: `${ECS}:task/warmbly-production/*` },
    { Effect: "Allow", Action: "logs:GetLogEvents", Resource: `arn:aws:logs:${REGION}:${ACCOUNT}:log-group:${LOG}:log-stream:origin-tls-${config.run}/*` },
  ];
  return { Version: "2012-10-17", Statement: [...statements,
    { Effect: "Allow", Action: "s3:PutObject", Resource: `arn:aws:s3:::${BUCKET}/${PREFIX}/${config.run}/1/*`,
      Condition: { StringEquals: { "s3:x-amz-server-side-encryption": "AES256", "s3:ResourceAccount": ACCOUNT },
        Bool: { "aws:SecureTransport": "true" }, Null: { "s3:if-none-match": "false" } } },
  ] };
}

function awsCLI(service, action, args = []) {
  const result = spawnSync("aws", [service, action, ...args, "--region", REGION, "--output", "json", "--no-cli-pager",
    "--cli-connect-timeout", "5", "--cli-read-timeout", "8"], { encoding: "utf8", timeout: 15000,
    maxBuffer: 4 * 1024 * 1024, env: { ...process.env, AWS_PAGER: "", AWS_CLI_AUTO_PROMPT: "off", AWS_MAX_ATTEMPTS: "1" } });
  requireThat(!result.error && result.status === 0, `AWS ${service} ${action} failed; inspect normal access/configuration prerequisites`);
  try { return JSON.parse(result.stdout); } catch { throw new Error("AWS diagnostic response was not valid JSON"); }
}

async function discover(aws) {
  const lb = (await aws("elbv2", "describe-load-balancers", ["--load-balancer-arns", LB])).LoadBalancers;
  requireThat(lb?.length === 1 && lb[0].LoadBalancerArn === LB && lb[0].VpcId === VPC &&
    lb[0].State?.Code === "active" && lb[0].Type === "application" && lb[0].IpAddressType === "ipv4" &&
    canonical(lb[0].SecurityGroups) === canonical([ALB_SG]), "Exact active origin ALB required");
  const listener = (await aws("elbv2", "describe-listeners", ["--listener-arns", LISTENER])).Listeners;
  requireThat(listener?.length === 1 && listener[0].ListenerArn === LISTENER && listener[0].LoadBalancerArn === LB &&
    listener[0].Protocol === "HTTPS" && listener[0].Port === 443 && listener[0].SslPolicy === DESIRED,
  "Bound HTTPS listener must have the reviewed restricted policy");
  const enis = (await aws("ec2", "describe-network-interfaces", ["--filters",
    "Name=description,Values=ELB app/warmbly-production-ingress/19d5c82c1651c871", `Name=vpc-id,Values=${VPC}`])).NetworkInterfaces;
  requireThat(enis?.length >= 2 && lb[0].AvailabilityZones?.length === 2 &&
    new Set(enis.map((e) => e.AvailabilityZone)).size === 2 && new Set(enis.map((e) => e.SubnetId)).size === 2 &&
    new Set(enis.map((e) => e.NetworkInterfaceId)).size === enis.length && validTargets(enis.map((e) => e.PrivateIpAddress)) &&
    enis.every((e) => /^eni-[a-f0-9]+$/.test(e.NetworkInterfaceId || "") && e.VpcId === VPC && e.Status === "in-use" &&
      e.RequesterManaged === true && e.OwnerId === ACCOUNT && e.Description === "ELB app/warmbly-production-ingress/19d5c82c1651c871" &&
      e.Groups?.length === 1 && e.Groups[0].GroupId === ALB_SG &&
      lb[0].AvailabilityZones?.some((z) => z.ZoneName === e.AvailabilityZone && z.SubnetId === e.SubnetId)), "Complete exact-ALB private ENIs required");
  const service = (await aws("ecs", "describe-services", ["--cluster", CLUSTER, "--services", SERVICE])).services;
  const network = service?.[0]?.networkConfiguration?.awsvpcConfiguration;
  requireThat(service?.length === 1 && service[0].serviceArn === SERVICE && service[0].status === "ACTIVE" &&
    network?.assignPublicIp === "ENABLED" && canonical(network.securityGroups) === canonical([SG]) &&
    network.subnets?.length === 2 && new Set(network.subnets).size === 2 &&
    enis.every((e) => network.subnets.includes(e.SubnetId)), "Existing application task network required");
  const subnets = (await aws("ec2", "describe-subnets", ["--subnet-ids", ...network.subnets])).Subnets;
  requireThat(subnets?.length === 2 && new Set(subnets.map((s) => s.SubnetId)).size === 2 &&
    subnets.every((s) => s.VpcId === VPC && s.State === "available" &&
    enis.some((e) => e.SubnetId === s.SubnetId)), "Existing application/ALB subnets required");
  return { at: new Date().toISOString(), lb: lb[0], listener: listener[0], enis, network };
}

function definition(config, targets) {
  return { family: config.family, networkMode: "awsvpc", requiresCompatibilities: ["FARGATE"], cpu: "256", memory: "512",
    executionRoleArn: EXECUTION, runtimePlatform: { cpuArchitecture: "ARM64", operatingSystemFamily: "LINUX" },
    containerDefinitions: [{ name: "probe", image: IMAGE, essential: true, user: "1000:1000", readonlyRootFilesystem: true,
      linuxParameters: { capabilities: { drop: ["ALL"] } }, stopTimeout: 10,
      entryPoint: ["timeout", "-s", "KILL", "210", "node"], command: ["-e", readFileSync(join(__dirname, "origin-tls-client.cjs"), "utf8")],
      environment: [{ name: "ORIGIN_TLS_TASK", value: "1" }, { name: "ORIGIN_TLS_TARGETS", value: JSON.stringify(targets) }],
      logConfiguration: { logDriver: "awslogs", options: { "awslogs-group": LOG, "awslogs-region": REGION,
        "awslogs-stream-prefix": `origin-tls-${config.run}`, mode: "blocking" } } }] };
}

const save = (config, evidence) => writeFileSync(join(config.work, "evidence.json"), JSON.stringify(evidence), { mode: 0o600 });
const load = (config) => JSON.parse(readFileSync(join(config.work, "evidence.json")));
async function archive(aws, config, name, evidence) {
  const bytes = Buffer.from(JSON.stringify(evidence));
  const path = join(config.work, name);
  writeFileSync(path, bytes, { mode: 0o600 });
  const checksum = Buffer.from(hash(bytes), "hex").toString("base64");
  const result = await aws("s3api", "put-object", ["--bucket", BUCKET, "--key", `${PREFIX}/${config.run}/1/${name}`,
    "--expected-bucket-owner", ACCOUNT, "--server-side-encryption", "AES256", "--if-none-match", "*",
    "--checksum-algorithm", "SHA256", "--checksum-sha256", checksum, "--body", path]);
  requireThat(result.ServerSideEncryption === "AES256" && result.ChecksumSHA256 === checksum &&
    typeof result.VersionId === "string" && result.VersionId.length > 0 && result.VersionId !== "null", "Private encrypted versioned evidence required");
  return { version: result.VersionId, sha256: hash(bytes) };
}

async function execute(aws, config, pause = sleep) {
  mkdirSync(config.work, { recursive: true, mode: 0o700 });
  const evidence = { run: config.run, source: config.source, success: false, image: IMAGE };
  save(config, evidence);
  try {
    const identity = await aws("sts", "get-caller-identity");
    requireThat(identity.Account === ACCOUNT && /^arn:aws:sts::925323749693:assumed-role\/warmbly-production-terraform-apply\/[^/]+$/.test(identity.Arn || ""), "Existing infrastructure apply identity required");
    evidence.before = await discover(aws);
    const targets = evidence.before.enis.map((e) => e.PrivateIpAddress).sort();
    evidence.definition = definition(config, targets);
    await archive(aws, config, "intent.json", evidence);
    evidence.registerAttempted = true;
    save(config, evidence);
    const registered = await aws("ecs", "register-task-definition", ["--cli-input-json", JSON.stringify(evidence.definition)]);
    evidence.definitionArn = registered.taskDefinition?.taskDefinitionArn;
    requireThat(evidence.definitionArn?.startsWith(`${ECS}:task-definition/${config.family}:`), "Dedicated diagnostic definition required");
    save(config, evidence);
    // Refresh immediately before launch; a changed address requires a new dispatch.
    evidence.prelaunch = await discover(aws);
    requireThat(originIdentity(evidence.prelaunch.enis) === originIdentity(evidence.before.enis) &&
      canonical(evidence.prelaunch.network) === canonical(evidence.before.network), "Origin/task network changed before launch");
    const request = { cluster: CLUSTER, taskDefinition: evidence.definitionArn, launchType: "FARGATE", platformVersion: "1.4.0",
      count: 1, startedBy: config.startedBy, clientToken: config.startedBy, enableExecuteCommand: false,
      networkConfiguration: { awsvpcConfiguration: evidence.before.network } };
    evidence.runAttempted = true;
    save(config, evidence);
    const launched = await aws("ecs", "run-task", ["--cli-input-json", JSON.stringify(request)]);
    evidence.taskArn = launched.tasks?.[0]?.taskArn;
    requireThat(!launched.failures?.length && launched.tasks?.length === 1 &&
      evidence.taskArn?.startsWith(`${ECS}:task/warmbly-production/`), "One disposable diagnostic task required");
    save(config, evidence);
    for (let i = 0; i < 48; i++) {
      const readback = await aws("ecs", "describe-tasks", ["--cluster", CLUSTER, "--tasks", evidence.taskArn]);
      requireThat(!readback.failures?.length && readback.tasks?.length === 1, "Diagnostic task readback required");
      evidence.task = readback.tasks[0];
      requireThat(evidence.task.taskArn === evidence.taskArn && evidence.task.clusterArn === CLUSTER &&
        evidence.task.taskDefinitionArn === evidence.definitionArn && evidence.task.startedBy === config.startedBy &&
        evidence.task.launchType === "FARGATE" && evidence.task.enableExecuteCommand === false, "Exact diagnostic task identity required");
      save(config, evidence);
      if (evidence.task.lastStatus === "STOPPED") break;
      await pause(5000);
    }
    requireThat(evidence.task?.lastStatus === "STOPPED" && evidence.task.containers?.length === 1 &&
      evidence.task.containers[0].name === "probe" && evidence.task.containers[0].imageDigest === IMAGE.split("@")[1] &&
      evidence.task.containers[0].exitCode === 0, "Probe incomplete or inconclusive; inspect private evidence");
    evidence.after = await discover(aws);
    requireThat(originIdentity(evidence.after.enis) === originIdentity(evidence.before.enis) &&
      canonical(evidence.after.network) === canonical(evidence.before.network), "Origin interfaces or task network changed during probe");
    evidence.success = true;
  } catch (error) {
    evidence.failure = error.message;
    throw error;
  } finally { save(config, evidence); }
}

async function finish(aws, config, pause = sleep) {
  if (!existsSync(join(config.work, "evidence.json"))) return;
  const evidence = load(config);
  evidence.cleanup = { errors: [], stopped: false, deregistered: false };
  const attempt = async (action) => { try { return await action(); } catch (error) { evidence.cleanup.errors.push(error.message); return null; } };
  // Recover identities even when a registration or launch response was lost.
  const candidates = new Set([evidence.taskArn].filter(Boolean));
  if (evidence.runAttempted) {
    for (let i = 0; i < 3; i++) {
      const running = await attempt(() => aws("ecs", "list-tasks", ["--cluster", CLUSTER, "--started-by", config.startedBy]));
      // startedBy cannot be combined with another ListTasks filter.
      const stopped = await attempt(() => aws("ecs", "list-tasks", ["--cluster", CLUSTER, "--family", config.family, "--desired-status", "STOPPED"]));
      for (const task of [...(running?.taskArns || []), ...(stopped?.taskArns || [])]) candidates.add(task);
      if (candidates.size) break;
      await pause(5000);
    }
  }
  const tasks = [...candidates];
  const verified = new Set();
  const stopAttempted = new Set();
  evidence.cleanup.candidateTaskArns = tasks;
  save(config, evidence);
  if (tasks.length) {
    for (let i = 0; i < 12; i++) {
      const state = await attempt(() => aws("ecs", "describe-tasks", ["--cluster", CLUSTER, "--tasks", ...tasks]));
      evidence.cleanup.tasks = state;
      const owned = (state?.tasks || []).filter((t) => candidates.has(t.taskArn) && t.clusterArn === CLUSTER &&
        t.startedBy === config.startedBy && typeof evidence.definitionArn === "string" && t.taskDefinitionArn === evidence.definitionArn);
      for (const task of owned) {
        verified.add(task.taskArn);
        if (task.lastStatus !== "STOPPED" && !stopAttempted.has(task.taskArn)) {
          stopAttempted.add(task.taskArn);
          await attempt(() => aws("ecs", "stop-task", ["--cluster", CLUSTER, "--task", task.taskArn, "--reason", "Bounded origin TLS diagnostic cleanup"]));
        }
      }
      if (state && !state.failures?.length && new Set(owned.map((t) => t.taskArn)).size === tasks.length && owned.every((t) => t.lastStatus === "STOPPED")) {
        evidence.cleanup.stopped = true;
        break;
      }
      await pause(5000);
    }
  } else if (!evidence.runAttempted) {
    evidence.cleanup.stopped = true;
  }
  evidence.cleanup.taskArns = [...verified];
  evidence.cleanup.recovery = !evidence.runAttempted ? "not-required" :
    verified.size > 0 && verified.size === tasks.length ? "resolved" : "unresolved";
  if (evidence.cleanup.recovery === "unresolved") evidence.cleanup.errors.push("Diagnostic task identity unresolved");
  const definitionPattern = new RegExp(`^${ECS}:task-definition/${config.family}:\\d+$`);
  const isRunDefinition = (arn) => typeof arn === "string" && definitionPattern.exec(arn)?.[0] === arn;
  const arns = new Set([evidence.definitionArn].filter(isRunDefinition));
  let definitions;
  if (evidence.registerAttempted) {
    for (let i = 0; i < 3; i++) {
      definitions = await attempt(async () => {
        const result = await aws("ecs", "list-task-definitions", ["--family-prefix", config.family, "--status", "ACTIVE"]);
        requireThat(Array.isArray(result?.taskDefinitionArns), "Diagnostic definition discovery unconfirmed");
        return result;
      });
      for (const arn of (definitions?.taskDefinitionArns || []).filter(isRunDefinition)) arns.add(arn);
      if (arns.size) break;
      if (i < 2) await pause(5000);
    }
  }
  evidence.cleanup.definitionArns = [...arns];
  evidence.cleanup.definitionRecovery = !evidence.registerAttempted ? "not-required" : arns.size ? "resolved" : "unresolved";
  if (evidence.cleanup.definitionRecovery === "unresolved") evidence.cleanup.errors.push("Diagnostic definition identity unresolved");
  save(config, evidence);
  for (const arn of arns) {
    const result = await attempt(() => aws("ecs", "deregister-task-definition", ["--task-definition", arn]));
    if (result?.taskDefinition?.taskDefinitionArn !== arn || result?.taskDefinition?.status !== "INACTIVE") evidence.cleanup.errors.push("Diagnostic definition deregistration unconfirmed");
  }
  evidence.cleanup.deregistered = (!evidence.registerAttempted || !!definitions && arns.size > 0) && evidence.cleanup.errors.length === 0;
  evidence.logs = [];
  for (const task of verified) {
    let token;
    for (let i = 0; i < 6; i++) {
      const page = await attempt(() => aws("logs", "get-log-events", ["--log-group-name", LOG,
        "--log-stream-name", `origin-tls-${config.run}/probe/${task.split("/").pop()}`, "--start-from-head",
        ...(token ? ["--next-token", token] : [])]));
      if (!page) break;
      evidence.logs.push(...(page.events || []));
      if (page.nextForwardToken === token) {
        if (verifiedLogs(evidence.logs, evidence.before?.enis?.map((e) => e.PrivateIpAddress) || [])) break;
        if (i === 5) break;
        await pause(5000);
        evidence.logs = [];
        token = undefined;
        continue;
      }
      token = page.nextForwardToken;
    }
  }
  const complete = verifiedLogs(evidence.logs, evidence.before?.enis?.map((e) => e.PrivateIpAddress) || []);
  evidence.success = evidence.success && complete && evidence.cleanup.stopped && evidence.cleanup.deregistered && evidence.cleanup.errors.length === 0;
  save(config, evidence);
  const archived = await archive(aws, config, "result.json", evidence);
  appendFileSync(process.env.GITHUB_STEP_SUMMARY, `Private origin TLS result: version ${archived.version}, SHA-256 ${archived.sha256}. Outcome: ${evidence.success ? "verified" : "inconclusive"}.\n`);
  requireThat(evidence.success, "Diagnostic incomplete, inconclusive or cleanup failed; inspect the private result before any new dispatch");
}

function verifiedLogs(events, targets) {
  const rows = events.map((e) => { try { return JSON.parse(e.message); } catch { return null; } }).filter(Boolean);
  if (!validTargets(targets) || !rows.some((r) => r.kind === "complete" && r.success === true) ||
    rows.some((r) => r.kind === "handshake" && !targets.includes(r.ip))) return false;
  return targets.every((ip) => {
    const handshakes = rows.filter((r) => r.kind === "handshake" && r.ip === ip);
    return canonical(handshakes.map((r) => r.cipher)) === canonical([GCM[0], GCM[1], TLS13, ...CBC, GCM[0], TLS13]) &&
      handshakes.every((r) => classify(r, r.cipher === TLS13 ? "TLSv1.3" : "TLSv1.2", r.cipher, CBC.includes(r.cipher)) ===
        (CBC.includes(r.cipher) ? "refused" : "trusted"));
  });
}

if (require.main === module) {
  process.umask(0o077);
  Promise.resolve().then(async () => {
    const config = configuration(process.env);
    const source = spawnSync("git", ["rev-parse", "HEAD"], { encoding: "utf8" });
    requireThat(source.status === 0 && source.stdout.trim() === config.source, "Checkout must match main workflow source");
    if (process.argv[2] === "session-policy") {
      for (const phase of ["probe", "cleanup"]) appendFileSync(process.env.GITHUB_OUTPUT, `${phase}_policy=${canonical(sessionPolicy(config, phase))}\n`);
    } else if (process.argv[2] === "probe") await execute(awsCLI, config);
    else if (process.argv[2] === "finish") await finish(awsCLI, config);
    else throw new Error("Unknown diagnostic command");
  }).catch((error) => { console.error(`Origin TLS diagnostic stopped: ${error.message}`); process.exitCode = 1; });
}
module.exports = { configuration, sessionPolicy, discover, definition, archive, execute, finish, verifiedLogs, IMAGE, CLUSTER, SERVICE, EXECUTION, SG, ALB_SG, VPC, ECS, LOG };
