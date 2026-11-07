const { test } = require("node:test");
const assert = require("node:assert/strict");
const { mkdtempSync, readFileSync, writeFileSync, rmSync } = require("node:fs");
const { tmpdir } = require("node:os");
const { join } = require("node:path");
const { ACCOUNT, BUCKET, ROLE, LB, LISTENER, BASELINE, DESIRED, PREFIX, WORKFLOW,
  configuration, sessionPolicy, collect, makePlan, checkPlan, execute, run, canonical, hash } = require("./aws-origin-alb.cjs");

const SOURCE = "a".repeat(40);
const TG = `arn:aws:elasticloadbalancing:us-east-1:${ACCOUNT}:targetgroup/fixture/1234`;
const CERT = `arn:aws:acm:us-east-1:${ACCOUNT}:certificate/fixture`;

function fixture(t) {
  const dir = mkdtempSync(join(tmpdir(), "origin-alb-"));
  t.after(() => rmSync(dir, { force: true, recursive: true }));
  const config = { mode: "plan", source: SOURCE, currentRun: "100", work: join(dir, "plan") };
  const listener = { ListenerArn: LISTENER, LoadBalancerArn: LB, Protocol: "HTTPS", Port: 443,
    SslPolicy: BASELINE, Certificates: [{ CertificateArn: CERT }],
    DefaultActions: [{ Type: "forward", TargetGroupArn: TG }], MutualAuthentication: { Mode: "off" } };
  const live = {
    loadBalancers: [{ LoadBalancerArn: LB, Type: "application", State: { Code: "active" }, SecurityGroups: ["sg-fixture"], DNSName: "fixture.invalid" }],
    listeners: [listener, { ListenerArn: LISTENER + "-http", LoadBalancerArn: LB, Protocol: "HTTP", Port: 80, DefaultActions: [{ Type: "redirect" }] }],
    listenerDetails: [{ arn: LISTENER, attributes: [{ Key: "fixture", Value: "true" }],
      certificates: [{ CertificateArn: CERT, IsDefault: true }],
      rules: [{ RuleArn: LISTENER + "/rule/fixture", IsDefault: true, Actions: listener.DefaultActions }] },
    { arn: LISTENER + "-http", attributes: [], rules: [] }],
    targetGroups: [{ TargetGroupArn: TG, Protocol: "HTTPS", Port: 8443 }],
    targetAttributes: [{ arn: TG, attributes: [{ Key: "deregistration_delay.timeout_seconds", Value: "30" }] }],
    loadBalancerAttributes: [{ Key: "deletion_protection.enabled", Value: "true" }],
    securityGroups: [{ GroupId: "sg-fixture", IpPermissions: [{ IpProtocol: "tcp", FromPort: 443, ToPort: 443 }] }],
  };
  const calls = [];
  const objects = new Map();
  let unhealthy = false;
  const aws = async (service, action, args = []) => {
    calls.push({ service, action, args: structuredClone(args) });
    const arg = (name) => args[args.indexOf(name) + 1];
    switch (`${service}:${action}`) {
    case "sts:get-caller-identity": return { Account: ACCOUNT, Arn: `arn:aws:sts::${ACCOUNT}:assumed-role/warmbly-production-terraform-apply/fixture` };
    case "elbv2:describe-load-balancers": return { LoadBalancers: structuredClone(live.loadBalancers) };
    case "elbv2:describe-listeners": return { Listeners: structuredClone(live.listeners) };
    case "elbv2:describe-listener-attributes": return { Attributes: structuredClone(live.listenerDetails.find((d) => d.arn === arg("--listener-arn")).attributes) };
    case "elbv2:describe-listener-certificates": return { Certificates: structuredClone(live.listenerDetails.find((d) => d.arn === arg("--listener-arn")).certificates) };
    case "elbv2:describe-rules": return { Rules: structuredClone(live.listenerDetails.find((d) => d.arn === arg("--listener-arn")).rules) };
    case "elbv2:describe-target-groups": return { TargetGroups: structuredClone(live.targetGroups) };
    case "elbv2:describe-target-group-attributes": return { Attributes: structuredClone(live.targetAttributes.find((d) => d.arn === arg("--target-group-arn")).attributes) };
    case "elbv2:describe-load-balancer-attributes": return { Attributes: structuredClone(live.loadBalancerAttributes) };
    case "ec2:describe-security-groups": return { SecurityGroups: structuredClone(live.securityGroups) };
    case "elbv2:describe-target-health": return { TargetHealthDescriptions: [{ Target: { Id: "192.0.2.1", Port: 8443 }, TargetHealth: { State: unhealthy ? "unhealthy" : "healthy" } }] };
    case "elbv2:modify-listener": {
      assert.deepEqual(args.slice(0, 3), ["--listener-arn", LISTENER, "--ssl-policy"]);
      assert.equal(args.length, 4);
      live.listeners[0].SslPolicy = arg("--ssl-policy");
      return { Listeners: [structuredClone(live.listeners[0])] };
    }
    case "s3api:put-object": {
      const key = arg("--key");
      assert.equal(arg("--bucket"), BUCKET);
      assert.equal(arg("--expected-bucket-owner"), ACCOUNT);
      assert.equal(arg("--if-none-match"), "*");
      assert.equal(arg("--server-side-encryption"), "AES256");
      assert.ok(!objects.has(key), "immutable evidence cannot be overwritten");
      const bytes = readFileSync(arg("--body"));
      const metadata = { VersionId: `version-${objects.size}`, ChecksumSHA256: Buffer.from(hash(bytes), "hex").toString("base64"), ServerSideEncryption: "AES256" };
      assert.equal(arg("--checksum-sha256"), metadata.ChecksumSHA256);
      objects.set(key, { bytes, metadata });
      return metadata;
    }
    case "s3api:get-object": {
      assert.equal(arg("--bucket"), BUCKET);
      assert.equal(arg("--expected-bucket-owner"), ACCOUNT);
      const object = objects.get(arg("--key"));
      assert.equal(arg("--version-id"), object.metadata.VersionId);
      writeFileSync(args.at(-1), object.bytes);
      return object.metadata;
    }
    default: throw new Error(`Unexpected API ${service}:${action}`);
    }
  };
  const writeCalls = () => calls.filter((c) => c.action === "modify-listener");
  const reports = [];
  const plan = async () => {
    await run(aws, config, (s) => {
      assert.ok(objects.has(`${PREFIX}/100/1/result.json`));
      reports.push(s);
    });
    const object = objects.get(`${PREFIX}/100/1/plan.json`);
    return { mode: "apply", source: SOURCE, currentRun: "200", run: "100", version: object.metadata.VersionId,
      digest: hash(object.bytes), work: join(dir, "apply") };
  };
  return { config, live, calls, objects, aws, writeCalls, reports, plan, dir, setUnhealthy: () => { unhealthy = true; } };
}

function env() {
  return { GITHUB_ACTIONS: "true", GITHUB_REPOSITORY: "warmbly/warmbly", GITHUB_REF: "refs/heads/main", GITHUB_EVENT_NAME: "workflow_dispatch",
    GITHUB_WORKFLOW_REF: WORKFLOW, GITHUB_RUN_ATTEMPT: "1", GITHUB_RUN_ID: "100", GITHUB_SHA: SOURCE,
    STATE_BUCKET: BUCKET, APPLY_ROLE: ROLE, WORK: "/tmp/fixture", GITHUB_OUTPUT: "/tmp/output", GITHUB_STEP_SUMMARY: "/tmp/summary" };
}

test("default plan is main-only, refuses reruns, invalid scope and missing ordinary environment configuration", () => {
  assert.equal(configuration(env()).mode, "plan");
  for (const changes of [{ GITHUB_REF: "refs/heads/feature" }, { GITHUB_RUN_ATTEMPT: "2" }, { GITHUB_EVENT_NAME: "push" },
    { GITHUB_WORKFLOW_REF: "other" }, { APPLY_ROLE: "" }, { STATE_BUCKET: "" }, { STATE_BUCKET: "other" },
    { ORIGIN_ALB_MODE: "apply" }, { ORIGIN_ALB_MODE: "recovery" }, { ORIGIN_ALB_PLAN_RUN_ID: "99" }]) {
    assert.throws(() => configuration({ ...env(), ...changes }));
  }
  const apply = { ...env(), ORIGIN_ALB_MODE: "apply", ORIGIN_ALB_PLAN_RUN_ID: "99", ORIGIN_ALB_PLAN_VERSION: "version-1", ORIGIN_ALB_PLAN_SHA256: "f".repeat(64) };
  assert.equal(configuration(apply).run, "99");
  for (const changes of [{ ORIGIN_ALB_PLAN_RUN_ID: "100" }, { ORIGIN_ALB_PLAN_RUN_ID: "../other" },
    { ORIGIN_ALB_PLAN_VERSION: "null" }, { ORIGIN_ALB_PLAN_VERSION: "invalid\nvalue" }, { ORIGIN_ALB_PLAN_SHA256: "bad" }]) {
    assert.throws(() => configuration({ ...apply, ...changes }));
  }
});

test("session permissions intersect the existing role: no listener writes in plan, exact listener in apply, conditional private writes only", () => {
  const config = configuration(env());
  const plan = sessionPolicy(config);
  assert.ok(!canonical(plan).includes("ModifyListener"));
  assert.ok(!canonical(plan).includes("GetObject"));
  const apply = sessionPolicy({ ...config, mode: "apply", run: "99" });
  assert.deepEqual(apply.Statement.find((s) => s.Action === "elasticloadbalancing:ModifyListener"),
    { Effect: "Allow", Action: "elasticloadbalancing:ModifyListener", Resource: LISTENER });
  const write = apply.Statement.find((s) => s.Action === "s3:PutObject");
  assert.equal(write.Resource, `arn:aws:s3:::${BUCKET}/${PREFIX}/100/1/*`);
  assert.equal(write.Condition.Null["s3:if-none-match"], "false");
  assert.equal(write.Condition.StringEquals["s3:x-amz-server-side-encryption"], "AES256");
  assert.equal(apply.Statement.find((s) => s.Action === "s3:GetObjectVersion").Resource, `arn:aws:s3:::${BUCKET}/${PREFIX}/99/1/plan.json`);
  const actions = apply.Statement.flatMap((s) => s.Action);
  assert.ok(!/secretsmanager|iam:|ecs:|DeleteObject|ModifyRule|Create/.test(actions.join(" ")));
});

test("dry plan archives the complete baseline and exact change without a configuration write", async (t) => {
  const f = fixture(t);
  const baseline = structuredClone(f.live);
  await f.plan();
  assert.deepEqual(f.live, baseline);
  assert.equal(f.writeCalls().length, 0);
  const plan = JSON.parse(f.objects.get(`${PREFIX}/100/1/plan.json`).bytes);
  checkPlan(plan, { source: SOURCE, run: "100" });
  assert.equal(plan.before.listeners[0].SslPolicy, BASELINE);
  assert.equal(plan.after.listeners[0].SslPolicy, DESIRED);
  assert.ok(f.reports[0].includes("Complete plan SHA-256"));
  assert.ok(f.objects.has(`${PREFIX}/100/1/result.json`));
});

test("complete plan guard rejects unrelated changes, replacements, missing identity and baseline drift", async (t) => {
  const f = fixture(t);
  const plan = makePlan(await collect(f.aws), f.config);
  const config = { source: SOURCE, run: "100" };
  for (const mutate of [
    (p) => { p.changes.push({ address: "aws_ecs_service.backend", action: "update" }); },
    (p) => { p.changes[0].action = "replace"; },
    (p) => { p.after.listeners[0].Port = 8443; },
    (p) => { p.after.listeners[0].Certificates[0].CertificateArn = "other"; },
    (p) => { p.after.listeners[0].DefaultActions = []; },
    (p) => { p.after.listeners[0].MutualAuthentication.Mode = "verify"; },
    (p) => { p.after.listeners[1].Port = 81; },
    (p) => { p.after.listenerDetails[0].rules = []; },
    (p) => { p.after.targetGroups[0].Port = 80; },
    (p) => { p.after.securityGroups[0].IpPermissions = []; },
    (p) => { p.before.listeners[0].SslPolicy = DESIRED; },
    (p) => { p.before.listeners[0].ListenerArn += "other"; },
    (p) => { p.before.listeners[0].LoadBalancerArn += "other"; },
    (p) => { p.before.listenerDetails = []; },
    (p) => { p.before.listenerDetails[0].certificates = []; },
    (p) => { p.before.targetAttributes = []; },
    (p) => { p.before.securityGroups = []; },
    (p) => { p.after.extra = "unreviewed"; },
    (p) => { p.source = "b".repeat(40); },
    (p) => { p.createdAt = "2000-01-01T00:00:00Z"; },
    (p) => { p.createdAt = "2999-01-01T00:00:00Z"; },
  ]) {
    const invalid = structuredClone(plan);
    mutate(invalid);
    assert.throws(() => checkPlan(invalid, config));
  }
});

test("apply changes only ssl policy after immutable intent and verifies the entire origin configuration", async (t) => {
  const f = fixture(t);
  const config = await f.plan();
  const before = structuredClone(f.live);
  await run(f.aws, config, (s) => {
    assert.ok(f.objects.has(`${PREFIX}/200/1/result.json`));
    f.reports.push(s);
  });
  const expected = structuredClone(before);
  expected.listeners[0].SslPolicy = DESIRED;
  assert.deepEqual(f.live, expected);
  assert.deepEqual(f.writeCalls(), [{ service: "elbv2", action: "modify-listener", args: ["--listener-arn", LISTENER, "--ssl-policy", DESIRED] }]);
  const intentIndex = f.calls.findIndex((c) => c.action === "put-object" && c.args.includes(`${PREFIX}/200/1/intent.json`));
  const writeIndex = f.calls.findIndex((c) => c.action === "modify-listener");
  assert.ok(intentIndex >= 0 && intentIndex < writeIndex);
  assert.equal(JSON.parse(f.objects.get(`${PREFIX}/200/1/result.json`).bytes).success, true);
});

for (const mode of ["plan", "apply", "rollback"]) test(`${mode} completion requires a successful result archive`, async (t) => {
  const f = fixture(t);
  let config = f.config;
  if (mode !== "plan") config = await f.plan();
  if (mode === "rollback") {
    await run(f.aws, config, () => {});
    config = { ...config, mode, currentRun: "300", work: join(f.dir, "rollback") };
  }
  const resultKey = `${PREFIX}/${config.currentRun}/1/result.json`;
  const reports = [];
  const logs = [];
  let archivedEvidence;
  let reportsAtArchive;
  t.mock.method(console, "error", (...args) => logs.push(args));
  const aws = async (service, action, args) => {
    if (action === "put-object" && args.includes(resultKey)) {
      reportsAtArchive = [...reports];
      archivedEvidence = JSON.parse(readFileSync(args[args.indexOf("--body") + 1]));
      throw new Error("Private archive provider response\ncredential=fixture-secret");
    }
    return f.aws(service, action, args);
  };
  await assert.rejects(run(aws, config, (s) => reports.push(s)), {
    message: `Result evidence archive failed; writeAttempted=${mode !== "plan"}`,
  });
  assert.equal(archivedEvidence.success, true);
  assert.equal(archivedEvidence.writeAttempted, mode !== "plan");
  assert.deepEqual(reportsAtArchive, []);
  assert.deepEqual(reports, []);
  assert.deepEqual(logs, []);
  assert.ok(!f.objects.has(resultKey));
  assert.equal(f.live.listeners[0].SslPolicy, mode === "apply" ? DESIRED : BASELINE);
  assert.equal(f.writeCalls().length, mode === "plan" ? 0 : mode === "apply" ? 1 : 2);
});

for (const writeAttempted of [false, true]) test(`operation error survives result archive failure with writeAttempted=${writeAttempted}`, async (t) => {
  const f = fixture(t);
  const config = await f.plan();
  const resultKey = `${PREFIX}/200/1/result.json`;
  const operationError = new Error("Origin policy operation failed");
  const reports = [];
  const logs = [];
  let archivedEvidence;
  t.mock.method(console, "error", (...args) => logs.push(args));
  const aws = async (service, action, args) => {
    if (!writeAttempted && action === "get-caller-identity") throw operationError;
    if (action === "put-object" && args.includes(resultKey)) {
      archivedEvidence = JSON.parse(readFileSync(args[args.indexOf("--body") + 1]));
      throw new Error("Private archive provider response\ncredential=fixture-secret");
    }
    const response = await f.aws(service, action, args);
    if (action === "modify-listener") throw operationError;
    return response;
  };
  await assert.rejects(run(aws, config, (s) => reports.push(s)), (error) => error === operationError);
  assert.equal(archivedEvidence.success, false);
  assert.equal(archivedEvidence.error, operationError.message);
  assert.equal(archivedEvidence.writeAttempted, writeAttempted);
  assert.deepEqual(reports, []);
  assert.deepEqual(logs, [[`Result evidence archive failed; writeAttempted=${writeAttempted}`]]);
  assert.ok(!f.objects.has(resultKey));
  assert.equal(f.writeCalls().length, writeAttempted ? 1 : 0);
});

test("scoped rollback uses the same exact reviewed plan and restores only the baseline policy", async (t) => {
  const f = fixture(t);
  const config = await f.plan();
  const baseline = structuredClone(f.live);
  await run(f.aws, config, () => {});
  await run(f.aws, { ...config, mode: "rollback", currentRun: "300", work: join(f.dir, "rollback") }, () => {});
  assert.deepEqual(f.live, baseline);
  assert.deepEqual(f.writeCalls().map((c) => c.args), [["--listener-arn", LISTENER, "--ssl-policy", DESIRED], ["--listener-arn", LISTENER, "--ssl-policy", BASELINE]]);
});

for (const defaultFirst of [true, false]) test(`plan, apply and rollback preserve default and SNI certificate entries with default first: ${defaultFirst}`, async (t) => {
  const f = fixture(t);
  const certificates = [{ CertificateArn: CERT, IsDefault: false }, { CertificateArn: CERT, IsDefault: true }];
  const inventory = f.live.listenerDetails.find((d) => d.arn === LISTENER);
  inventory.certificates = structuredClone(certificates);
  if (defaultFirst) inventory.certificates.reverse();
  const baseline = structuredClone(f.live);
  const config = await f.plan();
  assert.deepEqual(f.live, baseline);
  assert.equal(f.writeCalls().length, 0);
  const plan = JSON.parse(f.objects.get(`${PREFIX}/100/1/plan.json`).bytes);
  checkPlan(plan, { source: SOURCE, run: "100" });
  for (const snapshot of [plan.before, plan.after]) {
    assert.deepEqual(snapshot.listenerDetails.find((d) => d.arn === LISTENER).certificates, certificates);
  }
  for (const [mode, currentRun, policy] of [["apply", "200", DESIRED], ["rollback", "300", BASELINE]]) {
    inventory.certificates.reverse();
    const expected = structuredClone(f.live);
    expected.listeners[0].SslPolicy = policy;
    await run(f.aws, { ...config, mode, currentRun, work: join(f.dir, mode) }, () => {});
    assert.deepEqual(f.live, expected);
    const result = JSON.parse(f.objects.get(`${PREFIX}/${currentRun}/1/result.json`).bytes);
    assert.equal(result.success, true);
    assert.deepEqual(result.readback.listenerDetails.find((d) => d.arn === LISTENER).certificates, certificates);
  }
  assert.deepEqual(f.live, baseline);
  assert.deepEqual(f.writeCalls().map((c) => c.args), [["--listener-arn", LISTENER, "--ssl-policy", DESIRED], ["--listener-arn", LISTENER, "--ssl-policy", BASELINE]]);
});

test("certificate inventory requires distinct ARN and default-status pairs with boolean status", async (t) => {
  for (const status of [true, false, undefined, "true"]) {
    const f = fixture(t);
    f.live.listenerDetails[0].certificates = [
      { CertificateArn: CERT, IsDefault: status }, { CertificateArn: CERT, IsDefault: status },
    ];
    await assert.rejects(collect(f.aws), /Incomplete or duplicate ALB certificate response/);
    assert.equal(f.writeCalls().length, 0);
  }
});

test("certificate inventory rejects a single certificate with non-boolean default status", async (t) => {
  for (const status of [undefined, "true"]) {
    const f = fixture(t);
    f.live.listenerDetails[0].certificates = [{ CertificateArn: CERT, IsDefault: status }];
    await assert.rejects(collect(f.aws), /Incomplete or duplicate ALB certificate response/);
    assert.equal(f.writeCalls().length, 0);
  }
});

for (const [label, drift] of [
  ["policy", (f) => { f.live.listeners[0].SslPolicy = DESIRED; }],
  ["certificate", (f) => { f.live.listeners[0].Certificates = []; }],
  ["HTTP listener", (f) => { f.live.listeners[1].Port = 81; }],
  ["rule", (f) => { f.live.listenerDetails[0].rules[0].Actions = []; }],
  ["target group", (f) => { f.live.targetGroups[0].Port = 80; }],
  ["security group", (f) => { f.live.securityGroups[0].IpPermissions = []; }],
  ["listener attribute", (f) => { f.live.listenerDetails[0].attributes[0].Value = "false"; }],
]) test(`apply refuses ${label} baseline drift before mutation`, async (t) => {
  const f = fixture(t);
  const config = await f.plan();
  drift(f);
  await assert.rejects(run(f.aws, config, () => {}), /baseline drifted/);
  assert.equal(f.writeCalls().length, 0);
  assert.equal(JSON.parse(f.objects.get(`${PREFIX}/200/1/result.json`).bytes).success, false);
});

test("drift immediately after intent archival and rollback drift both refuse mutation", async (t) => {
  const f = fixture(t);
  const config = await f.plan();
  const plan = JSON.parse(f.objects.get(`${PREFIX}/100/1/plan.json`).bytes);
  const evidence = {};
  const aws = async (service, action, args) => {
    const out = await f.aws(service, action, args);
    if (action === "put-object") f.live.listeners[0].Port = 8443;
    return out;
  };
  // execute writes its intent into an existing private runner directory.
  await assert.rejects(execute(aws, { ...config, work: f.dir }, plan, evidence), /changed before the write/);
  assert.equal(f.writeCalls().length, 0);
  f.live.listeners[0].SslPolicy = DESIRED;
  await assert.rejects(run(f.aws, { ...config, mode: "rollback", currentRun: "300", work: join(f.dir, "rollback") }, () => {}), /baseline drifted/);
  assert.equal(f.writeCalls().length, 0);
});

test("altered plan bytes, stale source, unhealthy targets and failed immutable archive refuse writes", async (t) => {
  for (const variant of ["digest", "source", "health", "archive", "versioning", "identity"]) {
    const f = fixture(t);
    const config = await f.plan();
    if (variant === "digest") config.digest = "f".repeat(64);
    if (variant === "source") config.source = "b".repeat(40);
    if (variant === "health") f.setUnhealthy();
    const aws = async (service, action, args) => {
      if (variant === "archive" && action === "put-object") throw new Error("Evidence permission denied");
      const response = await f.aws(service, action, args);
      if (variant === "versioning" && action === "put-object") delete response.VersionId;
      if (variant === "identity" && action === "get-caller-identity") response.Account = "000000000000";
      return response;
    };
    await assert.rejects(run(aws, config, () => {}));
    assert.equal(f.writeCalls().length, 0);
  }
});

test("failed postwrite verification records attempted write and does not automatically overwrite concurrent changes", async (t) => {
  const f = fixture(t);
  const config = await f.plan();
  const aws = async (service, action, args) => {
    const out = await f.aws(service, action, args);
    if (action === "modify-listener") f.live.listeners[0].Port = 8443;
    return out;
  };
  await assert.rejects(run(aws, config, () => {}), /readback differs/);
  assert.equal(f.writeCalls().length, 1);
  const result = JSON.parse(f.objects.get(`${PREFIX}/200/1/result.json`).bytes);
  assert.equal(result.writeAttempted, true);
  assert.equal(result.success, false);
});
