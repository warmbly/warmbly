const { test } = require("node:test");
const assert = require("node:assert/strict");
const { mkdtempSync, readFileSync, rmSync } = require("node:fs");
const { tmpdir } = require("node:os");
const { join } = require("node:path");
const { ACCOUNT, BUCKET, ROLE, LB, LISTENER, DESIRED } = require("./aws-origin-alb.cjs");
const { configuration, sessionPolicy, discover, definition, execute, finish, verifiedLogs, IMAGE, CLUSTER, SERVICE, EXECUTION, SG, ALB_SG, VPC, ECS } = require("./aws-origin-tls.cjs");
const { probe, CBC, TLS13 } = require("./origin-tls-client.cjs");

function logFixture() {
  const events = [];
  probe(["10.42.0.85", "10.42.1.12"], (args) => {
    if (args[0] === "version") return { status: 0, stdout: "OpenSSL fixture" };
    if (args[0] === "ciphers") return { status: 0, stdout: CBC.join(":") };
    const cipher = args.at(-1);
    return { command: ["openssl", ...args], ...(CBC.includes(cipher) ?
      { status: 1, stdout: "New, (NONE), Cipher is (NONE)", stderr: "sslv3 alert handshake failure" } :
      { status: 0, stdout: `-----BEGIN CERTIFICATE-----\nfixture\nNew, ${cipher === TLS13 ? "TLSv1.3" : "TLSv1.2"}, Cipher is ${cipher}\nVerify return code: 0 (ok)`, stderr: "" }) };
  }, (row) => events.push({ message: JSON.stringify(row) }));
  return events;
}

const env = () => ({ GITHUB_ACTIONS: "true", GITHUB_REPOSITORY: "warmbly/warmbly", GITHUB_REF: "refs/heads/main",
  GITHUB_EVENT_NAME: "workflow_dispatch", GITHUB_WORKFLOW_REF: "warmbly/warmbly/.github/workflows/probe-origin-tls-aws.yml@refs/heads/main",
  GITHUB_RUN_ATTEMPT: "1", GITHUB_RUN_ID: "123456", GITHUB_SHA: "a".repeat(40), STATE_BUCKET: BUCKET, APPLY_ROLE: ROLE,
  WORK: "/tmp/fixture", GITHUB_OUTPUT: "/tmp/output", GITHUB_STEP_SUMMARY: "/tmp/summary" });

function fixture(t, change = () => {}) {
  const work = mkdtempSync(join(tmpdir(), "origin-tls-test-"));
  t.after(() => rmSync(work, { recursive: true, force: true }));
  const config = configuration({ ...env(), WORK: work });
  const arn = `${ECS}:task-definition/${config.family}:1`;
  const task = `${ECS}:task/warmbly-production/${"1".repeat(32)}`;
  const calls = [];
  const zones = [{ ZoneName: "us-east-1a", SubnetId: "subnet-a" }, { ZoneName: "us-east-1b", SubnetId: "subnet-b" }];
  const network = { subnets: zones.map((z) => z.SubnetId), securityGroups: [SG], assignPublicIp: "ENABLED" };
  const state = { taskArn: task, clusterArn: CLUSTER, taskDefinitionArn: arn, startedBy: config.startedBy,
    launchType: "FARGATE", enableExecuteCommand: false, lastStatus: "STOPPED",
    containers: [{ name: "probe", exitCode: 0, imageDigest: IMAGE.split("@")[1] }] };
  const aws = async (service, action, args = []) => {
    calls.push({ service, action, args });
    let result;
    switch (action) {
      case "get-caller-identity": result = { Account: ACCOUNT, Arn: `arn:aws:sts::${ACCOUNT}:assumed-role/warmbly-production-terraform-apply/fixture` }; break;
      case "describe-load-balancers": result = { LoadBalancers: [{ LoadBalancerArn: LB, VpcId: VPC,
        Type: "application", IpAddressType: "ipv4", State: { Code: "active" }, SecurityGroups: [ALB_SG], AvailabilityZones: zones }] }; break;
      case "describe-listeners": result = { Listeners: [{ ListenerArn: LISTENER, LoadBalancerArn: LB, Protocol: "HTTPS", Port: 443, SslPolicy: DESIRED }] }; break;
      case "describe-network-interfaces": result = { NetworkInterfaces: zones.map((z, i) => ({ Description: "ELB app/warmbly-production-ingress/19d5c82c1651c871",
        AvailabilityZone: z.ZoneName, SubnetId: z.SubnetId, VpcId: VPC, Status: "in-use", RequesterManaged: true, OwnerId: ACCOUNT,
        PrivateIpAddress: i ? "10.42.1.12" : "10.42.0.85", Groups: [{ GroupId: ALB_SG }] })) }; break;
      case "describe-services": result = { services: [{ serviceArn: SERVICE, status: "ACTIVE", networkConfiguration: { awsvpcConfiguration: network } }] }; break;
      case "describe-subnets": result = { Subnets: zones.map((z) => ({ SubnetId: z.SubnetId, VpcId: VPC, State: "available" })) }; break;
      case "put-object": result = { ServerSideEncryption: "AES256", ChecksumSHA256: args[args.indexOf("--checksum-sha256") + 1], VersionId: "private-version" }; break;
      case "register-task-definition": result = { taskDefinition: { taskDefinitionArn: arn } }; break;
      case "run-task": result = { tasks: [state] }; break;
      case "describe-tasks": result = { tasks: [state] }; break;
      case "list-tasks": result = { taskArns: [task] }; break;
      case "stop-task": result = { task: state }; break;
      case "list-task-definitions": result = { taskDefinitionArns: [arn] }; break;
      case "deregister-task-definition": result = { taskDefinition: { taskDefinitionArn: arn, status: "INACTIVE" } }; break;
      case "get-log-events": result = { events: args.includes("--next-token") ? [] : logFixture(), nextForwardToken: "end" }; break;
      default: throw new Error(`Unexpected ${service} ${action}`);
    }
    await change(action, result, calls);
    return result;
  };
  return { config, aws, calls, arn, task, state, evidence: () => JSON.parse(readFileSync(join(work, "evidence.json"))) };
}

test("only fresh main dispatches under normal infrastructure configuration are admitted", () => {
  assert.equal(configuration(env()).run, "123456");
  for (const [key, value] of Object.entries({ GITHUB_ACTIONS: "false", GITHUB_REPOSITORY: "fork/warmbly", GITHUB_REF: "refs/heads/branch",
    GITHUB_EVENT_NAME: "pull_request", GITHUB_WORKFLOW_REF: "other", GITHUB_RUN_ATTEMPT: "2", GITHUB_RUN_ID: "../1",
    GITHUB_SHA: "bad", STATE_BUCKET: "other", APPLY_ROLE: "other", WORK: "", GITHUB_OUTPUT: "", GITHUB_STEP_SUMMARY: "" })) {
    assert.throws(() => configuration({ ...env(), [key]: value }), key);
  }
});

test("session policy admits only diagnostics, bound roles, cluster and encrypted conditional private archives", () => {
  const config = configuration(env());
  const policy = sessionPolicy(config);
  assert.ok(JSON.stringify(policy).length < 2048);
  const all = policy.Statement.flatMap((s) => s.Action);
  for (const forbidden of ["ecs:UpdateService", "ecs:ExecuteCommand", "secretsmanager:GetSecretValue", "ssm:GetParameters",
    "iam:PutRolePolicy", "elasticloadbalancing:ModifyListener", "ec2:AuthorizeSecurityGroupIngress"]) assert.ok(!all.includes(forbidden));
  assert.deepEqual(policy.Statement.find((s) => s.Action === "iam:PassRole").Resource, EXECUTION);
  assert.equal(policy.Statement.find((s) => s.Action === "ecs:RunTask").Condition.ArnEquals["ecs:cluster"], CLUSTER);
  const put = policy.Statement.find((s) => s.Action === "s3:PutObject");
  assert.ok(put.Resource.endsWith(`/origin-tls/${config.run}/1/*`));
  assert.equal(put.Condition.StringEquals["s3:x-amz-server-side-encryption"], "AES256");
  assert.equal(put.Condition.Null["s3:if-none-match"], "false");
});

test("fresh definition contains only the official digest, hard-bounded TLS client and existing execution/log facilities", () => {
  const task = definition(configuration(env()), ["10.42.0.85", "10.42.1.12"]);
  assert.deepEqual(Object.keys(task).sort(), ["family", "networkMode", "requiresCompatibilities", "cpu", "memory", "executionRoleArn", "runtimePlatform", "containerDefinitions"].sort());
  assert.equal(task.executionRoleArn, EXECUTION);
  assert.equal(task.runtimePlatform.cpuArchitecture, "ARM64");
  const c = task.containerDefinitions[0];
  assert.match(c.image, /^docker\.io\/library\/node@sha256:[a-f0-9]{64}$/);
  assert.equal(c.secrets, undefined);
  assert.equal(c.repositoryCredentials, undefined);
  assert.equal(c.portMappings, undefined);
  assert.equal(c.mountPoints, undefined);
  assert.equal(c.readonlyRootFilesystem, true);
  assert.deepEqual(c.entryPoint, ["timeout", "-s", "KILL", "210", "node"]);
  assert.deepEqual(c.environment.map((e) => e.name), ["ORIGIN_TLS_TASK", "ORIGIN_TLS_TARGETS"]);
  assert.equal(c.logConfiguration.options["awslogs-create-group"], undefined);
});

test("only the exact active ALB, restricted listener, managed private ENIs and existing task network are accepted", async (t) => {
  const changes = [
    ["describe-load-balancers", (r) => { r.LoadBalancers[0].VpcId = "vpc-other"; }],
    ["describe-listeners", (r) => { r.Listeners[0].SslPolicy = "other"; }],
    ["describe-network-interfaces", (r) => { r.NetworkInterfaces.pop(); }],
    ["describe-network-interfaces", (r) => { r.NetworkInterfaces[0].Description = "another ALB"; }],
    ["describe-network-interfaces", (r) => { r.NetworkInterfaces[0].Groups[0].GroupId = SG; }],
    ["describe-network-interfaces", (r) => { r.NetworkInterfaces[0].RequesterManaged = false; }],
    ["describe-services", (r) => { r.services[0].networkConfiguration.awsvpcConfiguration.securityGroups = ["sg-worker"]; }],
    ["describe-subnets", (r) => { r.Subnets[0].VpcId = "vpc-other"; }],
  ];
  for (const [target, mutate] of changes) {
    const f = fixture(t, (action, result) => { if (action === target) mutate(result); });
    await assert.rejects(discover(f.aws));
    assert.ok(!f.calls.some((c) => c.action === "run-task"));
  }
});

test("archive requires encryption, checksum and a real version before admitting a task", async (t) => {
  for (const mutate of [(r) => { r.VersionId = "null"; }, (r) => { r.ChecksumSHA256 = "bad"; }, (r) => { r.ServerSideEncryption = ""; }]) {
    const f = fixture(t, (action, result) => { if (action === "put-object") mutate(result); });
    await assert.rejects(execute(f.aws, f.config, async () => {}), /versioned/);
    assert.ok(!f.calls.some((c) => c.action === "register-task-definition"));
  }
});

test("successful lifecycle archives intent first, launches only one secretless task, stops/deregisters and retains private logs", async (t) => {
  const f = fixture(t);
  const oldSummary = process.env.GITHUB_STEP_SUMMARY;
  process.env.GITHUB_STEP_SUMMARY = join(f.config.work, "summary");
  t.after(() => { if (oldSummary === undefined) delete process.env.GITHUB_STEP_SUMMARY; else process.env.GITHUB_STEP_SUMMARY = oldSummary; });
  await execute(f.aws, f.config, async () => {});
  await finish(f.aws, f.config, async () => {});
  assert.equal(f.evidence().success, true);
  const actions = f.calls.map((c) => c.action);
  assert.ok(actions.indexOf("put-object") < actions.indexOf("register-task-definition"));
  assert.equal(actions.filter((a) => a === "run-task").length, 1);
  const request = JSON.parse(f.calls.find((c) => c.action === "run-task").args[1]);
  assert.equal(request.taskDefinition, f.arn);
  assert.equal(request.enableExecuteCommand, false);
  assert.equal(request.clientToken, f.config.startedBy);
  assert.equal(request.overrides, undefined);
  assert.deepEqual(request.networkConfiguration.awsvpcConfiguration.securityGroups, [SG]);
  assert.ok(actions.indexOf("stop-task") < actions.indexOf("deregister-task-definition"));
  assert.equal(f.evidence().cleanup.stopped, true);
  assert.equal(f.evidence().cleanup.deregistered, true);
  assert.ok(f.evidence().logs.length > 0);
  assert.ok(!readFileSync(process.env.GITHUB_STEP_SUMMARY, "utf8").includes("10.42."));
});

for (const failure of ["register-task-definition", "run-task", "describe-tasks"]) {
  test(`lost ${failure} response still permits diagnostic-only recovery and private failure evidence`, async (t) => {
    let lost = false;
    const f = fixture(t, (action) => { if (action === failure && !lost) { lost = true; throw new Error("Fixture lost response"); } });
    const oldSummary = process.env.GITHUB_STEP_SUMMARY;
    process.env.GITHUB_STEP_SUMMARY = join(f.config.work, "summary");
    t.after(() => { if (oldSummary === undefined) delete process.env.GITHUB_STEP_SUMMARY; else process.env.GITHUB_STEP_SUMMARY = oldSummary; });
    await assert.rejects(execute(f.aws, f.config, async () => {}), /lost response/);
    await assert.rejects(finish(f.aws, f.config, async () => {}), /incomplete/);
    assert.ok(f.calls.some((c) => c.action === "deregister-task-definition"));
    if (failure !== "register-task-definition") assert.ok(f.calls.some((c) => c.action === "stop-task"));
    assert.equal(f.evidence().success, false);
    assert.equal(f.calls.filter((c) => c.action === "put-object").length, 2);
    assert.equal(f.calls.filter((c) => c.action === "run-task").length, failure === "register-task-definition" ? 0 : 1);
  });
}

test("network drift after registration prevents launch and remains eligible for cleanup", async (t) => {
  let reads = 0;
  const f = fixture(t, (action, result) => { if (action === "describe-network-interfaces" && ++reads > 1) result.NetworkInterfaces[0].PrivateIpAddress = "10.42.0.86"; });
  await assert.rejects(execute(f.aws, f.config, async () => {}), /changed before launch/);
  assert.ok(!f.calls.some((c) => c.action === "run-task"));
  assert.equal(f.evidence().registerAttempted, true);
});

test("bad image identity and nonzero exit never establish proof", async (t) => {
  for (const mutate of [(r) => { r.tasks[0].containers[0].exitCode = 1; }, (r) => { r.tasks[0].containers[0].imageDigest = "sha256:other"; }]) {
    const f = fixture(t, (action, result) => { if (action === "describe-tasks") mutate(result); });
    await assert.rejects(execute(f.aws, f.config, async () => {}), /inconclusive/);
    assert.equal(f.evidence().success, false);
  }
});

test("missing or truncated raw handshake logs cannot be rescued by a successful completion marker", () => {
  const targets = ["10.42.0.85", "10.42.1.12"];
  assert.equal(verifiedLogs(logFixture(), targets), true);
  assert.equal(verifiedLogs(logFixture().filter((e) => JSON.parse(e.message).kind !== "handshake"), targets), false);
  assert.equal(verifiedLogs(logFixture().slice(0, -1), targets), false);
  const logs = logFixture();
  logs[2].message = logs[2].message.replace("return code: 0 (ok)", "return code: 62 (hostname mismatch)");
  assert.equal(verifiedLogs(logs, targets), false);
});

test("delayed log delivery is retried within a fixed page budget before verification", async (t) => {
  let reads = 0;
  const f = fixture(t, (action, result) => { if (action === "get-log-events" && ++reads <= 2) result.events = []; });
  const oldSummary = process.env.GITHUB_STEP_SUMMARY;
  process.env.GITHUB_STEP_SUMMARY = join(f.config.work, "summary");
  t.after(() => { if (oldSummary === undefined) delete process.env.GITHUB_STEP_SUMMARY; else process.env.GITHUB_STEP_SUMMARY = oldSummary; });
  await execute(f.aws, f.config, async () => {});
  await finish(f.aws, f.config, async () => {});
  assert.equal(f.evidence().success, true);
  assert.equal(reads, 4);
});

test("a task that never stops and absent logs fail within bounded polling and preserve cleanup evidence", async (t) => {
  let sleeps = 0;
  const f = fixture(t, (action, result) => {
    if (action === "describe-tasks") result.tasks[0].lastStatus = "RUNNING";
    if (action === "get-log-events") result.events = [];
  });
  const oldSummary = process.env.GITHUB_STEP_SUMMARY;
  process.env.GITHUB_STEP_SUMMARY = join(f.config.work, "summary");
  t.after(() => { if (oldSummary === undefined) delete process.env.GITHUB_STEP_SUMMARY; else process.env.GITHUB_STEP_SUMMARY = oldSummary; });
  await assert.rejects(execute(f.aws, f.config, async () => { sleeps++; }), /inconclusive/);
  await assert.rejects(finish(f.aws, f.config, async () => { sleeps++; }), /cleanup failed/);
  assert.equal(f.calls.filter((c) => c.action === "describe-tasks").length, 60);
  assert.equal(f.calls.filter((c) => c.action === "get-log-events").length, 6);
  assert.equal(sleeps, 62);
  assert.equal(f.evidence().cleanup.stopped, false);
  assert.equal(f.calls.at(-1).action, "put-object");
});

test("cleanup failure stays bounded, still deregisters and archives, and never touches a neighboring family", async (t) => {
  const f = fixture(t, (action, result) => {
    if (action === "stop-task") throw new Error("Fixture stop denied");
    if (action === "list-task-definitions") result.taskDefinitionArns.push(`${ECS}:task-definition/${f.config.family}7:1`);
  });
  const oldSummary = process.env.GITHUB_STEP_SUMMARY;
  process.env.GITHUB_STEP_SUMMARY = join(f.config.work, "summary");
  t.after(() => { if (oldSummary === undefined) delete process.env.GITHUB_STEP_SUMMARY; else process.env.GITHUB_STEP_SUMMARY = oldSummary; });
  await execute(f.aws, f.config, async () => {});
  await assert.rejects(finish(f.aws, f.config, async () => {}), /cleanup failed/);
  assert.deepEqual(f.calls.filter((c) => c.action === "deregister-task-definition").map((c) => c.args[1]), [f.arn]);
  assert.ok(f.evidence().cleanup.errors.includes("Fixture stop denied"));
  assert.equal(f.calls.at(-1).action, "put-object");
});
