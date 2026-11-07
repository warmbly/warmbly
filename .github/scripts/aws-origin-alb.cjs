const { createHash } = require("node:crypto");
const { spawnSync } = require("node:child_process");
const { appendFileSync, mkdirSync, readFileSync, writeFileSync } = require("node:fs");
const { join } = require("node:path");
const { isDeepStrictEqual } = require("node:util");

const ACCOUNT = "925323749693";
const REGION = "us-east-1";
const BUCKET = `warmbly-terraform-use1-${ACCOUNT}`;
const ROLE = `arn:aws:iam::${ACCOUNT}:role/warmbly-production-terraform-apply`;
const LB = `arn:aws:elasticloadbalancing:${REGION}:${ACCOUNT}:loadbalancer/app/warmbly-production-ingress/19d5c82c1651c871`;
const LISTENER = `arn:aws:elasticloadbalancing:${REGION}:${ACCOUNT}:listener/app/warmbly-production-ingress/19d5c82c1651c871/d0898c3f7d2223ea`;
const BASELINE = "ELBSecurityPolicy-TLS13-1-2-2021-06";
const DESIRED = "ELBSecurityPolicy-TLS13-1-2-Res-2021-06";
const PREFIX = "warmbly/production/diagnostics/origin-alb-policy";
const WORKFLOW = "warmbly/warmbly/.github/workflows/configure-origin-alb-aws.yml@refs/heads/main";
const hash = (bytes) => createHash("sha256").update(bytes).digest("hex");
const canonical = (value) => JSON.stringify(value, (_, v) => v && typeof v === "object" && !Array.isArray(v)
  ? Object.fromEntries(Object.keys(v).sort().map((key) => [key, v[key]])) : v);
const same = (a, b) => isDeepStrictEqual(a, b);
const requireThat = (condition, message) => { if (!condition) throw new Error(message); };
const planKey = (run) => `${PREFIX}/${run}/1/plan.json`;
function privateJSON(bytes) {
  try { return JSON.parse(bytes); } catch { throw new Error("Private input is not valid JSON"); }
}

function configuration(env) {
  requireThat(env.GITHUB_ACTIONS === "true" && env.GITHUB_REPOSITORY === "warmbly/warmbly" &&
    env.GITHUB_REF === "refs/heads/main" && env.GITHUB_EVENT_NAME === "workflow_dispatch" &&
    env.GITHUB_WORKFLOW_REF === WORKFLOW && env.GITHUB_RUN_ATTEMPT === "1" &&
    /^[1-9]\d*$/.test(env.GITHUB_RUN_ID || "") && /^[a-f0-9]{40}$/.test(env.GITHUB_SHA || ""),
  "A new main workflow dispatch is required; reruns are refused");
  requireThat(env.STATE_BUCKET === BUCKET && env.APPLY_ROLE === ROLE,
    "Configure AWS_TERRAFORM_STATE_BUCKET and AWS_TERRAFORM_APPLY_ROLE_ARN through the normal aws-infrastructure configuration gate; no role fallback is permitted");
  const mode = env.ORIGIN_ALB_MODE || "plan";
  requireThat(["plan", "apply", "rollback"].includes(mode), "Unknown origin ALB mode");
  const run = env.ORIGIN_ALB_PLAN_RUN_ID || "";
  const version = env.ORIGIN_ALB_PLAN_VERSION || "";
  const digest = env.ORIGIN_ALB_PLAN_SHA256 || "";
  if (mode === "plan") requireThat(!run && !version && !digest, "Plan mode does not accept prior plan inputs");
  else requireThat(/^[1-9]\d*$/.test(run) && run !== env.GITHUB_RUN_ID &&
    /^[A-Za-z0-9_./+=-]{1,1024}$/.test(version) && version !== "null" && /^[a-f0-9]{64}$/.test(digest),
  "Apply and rollback require the exact prior plan run, version and SHA-256");
  requireThat(env.WORK && env.GITHUB_OUTPUT && env.GITHUB_STEP_SUMMARY, "Private work directory and workflow output paths are required");
  return { mode, run, version, digest, source: env.GITHUB_SHA, currentRun: env.GITHUB_RUN_ID, work: env.WORK };
}

function sessionPolicy(config) {
  const statements = [
    { Effect: "Allow", Action: ["elasticloadbalancing:Describe*", "ec2:DescribeSecurityGroups"], Resource: "*" },
    { Effect: "Allow", Action: "s3:PutObject", Resource: `arn:aws:s3:::${BUCKET}/${PREFIX}/${config.currentRun}/1/*`,
      Condition: { StringEquals: { "s3:x-amz-server-side-encryption": "AES256", "s3:ResourceAccount": ACCOUNT },
        Bool: { "aws:SecureTransport": "true" }, Null: { "s3:if-none-match": "false" } } },
  ];
  if (config.mode !== "plan") statements.push(
    { Effect: "Allow", Action: "s3:GetObjectVersion", Resource: `arn:aws:s3:::${BUCKET}/${planKey(config.run)}` },
    { Effect: "Allow", Action: "elasticloadbalancing:ModifyListener", Resource: LISTENER },
  );
  return { Version: "2012-10-17", Statement: statements };
}

function awsCLI(service, action, args = []) {
  const result = spawnSync("aws", [service, action, ...args, "--region", REGION, "--output", "json", "--no-cli-pager"],
    { encoding: "utf8", maxBuffer: 16 * 1024 * 1024, timeout: 60000,
      env: { ...process.env, AWS_PAGER: "", AWS_CLI_AUTO_PROMPT: "off", AWS_MAX_ATTEMPTS: "1" } });
  // Provider output stays private, including failures that may contain request data.
  if (result.error || result.status !== 0) throw new Error(`AWS ${service} ${action} failed; use the normal IAM/configuration gate if access is insufficient`);
  return privateJSON(result.stdout);
}

function sorted(rows, key) {
  requireThat(Array.isArray(rows) && rows.every((row) => typeof row[key] === "string") &&
    new Set(rows.map((row) => row[key])).size === rows.length, "Incomplete or duplicate ALB response");
  return rows.sort((a, b) => a[key].localeCompare(b[key]));
}

function sortedCertificates(rows) {
  requireThat(Array.isArray(rows) && rows.every((row) => typeof row.CertificateArn === "string" && typeof row.IsDefault === "boolean") &&
    new Set(rows.map((row) => canonical([row.CertificateArn, row.IsDefault]))).size === rows.length,
  "Incomplete or duplicate ALB certificate response");
  return rows.sort((a, b) => a.CertificateArn.localeCompare(b.CertificateArn) || Number(a.IsDefault) - Number(b.IsDefault));
}

async function collect(aws) {
  const lb = await aws("elbv2", "describe-load-balancers", ["--load-balancer-arns", LB]);
  requireThat(lb.LoadBalancers?.length === 1 && lb.LoadBalancers[0].LoadBalancerArn === LB &&
    lb.LoadBalancers[0].Type === "application" && lb.LoadBalancers[0].State?.Code === "active", "The exact origin ALB must be active");
  const listeners = sorted((await aws("elbv2", "describe-listeners", ["--load-balancer-arn", LB])).Listeners, "ListenerArn");
  requireThat(listeners.some((l) => l.ListenerArn === LISTENER), "The bound HTTPS listener is required");
  const listenerDetails = [];
  for (const listener of listeners) {
    const details = { arn: listener.ListenerArn,
      attributes: sorted((await aws("elbv2", "describe-listener-attributes", ["--listener-arn", listener.ListenerArn])).Attributes, "Key"),
      rules: sorted((await aws("elbv2", "describe-rules", ["--listener-arn", listener.ListenerArn])).Rules, "RuleArn") };
    if (listener.Protocol === "HTTPS") details.certificates = sortedCertificates((await aws("elbv2", "describe-listener-certificates", ["--listener-arn", listener.ListenerArn])).Certificates);
    listenerDetails.push(details);
  }
  const targetGroups = sorted((await aws("elbv2", "describe-target-groups", ["--load-balancer-arn", LB])).TargetGroups, "TargetGroupArn");
  const targetAttributes = [];
  for (const tg of targetGroups) targetAttributes.push({ arn: tg.TargetGroupArn,
    attributes: sorted((await aws("elbv2", "describe-target-group-attributes", ["--target-group-arn", tg.TargetGroupArn])).Attributes, "Key") });
  const groups = lb.LoadBalancers[0].SecurityGroups;
  requireThat(Array.isArray(groups) && groups.length > 0, "ALB security groups are required");
  return { loadBalancers: lb.LoadBalancers, listeners, listenerDetails, targetGroups, targetAttributes,
    loadBalancerAttributes: sorted((await aws("elbv2", "describe-load-balancer-attributes", ["--load-balancer-arn", LB])).Attributes, "Key"),
    securityGroups: sorted((await aws("ec2", "describe-security-groups", ["--group-ids", ...groups.slice().sort()])).SecurityGroups, "GroupId") };
}

function assertSnapshot(snapshot, policy) {
  requireThat(Array.isArray(snapshot?.listeners), "The complete listener inventory is required");
  const listener = snapshot?.listeners?.filter((l) => l.ListenerArn === LISTENER);
  requireThat(listener?.length === 1 && listener[0].LoadBalancerArn === LB && listener[0].Port === 443 &&
    listener[0].Protocol === "HTTPS" && listener[0].SslPolicy === policy && listener[0].Certificates?.length > 0,
  "Exact listener identity, HTTPS port, certificates and policy baseline are required");
  requireThat(snapshot.loadBalancers?.length === 1 && snapshot.loadBalancers[0].LoadBalancerArn === LB &&
    Array.isArray(snapshot.listenerDetails) && Array.isArray(snapshot.targetGroups) &&
    Array.isArray(snapshot.targetAttributes) && Array.isArray(snapshot.loadBalancerAttributes) && Array.isArray(snapshot.securityGroups),
  "A complete preserved origin snapshot is required");
  const ids = (rows, key) => rows.map((row) => row[key]).sort();
  requireThat(same(ids(snapshot.listeners, "ListenerArn"), ids(snapshot.listenerDetails, "arn")) &&
    snapshot.listenerDetails.every((d) => Array.isArray(d.attributes) && Array.isArray(d.rules)) &&
    snapshot.listenerDetails.find((d) => d.arn === LISTENER).certificates?.some((c) => c.IsDefault === true) &&
    same(ids(snapshot.targetGroups, "TargetGroupArn"), ids(snapshot.targetAttributes, "arn")) &&
    snapshot.targetAttributes.every((d) => Array.isArray(d.attributes)) &&
    snapshot.securityGroups.length > 0 && same(snapshot.loadBalancers[0].SecurityGroups.slice().sort(), ids(snapshot.securityGroups, "GroupId")),
  "Every listener, certificate inventory, target group attribute set and security group must be preserved");
}

function makePlan(snapshot, config, now = new Date()) {
  assertSnapshot(snapshot, BASELINE);
  const after = structuredClone(snapshot);
  after.listeners.find((l) => l.ListenerArn === LISTENER).SslPolicy = DESIRED;
  return { schemaVersion: 1, repository: "warmbly/warmbly", workflow: WORKFLOW, source: config.source,
    run: config.currentRun, createdAt: now.toISOString(), account: ACCOUNT, region: REGION,
    changes: [{ address: "aws_lb_listener.https", action: "update", listener: LISTENER, field: "SslPolicy", before: BASELINE, after: DESIRED }],
    before: snapshot, after };
}

function checkPlan(plan, config, now = new Date()) {
  requireThat(plan?.schemaVersion === 1 && plan.repository === "warmbly/warmbly" && plan.workflow === WORKFLOW &&
    plan.account === ACCOUNT && plan.region === REGION && plan.source === config.source && plan.run === config.run,
  "Plan must bind this workflow source, account, region and prior plan run");
  const age = now.getTime() - Date.parse(plan.createdAt);
  requireThat(Number.isFinite(age) && age >= 0 && age <= 24 * 60 * 60 * 1000, "Plan must be at most 24 hours old; prepare a fresh plan");
  requireThat(same(plan, makePlan(plan.before, { source: config.source, currentRun: config.run }, new Date(plan.createdAt))),
    "The complete plan may change only the bound listener SslPolicy from the exact baseline to the restricted policy");
  assertSnapshot(plan.after, DESIRED);
}

async function health(aws, snapshot) {
  const routes = snapshot.listenerDetails.find((d) => d.arn === LISTENER).rules;
  const arns = new Set();
  for (const rule of routes) for (const action of rule.Actions || []) {
    if (action.TargetGroupArn) arns.add(action.TargetGroupArn);
    for (const tg of action.ForwardConfig?.TargetGroups || []) if ((tg.Weight ?? 1) > 0) arns.add(tg.TargetGroupArn);
  }
  requireThat(arns.size > 0, "HTTPS forwarding targets are required");
  const observations = [];
  for (const arn of [...arns].sort()) {
    const tg = snapshot.targetGroups.find((g) => g.TargetGroupArn === arn);
    requireThat(tg?.Protocol === "HTTPS", "Every active HTTPS route must retain HTTPS targets");
    const result = await aws("elbv2", "describe-target-health", ["--target-group-arn", arn]);
    requireThat(result.TargetHealthDescriptions?.length > 0 && result.TargetHealthDescriptions.every((t) => t.TargetHealth?.State === "healthy"),
      "Every routed HTTPS target must be healthy");
    observations.push({ arn, ...result });
  }
  return observations;
}

async function put(aws, config, name, value) {
  const bytes = canonical(value) + "\n";
  const path = join(config.work, name);
  writeFileSync(path, bytes, { mode: 0o600, flag: "wx" });
  const checksum = createHash("sha256").update(bytes).digest("base64");
  const metadata = await aws("s3api", "put-object", ["--bucket", BUCKET, "--key", `${PREFIX}/${config.currentRun}/1/${name}`,
    "--expected-bucket-owner", ACCOUNT, "--server-side-encryption", "AES256", "--if-none-match", "*",
    "--checksum-algorithm", "SHA256", "--checksum-sha256", checksum, "--body", path]);
  requireThat(metadata.ServerSideEncryption === "AES256" && metadata.ChecksumSHA256 === checksum &&
    typeof metadata.VersionId === "string" && metadata.VersionId.length > 0 && metadata.VersionId !== "null", "Encrypted versioned immutable evidence is required");
  return { sha256: hash(bytes), version: metadata.VersionId };
}

async function loadPlan(aws, config) {
  const path = join(config.work, "reviewed-plan.json");
  const metadata = await aws("s3api", "get-object", ["--bucket", BUCKET, "--key", planKey(config.run),
    "--version-id", config.version, "--expected-bucket-owner", ACCOUNT, path]);
  requireThat(metadata.VersionId === config.version && metadata.DeleteMarker !== true && metadata.ServerSideEncryption === "AES256",
    "The exact encrypted private plan version is required");
  const bytes = readFileSync(path);
  requireThat(hash(bytes) === config.digest, "Private plan SHA-256 differs from the reviewed digest");
  const plan = privateJSON(bytes);
  checkPlan(plan, config);
  return plan;
}

async function execute(aws, config, plan, evidence) {
  const rollback = config.mode === "rollback";
  const before = rollback ? plan.after : plan.before;
  const after = rollback ? plan.before : plan.after;
  const policy = rollback ? BASELINE : DESIRED;
  evidence.preflight = await collect(aws);
  requireThat(same(evidence.preflight, before), "Live origin baseline drifted; prepare a new reviewed plan");
  evidence.healthBefore = await health(aws, before);
  await put(aws, config, "intent.json", { mode: config.mode, source: config.source, planRun: config.run,
    planVersion: config.version, planSha256: config.digest, before, after, healthBefore: evidence.healthBefore });
  // Recheck after archiving, immediately before the sole configuration write.
  evidence.prewrite = await collect(aws);
  requireThat(same(evidence.prewrite, before), "Live origin changed before the write; a new plan is required");
  evidence.writeAttempted = true;
  evidence.response = await aws("elbv2", "modify-listener", ["--listener-arn", LISTENER, "--ssl-policy", policy]);
  requireThat(evidence.response.Listeners?.length === 1 &&
    same(evidence.response.Listeners[0], after.listeners.find((l) => l.ListenerArn === LISTENER)), "Listener write response must match the exact desired listener");
  evidence.readback = await collect(aws);
  requireThat(same(evidence.readback, after), "Origin readback differs from the exact desired snapshot; inspect private evidence before scoped rollback");
  evidence.healthAfter = await health(aws, after);
}

async function run(aws, config, report) {
  mkdirSync(config.work, { mode: 0o700 });
  const evidence = { mode: config.mode, source: config.source, run: config.currentRun, success: false, writeAttempted: false,
    reviewedPlan: { run: config.run, version: config.version, sha256: config.digest } };
  let message;
  let failure;
  try {
    const identity = await aws("sts", "get-caller-identity");
    requireThat(identity.Account === ACCOUNT && /^arn:aws:sts::925323749693:assumed-role\/warmbly-production-terraform-apply\/[^/]+$/.test(identity.Arn || ""),
      "The existing ordinary Terraform apply role is required");
    evidence.identity = identity;
    if (config.mode === "plan") {
      evidence.readback = await collect(aws);
      const plan = makePlan(evidence.readback, config);
      evidence.health = await health(aws, evidence.readback);
      const record = await put(aws, config, "plan.json", plan);
      message = `Plan only: one in-place origin listener SslPolicy update.\n\nPlan run: ${config.currentRun}\n\nPlan version: ${record.version}\n\nComplete plan SHA-256: ${record.sha256}\n\nReview the full private plan before a separate apply dispatch. No configuration write occurred.`;
    } else {
      const plan = await loadPlan(aws, config);
      await execute(aws, config, plan, evidence);
      message = `Origin policy ${config.mode} completed. Exact listener, certificates, attributes, rules, target groups and security groups read back unchanged apart from SslPolicy; routed HTTPS targets are healthy. Retain separate authorized origin handshake evidence.`;
    }
    evidence.success = true;
  } catch (error) {
    evidence.error = error.message;
    failure = error;
  }
  try {
    await put(aws, config, "result.json", evidence);
  } catch {
    const archiveFailure = `Result evidence archive failed; writeAttempted=${evidence.writeAttempted}`;
    if (!failure) throw new Error(archiveFailure);
    console.error(archiveFailure);
  }
  if (failure) throw failure;
  report(message);
}

if (require.main === module) {
  process.umask(0o077);
  Promise.resolve().then(async () => {
    const config = configuration(process.env);
    const source = spawnSync("git", ["rev-parse", "HEAD"], { encoding: "utf8" });
    requireThat(source.status === 0 && source.stdout.trim() === config.source, "Checkout must match the main workflow source");
    if (process.argv[2] === "session-policy") appendFileSync(process.env.GITHUB_OUTPUT, `policy=${canonical(sessionPolicy(config))}\n`);
    else {
      requireThat(process.argv[2] === "configure", "Unknown origin ALB command");
      await run(awsCLI, config, (message) => {
        console.log(message);
        appendFileSync(process.env.GITHUB_STEP_SUMMARY, `${message}\n`);
      });
    }
  }).catch((error) => { console.error(`Origin ALB configuration stopped: ${error.message}`); process.exitCode = 1; });
}

module.exports = { ACCOUNT, BUCKET, ROLE, LB, LISTENER, BASELINE, DESIRED, PREFIX, WORKFLOW,
  configuration, sessionPolicy, collect, makePlan, checkPlan, health, put, execute, run, canonical, hash };
