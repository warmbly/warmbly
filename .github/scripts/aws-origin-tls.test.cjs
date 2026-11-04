const { test } = require("node:test");
const assert = require("node:assert/strict");
const { spawnSync } = require("node:child_process");
const { mkdtempSync, readFileSync, rmSync, writeFileSync } = require("node:fs");
const { tmpdir } = require("node:os");
const { join } = require("node:path");
const { ACCOUNT, BUCKET, ROLE, LB, LISTENER, DESIRED } = require("./aws-origin-alb.cjs");
const { configuration, sessionPolicy, discover, definition, execute, finish, verifiedLogs, IMAGE, CLUSTER, SERVICE, EXECUTION, SG, ALB_SG, VPC, ECS, LOG } = require("./aws-origin-tls.cjs");
const { probe, classify, invoke, CBC, TLS13 } = require("./origin-tls-client.cjs");
const { refused } = require("./origin-tls-client.fixture.cjs");

const scaledTargets = ["10.42.0.85", "10.42.1.12", "10.42.0.86", "10.42.1.13"];

function logFixture(targets = ["10.42.0.85", "10.42.1.12"]) {
  const events = [];
  probe(targets, (args) => {
    if (args[0] === "version") return { status: 0, stdout: "OpenSSL fixture" };
    if (args[0] === "ciphers") return { status: 0, stdout: CBC.join(":") };
    const cipher = args.at(-1);
    return { command: ["openssl", ...args], ...(CBC.includes(cipher) ?
      refused(cipher) :
      { status: 0, stdout: `-----BEGIN CERTIFICATE-----\nfixture\nNew, ${cipher === TLS13 ? "TLSv1.3" : "TLSv1.2"}, Cipher is ${cipher}\nVerify return code: 0 (ok)`, stderr: "" }) };
  }, (row) => events.push({ message: JSON.stringify(row) }));
  return events;
}

const env = () => ({ GITHUB_ACTIONS: "true", GITHUB_REPOSITORY: "warmbly/warmbly", GITHUB_REF: "refs/heads/main",
  GITHUB_EVENT_NAME: "workflow_dispatch", GITHUB_WORKFLOW_REF: "warmbly/warmbly/.github/workflows/probe-origin-tls-aws.yml@refs/heads/main",
  GITHUB_RUN_ATTEMPT: "1", GITHUB_RUN_ID: "123456", GITHUB_SHA: "a".repeat(40), STATE_BUCKET: BUCKET, APPLY_ROLE: ROLE,
  WORK: "/tmp/fixture", GITHUB_OUTPUT: "/tmp/output", GITHUB_STEP_SUMMARY: "/tmp/summary" });

function fixture(t, change = () => {}, targets = ["10.42.0.85", "10.42.1.12"]) {
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
      case "describe-network-interfaces": result = { NetworkInterfaces: targets.map((ip, i) => {
        const z = zones[i % 2];
        return { Description: "ELB app/warmbly-production-ingress/19d5c82c1651c871", NetworkInterfaceId: `eni-${i + 1}`,
          AvailabilityZone: z.ZoneName, SubnetId: z.SubnetId, VpcId: VPC, Status: "in-use", RequesterManaged: true, OwnerId: ACCOUNT,
          PrivateIpAddress: ip, Groups: [{ GroupId: ALB_SG }] };
      }) }; break;
      case "describe-services": result = { services: [{ serviceArn: SERVICE, status: "ACTIVE", networkConfiguration: { awsvpcConfiguration: network } }] }; break;
      case "describe-subnets": result = { Subnets: zones.map((z) => ({ SubnetId: z.SubnetId, VpcId: VPC, State: "available" })) }; break;
      case "put-object": result = { ServerSideEncryption: "AES256", ChecksumSHA256: args[args.indexOf("--checksum-sha256") + 1], VersionId: "private-version" }; break;
      case "register-task-definition": result = { taskDefinition: { taskDefinitionArn: arn } }; break;
      case "run-task": result = { tasks: [state] }; break;
      case "describe-tasks": result = { tasks: [state] }; break;
      case "list-tasks": {
        assert.equal(args[1], CLUSTER);
        if (args.includes("--started-by")) {
          assert.deepEqual(args, ["--cluster", CLUSTER, "--started-by", config.startedBy]);
          result = { taskArns: state.lastStatus === "STOPPED" ? [] : [task] };
        } else {
          assert.deepEqual(args, ["--cluster", CLUSTER, "--family", config.family, "--desired-status", "STOPPED"]);
          result = { taskArns: state.lastStatus === "STOPPED" ? [task] : [] };
        }
        break;
      }
      case "stop-task": state.lastStatus = "STOPPED"; result = { task: state }; break;
      case "list-task-definitions": result = { taskDefinitionArns: [arn] }; break;
      case "deregister-task-definition": result = { taskDefinition: { taskDefinitionArn: arn, status: "INACTIVE" } }; break;
      case "get-log-events": result = { events: args.includes("--next-token") ? [] : logFixture(targets), nextForwardToken: "end" }; break;
      default: throw new Error(`Unexpected ${service} ${action}`);
    }
    result = structuredClone(result);
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

test("each credential phase grants exactly its required operations and resource scopes", () => {
  const config = configuration(env());
  const allow = (Action, Resource, Condition) => ({ Effect: "Allow", Action, Resource, ...(Condition ? { Condition } : {}) });
  const archive = allow("s3:PutObject", `arn:aws:s3:::${BUCKET}/warmbly/production/diagnostics/origin-tls/${config.run}/1/*`, {
    StringEquals: { "s3:x-amz-server-side-encryption": "AES256", "s3:ResourceAccount": ACCOUNT },
    Bool: { "aws:SecureTransport": "true" }, Null: { "s3:if-none-match": "false" },
  });
  const expected = {
    probe: [
      allow(["elasticloadbalancing:DescribeLoadBalancers", "elasticloadbalancing:DescribeListeners",
        "ec2:DescribeNetworkInterfaces", "ec2:DescribeSubnets", "ecs:RegisterTaskDefinition"], "*"),
      allow("ecs:DescribeServices", SERVICE),
      allow("ecs:RunTask", `${ECS}:task-definition/${config.family}:*`, { ArnEquals: { "ecs:cluster": CLUSTER } }),
      allow("ecs:DescribeTasks", `${ECS}:task/warmbly-production/*`),
      allow("iam:PassRole", EXECUTION, { StringEquals: { "iam:PassedToService": "ecs-tasks.amazonaws.com" } }),
      archive,
    ],
    cleanup: [
      allow(["ecs:ListTaskDefinitions", "ecs:ListTasks"], "*"),
      allow("ecs:DeregisterTaskDefinition", "*"),
      allow(["ecs:DescribeTasks", "ecs:StopTask"], `${ECS}:task/warmbly-production/*`),
      allow("logs:GetLogEvents", `arn:aws:logs:us-east-1:${ACCOUNT}:log-group:${LOG}:log-stream:origin-tls-${config.run}/*`),
      archive,
    ],
  };
  for (const phase of ["probe", "cleanup"]) {
    const policy = sessionPolicy(config, phase);
    assert.deepEqual(policy, { Version: "2012-10-17", Statement: expected[phase] });
    assert.ok(JSON.stringify(policy).length < 2048);
  }
  for (const phase of [undefined, "", "finish", "all"]) assert.throws(() => sessionPolicy(config, phase), /credential phase/);
});

test("the source gate emits distinct phase policies and refuses an unverified checkout", (t) => {
  const f = fixture(t);
  const revision = spawnSync("git", ["rev-parse", "HEAD"], { encoding: "utf8" });
  assert.equal(revision.status, 0);
  const output = join(f.config.work, "output");
  const childEnv = { ...process.env, ...env(), WORK: f.config.work, GITHUB_OUTPUT: output, GITHUB_SHA: revision.stdout.trim() };
  const run = () => spawnSync(process.execPath, [join(__dirname, "aws-origin-tls.cjs"), "session-policy"], { env: childEnv, encoding: "utf8" });
  assert.equal(run().status, 0);
  const policies = Object.fromEntries(readFileSync(output, "utf8").trim().split("\n").map((line) => {
    const equals = line.indexOf("=");
    return [line.slice(0, equals), JSON.parse(line.slice(equals + 1))];
  }));
  assert.deepEqual(policies, { probe_policy: sessionPolicy(f.config, "probe"), cleanup_policy: sessionPolicy(f.config, "cleanup") });
  writeFileSync(output, "");
  childEnv.GITHUB_SHA = "0".repeat(40);
  assert.equal(run().status, 1);
  assert.equal(readFileSync(output, "utf8"), "");
});

function phaseAWS(f, phase) {
  const actions = sessionPolicy(f.config, phase).Statement.flatMap((s) => s.Action);
  return (service, action, args) => {
    const prefix = { elbv2: "elasticloadbalancing", s3api: "s3" }[service] || service;
    const name = action.split("-").map((word) => word[0].toUpperCase() + word.slice(1)).join("");
    if (service !== "sts" || action !== "get-caller-identity") assert.ok(actions.includes(`${prefix}:${name}`), `${phase} must permit ${service} ${action}`);
    return f.aws(service, action, args);
  };
}

for (const failure of [null, "register-task-definition", "run-task", "describe-tasks"]) {
  test(`phase sessions support ${failure || "successful probe"} cleanup with one registration and launch at most`, async (t) => {
    let lost = false;
    const f = fixture(t, (action) => {
      if (action === failure && !lost) { lost = true; throw new Error("Fixture lost response"); }
    });
    const oldSummary = process.env.GITHUB_STEP_SUMMARY;
    process.env.GITHUB_STEP_SUMMARY = join(f.config.work, "summary");
    t.after(() => { if (oldSummary === undefined) delete process.env.GITHUB_STEP_SUMMARY; else process.env.GITHUB_STEP_SUMMARY = oldSummary; });
    const probeRun = execute(phaseAWS(f, "probe"), f.config, async () => {});
    if (failure) await assert.rejects(probeRun, /lost response/);
    else await probeRun;
    const boundary = f.calls.length;
    if (failure && failure !== "register-task-definition") f.state.lastStatus = "RUNNING";
    const cleanup = finish(phaseAWS(f, "cleanup"), f.config, async () => {});
    if (failure) await assert.rejects(cleanup, /incomplete/);
    else await cleanup;
    assert.ok(!f.calls.slice(0, boundary).some((c) => c.action === "deregister-task-definition"));
    assert.ok(!f.calls.slice(boundary).some((c) => ["register-task-definition", "run-task"].includes(c.action)));
    assert.equal(f.calls.filter((c) => c.action === "register-task-definition").length, 1);
    assert.equal(f.calls.filter((c) => c.action === "run-task").length, failure === "register-task-definition" ? 0 : 1);
    assert.equal(f.evidence().cleanup.deregistered, true);
    assert.equal(f.evidence().success, !failure);
    assert.equal(f.calls.at(-1).action, "put-object");
    assert.deepEqual(JSON.parse(readFileSync(join(f.config.work, "result.json"))), f.evidence());
  });
}

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
    ["describe-network-interfaces", (r) => { r.NetworkInterfaces[0].OwnerId = "000000000000"; }],
    ["describe-network-interfaces", (r) => { r.NetworkInterfaces[0].VpcId = "vpc-other"; }],
    ["describe-network-interfaces", (r) => { r.NetworkInterfaces[0].Status = "available"; }],
    ["describe-network-interfaces", (r) => { r.NetworkInterfaces[0].SubnetId = "subnet-other"; }],
    ["describe-network-interfaces", (r) => { r.NetworkInterfaces[0].AvailabilityZone = "us-east-1c"; }],
    ["describe-network-interfaces", (r) => { r.NetworkInterfaces[0].NetworkInterfaceId = r.NetworkInterfaces[1].NetworkInterfaceId; }],
    ["describe-network-interfaces", (r) => { r.NetworkInterfaces[0].PrivateIpAddress = r.NetworkInterfaces[1].PrivateIpAddress; }],
    ["describe-network-interfaces", (r) => { delete r.NetworkInterfaces[0].NetworkInterfaceId; }],
    ["describe-network-interfaces", (r) => { r.NetworkInterfaces[0].PrivateIpAddress = "10.42.0.999"; }],
    ["describe-services", (r) => { r.services[0].networkConfiguration.awsvpcConfiguration.securityGroups = ["sg-worker"]; }],
    ["describe-services", (r) => { r.services[0].networkConfiguration.awsvpcConfiguration.subnets = ["subnet-a", "subnet-a"]; }],
    ["describe-subnets", (r) => { r.Subnets[0].VpcId = "vpc-other"; }],
    ["describe-subnets", (r) => { r.Subnets[0] = r.Subnets[1]; }],
  ];
  for (const [target, mutate] of changes) {
    const f = fixture(t, (action, result) => { if (action === target) mutate(result); });
    await assert.rejects(discover(f.aws));
    assert.ok(!f.calls.some((c) => c.action === "run-task"));
  }
});

test("all current ALB interfaces across both zones are pinned, verified and privately archived", async (t) => {
  let reads = 0;
  const f = fixture(t, (action, result) => {
    if (action === "describe-network-interfaces" && ++reads % 2) result.NetworkInterfaces.reverse();
  }, scaledTargets);
  const oldSummary = process.env.GITHUB_STEP_SUMMARY;
  process.env.GITHUB_STEP_SUMMARY = join(f.config.work, "summary");
  t.after(() => { if (oldSummary === undefined) delete process.env.GITHUB_STEP_SUMMARY; else process.env.GITHUB_STEP_SUMMARY = oldSummary; });
  await execute(f.aws, f.config, async () => {});
  const evidence = f.evidence();
  assert.equal(evidence.before.enis.length, 4);
  const targets = JSON.parse(evidence.definition.containerDefinitions[0].environment.find((e) => e.name === "ORIGIN_TLS_TARGETS").value);
  assert.deepEqual(targets, [...scaledTargets].sort());
  await finish(f.aws, f.config, async () => {});
  assert.equal(f.evidence().success, true);
  assert.equal(verifiedLogs(f.evidence().logs, targets), true);
  for (const ip of targets) {
    assert.equal(verifiedLogs(f.evidence().logs.filter((e) => JSON.parse(e.message).ip !== ip), targets), false);
  }
  assert.equal(verifiedLogs(f.evidence().logs, [...targets, targets[0]]), false);
  assert.equal(verifiedLogs([...f.evidence().logs, ...logFixture(["10.42.0.87", "10.42.1.14"])], targets), false);
});

test("every interface remains stable before launch and after the diagnostic", async (t) => {
  for (const phase of [2, 3]) {
    for (const mutate of [
      (enis) => { enis.pop(); },
      (enis) => { enis.push({ ...enis[0], NetworkInterfaceId: "eni-a", PrivateIpAddress: "10.42.0.87" }); },
      (enis) => { enis[0].NetworkInterfaceId = "eni-b"; },
      (enis) => { enis[0].PrivateIpAddress = "10.42.0.87"; },
    ]) {
      let reads = 0;
      const f = fixture(t, (action, result) => {
        if (action === "describe-network-interfaces" && ++reads === phase) mutate(result.NetworkInterfaces);
      }, scaledTargets);
      await assert.rejects(execute(f.aws, f.config, async () => {}), /changed/);
      assert.equal(f.evidence().success, false);
      assert.equal(f.calls.filter((c) => c.action === "run-task").length, phase === 2 ? 0 : 1);
    }
  }
});

test("archive requires encryption, checksum and a real version before admitting a task", async (t) => {
  for (const mutate of [(r) => { r.VersionId = "null"; }, (r) => { r.ChecksumSHA256 = "bad"; }, (r) => { r.ServerSideEncryption = ""; }]) {
    const f = fixture(t, (action, result) => { if (action === "put-object") mutate(result); });
    await assert.rejects(execute(f.aws, f.config, async () => {}), /versioned/);
    assert.ok(!f.calls.some((c) => c.action === "register-task-definition"));
  }
});

test("successful lifecycle archives intent first, launches one secretless task, confirms cleanup and retains private logs", async (t) => {
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
  assert.ok(!actions.includes("stop-task"));
  assert.ok(actions.indexOf("describe-tasks") < actions.indexOf("deregister-task-definition"));
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
    if (failure !== "register-task-definition") f.state.lastStatus = "RUNNING";
    await assert.rejects(finish(f.aws, f.config, async () => {}), /incomplete/);
    assert.ok(f.calls.some((c) => c.action === "deregister-task-definition"));
    if (failure !== "register-task-definition") assert.ok(f.calls.some((c) => c.action === "stop-task"));
    if (failure !== "register-task-definition") {
      const stop = f.calls.findIndex((c) => c.action === "stop-task");
      assert.ok(f.calls.slice(0, stop).some((c) => c.action === "describe-tasks"));
      assert.equal(f.evidence().cleanup.recovery, "resolved");
      assert.equal(f.evidence().cleanup.stopped, true);
    }
    assert.equal(f.evidence().success, false);
    assert.equal(f.calls.filter((c) => c.action === "put-object").length, 2);
    assert.equal(f.calls.filter((c) => c.action === "run-task").length, failure === "register-task-definition" ? 0 : 1);
  });
}

test("stopped diagnostic recovery verifies exact identity and privately archives its logs", async (t) => {
  const f = fixture(t, (action) => { if (action === "run-task") throw new Error("Fixture lost response"); });
  const oldSummary = process.env.GITHUB_STEP_SUMMARY;
  process.env.GITHUB_STEP_SUMMARY = join(f.config.work, "summary");
  t.after(() => { if (oldSummary === undefined) delete process.env.GITHUB_STEP_SUMMARY; else process.env.GITHUB_STEP_SUMMARY = oldSummary; });
  await assert.rejects(execute(f.aws, f.config, async () => {}), /lost response/);
  assert.equal(f.evidence().taskArn, undefined);
  await assert.rejects(finish(f.aws, f.config, async () => {}), /incomplete/);
  const evidence = f.evidence();
  assert.equal(evidence.cleanup.recovery, "resolved");
  assert.equal(evidence.cleanup.stopped, true);
  assert.deepEqual(evidence.cleanup.taskArns, [f.task]);
  assert.deepEqual(evidence.cleanup.tasks.tasks, [f.state]);
  assert.equal(verifiedLogs(evidence.logs, evidence.before.enis.map((e) => e.PrivateIpAddress)), true);
  assert.ok(!f.calls.some((c) => c.action === "stop-task"));
  const described = f.calls.findIndex((c) => c.action === "describe-tasks");
  assert.ok(described < f.calls.findIndex((c) => c.action === "get-log-events"));
  assert.equal(evidence.success, false);
  const archived = JSON.parse(readFileSync(join(f.config.work, "result.json")));
  assert.deepEqual(archived, evidence);
  assert.equal(f.calls.at(-1).action, "put-object");
});

test("ambiguous diagnostic recovery retains an unresolved private outcome within its discovery budget", async (t) => {
  const f = fixture(t, (action, result) => {
    if (action === "run-task") throw new Error("Fixture lost response");
    if (action === "list-tasks") result.taskArns = [];
  });
  const oldSummary = process.env.GITHUB_STEP_SUMMARY;
  process.env.GITHUB_STEP_SUMMARY = join(f.config.work, "summary");
  t.after(() => { if (oldSummary === undefined) delete process.env.GITHUB_STEP_SUMMARY; else process.env.GITHUB_STEP_SUMMARY = oldSummary; });
  await assert.rejects(execute(f.aws, f.config, async () => {}), /lost response/);
  let sleeps = 0;
  await assert.rejects(finish(f.aws, f.config, async () => { sleeps++; }), /incomplete/);
  const evidence = f.evidence();
  assert.equal(evidence.cleanup.recovery, "unresolved");
  assert.equal(evidence.cleanup.stopped, false);
  assert.deepEqual(evidence.cleanup.taskArns, []);
  assert.deepEqual(evidence.logs, []);
  assert.ok(evidence.cleanup.errors.includes("Diagnostic task identity unresolved"));
  assert.equal(sleeps, 3);
  assert.equal(f.calls.filter((c) => c.action === "list-tasks").length, 6);
  assert.ok(!f.calls.some((c) => ["describe-tasks", "stop-task", "get-log-events"].includes(c.action)));
  assert.deepEqual(f.calls.filter((c) => c.action === "deregister-task-definition").map((c) => c.args[1]), [f.arn]);
  assert.equal(evidence.success, false);
  assert.deepEqual(JSON.parse(readFileSync(join(f.config.work, "result.json"))), evidence);
  assert.equal(f.calls.at(-1).action, "put-object");
});

for (const field of ["taskArn", "clusterArn", "startedBy", "taskDefinitionArn"]) {
  test(`diagnostic cleanup requires exact ${field} readback before stopping or collecting logs`, async (t) => {
    const f = fixture(t, (action, result) => {
      if (action === "run-task") throw new Error("Fixture lost response");
      if (action === "describe-tasks") result.tasks[0][field] += "7";
    });
    const oldSummary = process.env.GITHUB_STEP_SUMMARY;
    process.env.GITHUB_STEP_SUMMARY = join(f.config.work, "summary");
    t.after(() => { if (oldSummary === undefined) delete process.env.GITHUB_STEP_SUMMARY; else process.env.GITHUB_STEP_SUMMARY = oldSummary; });
    await assert.rejects(execute(f.aws, f.config, async () => {}), /lost response/);
    f.state.lastStatus = "RUNNING";
    await assert.rejects(finish(f.aws, f.config, async () => {}), /incomplete/);
    assert.ok(!f.calls.some((c) => ["stop-task", "get-log-events"].includes(c.action)));
    assert.equal(f.calls.filter((c) => c.action === "describe-tasks").length, 12);
    assert.deepEqual(f.evidence().cleanup.taskArns, []);
    assert.equal(f.evidence().cleanup.stopped, false);
    assert.equal(f.evidence().cleanup.recovery, "unresolved");
    assert.equal(f.calls.at(-1).action, "put-object");
  });
}

test("diagnostic recovery cleans and archives only verified tasks among mixed candidates", async (t) => {
  const other = `${ECS}:task/warmbly-production/${"2".repeat(32)}`;
  const f = fixture(t, (action, result, calls) => {
    if (action === "run-task") throw new Error("Fixture lost response");
    if (action === "list-tasks" && calls.at(-1).args.includes("STOPPED")) result.taskArns.push(other);
    if (action === "describe-tasks") result.tasks.push({ ...f.state, taskArn: other, startedBy: `${f.config.startedBy}7`, lastStatus: "STOPPED" });
  });
  const oldSummary = process.env.GITHUB_STEP_SUMMARY;
  process.env.GITHUB_STEP_SUMMARY = join(f.config.work, "summary");
  t.after(() => { if (oldSummary === undefined) delete process.env.GITHUB_STEP_SUMMARY; else process.env.GITHUB_STEP_SUMMARY = oldSummary; });
  await assert.rejects(execute(f.aws, f.config, async () => {}), /lost response/);
  f.state.lastStatus = "RUNNING";
  await assert.rejects(finish(f.aws, f.config, async () => {}), /incomplete/);
  assert.deepEqual(f.calls.filter((c) => c.action === "stop-task").map((c) => c.args[3]), [f.task]);
  assert.ok(f.calls.filter((c) => c.action === "get-log-events").every((c) =>
    c.args[3] === `origin-tls-${f.config.run}/probe/${f.task.split("/").pop()}`));
  assert.deepEqual(f.evidence().cleanup.taskArns, [f.task]);
  assert.equal(verifiedLogs(f.evidence().logs, f.evidence().before.enis.map((e) => e.PrivateIpAddress)), true);
  assert.equal(f.evidence().cleanup.stopped, false);
  assert.equal(f.evidence().cleanup.recovery, "unresolved");
  assert.equal(f.calls.filter((c) => c.action === "stop-task").length, 1);
  assert.equal(f.calls.at(-1).action, "put-object");
});

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

test("private archive verification reclassifies ambiguous or altered negative evidence", () => {
  const targets = ["10.42.0.85", "10.42.1.12"];
  for (const alter of [
    (r) => { r.stdout = "CONNECTED\nclosed\nno peer certificate available\nNew, (NONE), Cipher is (NONE)\n"; r.status = 0; },
    (r) => { r.stdout = r.stdout.replace("<<<", ">>>"); },
    (r) => { r.stdout = r.stdout.replace("02 28", "01 00"); },
    (r) => { r.stdout = r.stdout.replace("c0 27", "c0 2f"); },
  ]) {
    const logs = logFixture();
    const index = logs.findIndex((e) => JSON.parse(e.message).negative);
    const row = JSON.parse(logs[index].message);
    alter(row);
    assert.equal(row.outcome, "refused");
    logs[index].message = JSON.stringify(row);
    assert.equal(verifiedLogs(logs, targets), false);
  }
});

test("refusal evidence accounts for complete traces, framing and both captured streams", () => {
  const targets = ["10.42.0.85", "10.42.1.12"];
  const trace = (direction, kind, name, bytes, version = "1.2") =>
    `${direction} TLS ${version}, ${kind} [length ${bytes.length.toString(16).padStart(4, "0")}]${name ? `, ${name}` : ""}\n` +
    `    ${bytes.toString("hex").match(/../g).join(" ")}\n`;
  const check = (result, cipher, expected, label) => {
    assert.equal(classify(result, "TLSv1.2", cipher, true), expected ? "refused" : "inconclusive", label);
    const logs = logFixture();
    const index = logs.findIndex((e) => { const row = JSON.parse(e.message); return row.negative && row.cipher === cipher; });
    const row = JSON.parse(logs[index].message);
    assert.equal(row.outcome, "refused");
    logs[index].message = JSON.stringify({ ...row, ...result });
    assert.equal(verifiedLogs(logs, targets), expected, label);
  };
  for (const cipher of CBC) {
    for (const scsv of [false, true]) {
      const suites = [0xc0, cipher === CBC[0] ? 0x27 : 0x28, ...(scsv ? [0, 0xff] : [])];
      const bytes = Buffer.from([1, 0, 0, 39 + suites.length, 3, 3,
        ...Array.from({ length: 32 }, (_, i) => 0x60 + i), 0, 0, suites.length, ...suites, 1, 0]);
      const hello = trace(">>>", "Handshake", "ClientHello", bytes);
      const alert = trace("<<<", "Alert", "fatal handshake_failure", Buffer.from([2, 40]));
      const sent = trace(">>>", "RecordHeader", "", Buffer.from([22, 3, 1, 0, bytes.length]), "1.0");
      const received = trace("<<<", "RecordHeader", "", Buffer.from([21, 3, 3, 0, 2]));
      const summary = "no peer certificate available\nNew, (NONE), Cipher is (NONE)\n";
      const result = { status: 1, stdout: hello + alert + summary, stderr: "SSL_connect:reading peer alert\n" };
      check(result, cipher, true, "complete message trace");
      check({ ...result, stdout: sent + hello + received + alert + summary }, cipher, true, "complete record trace");
      for (const size of [0, 32, 33]) {
        const message = Buffer.concat([bytes.subarray(0, 38), Buffer.from([size]), Buffer.alloc(size, 0x5a), bytes.subarray(39)]);
        message.writeUIntBE(message.length - 4, 1, 3);
        check({ ...result, stdout: trace(">>>", "Handshake", "ClientHello", message) + alert + summary },
          cipher, size <= 32, "bounded ClientHello session identifier");
      }
      for (const [expected, tails] of [
        [true, [[1, 0], [1, 0, 0, 0], [1, 0, 0, 5, 0xfa, 0xce, 0, 1, 0xaa]]],
        [false, [[], [0], [1], [2, 0], [1, 1], [1, 0, 0], [1, 0, 0, 1],
          [1, 0, 0, 1, 0], [1, 0, 0, 5, 0xfa, 0xce, 0, 2, 0xaa],
          [1, 0, 0, 8, 0xfa, 0xce, 0, 0, 0xfa, 0xce, 0, 0]]],
      ]) {
        for (const tail of tails) {
          const message = Buffer.concat([bytes.subarray(0, -2), Buffer.from(tail)]);
          message.writeUIntBE(message.length - 4, 1, 3);
          check({ ...result, stdout: trace(">>>", "Handshake", "ClientHello", message) + alert + summary },
            cipher, expected, "complete ClientHello vectors");
        }
      }
      for (const [expected, versions] of [
        [true, [2, 3, 3]], [true, [4, 3, 4, 3, 3]], [true, [4, 3, 3, 3, 4]],
        [false, []], [false, [0]], [false, [1, 3]], [false, [2, 3]],
        [false, [2, 3, 4]], [false, [4, 3, 3]], [false, [2, 3, 3, 3, 4]],
      ]) {
        const extensions = Buffer.from([0, 43, 0, versions.length, ...versions]);
        const message = Buffer.concat([bytes, Buffer.from([0, extensions.length]), extensions]);
        message.writeUIntBE(message.length - 4, 1, 3);
        check({ ...result, stdout: trace(">>>", "Handshake", "ClientHello", message) + alert + summary },
          cipher, expected, "ClientHello version selection includes TLS 1.2");
      }
      for (const extra of [
        alert.slice(0, alert.indexOf("[length") + 3),
        alert.slice(0, alert.indexOf("\n") + 1),
        trace("<<<", "Handshake", "ServerHello", Buffer.from([2, 0, 0, 0])),
        "    91 a2 b3\n",
        "[TLS message omitted]\n",
      ]) {
        for (const stream of ["stdout", "stderr"]) {
          for (const position of ["before", "after"]) {
            const output = position === "before" ? extra + result[stream] : result[stream] + extra;
            check({ ...result, [stream]: output }, cipher, false, `${stream} ${position} envelope`);
          }
        }
      }
      for (const [label, stdout] of [
        ["record count", sent + hello + alert + summary],
        ["record order", sent + received + hello + alert + summary],
        ["record direction", sent + hello + received.replace("<<<", ">>>") + alert + summary],
        ["record content type", sent.replace("16 03", "15 03") + hello + received + alert + summary],
        ["record version", sent.replace("TLS 1.0", "TLS 1.2") + hello + received + alert + summary],
        ["record payload length", sent + hello + received.replace("00 02", "00 03") + alert + summary],
        ["message declared length", hello + alert.replace("length 0002", "length 0003") + summary],
        ["message line termination", summary + hello + alert.trimEnd()],
        ["intervening message", hello + "[TLS message omitted]\n" + alert + summary],
      ]) check({ ...result, stdout }, cipher, false, label);
      for (const stream of ["stdout", "stderr"]) {
        const ticket = trace("<<<", "Handshake", "NewSessionTicket", Buffer.from([4, 0, 0, 2, 0x95, 0xa6]));
        const sanitized = invoke([], () => ({ ...result, [stream]: result[stream] + ticket }));
        assert.match(sanitized[stream], /\[TLS message omitted\]/);
        check(sanitized, cipher, false, `${stream} omitted message`);
      }
    }
  }
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

test("definition recovery confirms exact run identities within a fixed discovery budget and archives every outcome", async (t) => {
  const oldSummary = process.env.GITHUB_STEP_SUMMARY;
  t.after(() => { if (oldSummary === undefined) delete process.env.GITHUB_STEP_SUMMARY; else process.env.GITHUB_STEP_SUMMARY = oldSummary; });
  for (const [name, discoveredAt, includeInvalid] of [
    ["empty discovery remains unresolved", Infinity, false],
    ["neighboring and malformed identities remain unresolved", Infinity, true],
    ["exact identity on second discovery is confirmed", 2, true],
    ["exact identity on final discovery is confirmed", 3, true],
  ]) {
    await t.test(name, async (t) => {
      let reads = 0;
      const f = fixture(t, (action, result, calls) => {
        if (action === "register-task-definition") throw new Error("Fixture registration response unavailable");
        if (action === "list-task-definitions") {
          assert.deepEqual(calls.at(-1).args, ["--family-prefix", f.config.family, "--status", "ACTIVE"]);
          const invalid = [
            `${ECS}:task-definition/${f.config.family}7:1`,
            `${ECS}:task-definition/${f.config.family}-other:1`,
            `arn:aws:ecs:us-west-2:${ACCOUNT}:task-definition/${f.config.family}:1`,
            `arn:aws:ecs:us-east-1:000000000000:task-definition/${f.config.family}:1`,
            `${ECS}:task-definition/${f.config.family}:latest`,
            `${f.arn}/suffix`, `${f.arn}\n`, `${f.arn}\r\n`, null, 42,
          ];
          result.taskDefinitionArns = [
            ...(includeInvalid ? invalid : []), ...(++reads >= discoveredAt ? [f.arn, f.arn] : []),
          ];
        }
      });
      process.env.GITHUB_STEP_SUMMARY = join(f.config.work, "summary");
      await assert.rejects(execute(f.aws, f.config, async () => {}), /registration response unavailable/);
      const delays = [];
      await assert.rejects(finish(f.aws, f.config, async (ms) => { delays.push(ms); }), /incomplete/);
      const resolved = Number.isFinite(discoveredAt);
      const evidence = f.evidence();
      assert.equal(reads, Math.min(discoveredAt, 3));
      assert.deepEqual(delays, Array(reads - 1).fill(5000));
      assert.deepEqual(evidence.cleanup.definitionArns, resolved ? [f.arn] : []);
      assert.equal(evidence.cleanup.definitionRecovery, resolved ? "resolved" : "unresolved");
      assert.equal(evidence.cleanup.deregistered, resolved);
      assert.equal(evidence.cleanup.errors.includes("Diagnostic definition identity unresolved"), !resolved);
      assert.deepEqual(f.calls.filter((c) => c.action === "deregister-task-definition").map((c) => c.args),
        resolved ? [["--task-definition", f.arn]] : []);
      assert.equal(f.calls.filter((c) => c.action === "register-task-definition").length, 1);
      assert.ok(!f.calls.some((c) => ["run-task", "stop-task", "get-log-events"].includes(c.action)));
      assert.equal(evidence.success, false);
      assert.deepEqual(JSON.parse(readFileSync(join(f.config.work, "result.json"))), evidence);
      assert.equal(f.calls.filter((c) => c.action === "put-object").length, 2);
      assert.equal(f.calls.at(-1).action, "put-object");
    });
  }
});

test("definition discovery errors remain inconclusive even when a later exact identity can be cleaned", async (t) => {
  const oldSummary = process.env.GITHUB_STEP_SUMMARY;
  t.after(() => { if (oldSummary === undefined) delete process.env.GITHUB_STEP_SUMMARY; else process.env.GITHUB_STEP_SUMMARY = oldSummary; });
  for (const malformed of [false, true]) {
    let reads = 0;
    const f = fixture(t, (action, result) => {
      if (action === "register-task-definition") throw new Error("Fixture registration response unavailable");
      if (action === "list-task-definitions" && ++reads === 1) {
        if (malformed) result.taskDefinitionArns = {};
        else throw new Error("Fixture discovery response unavailable");
      }
    });
    process.env.GITHUB_STEP_SUMMARY = join(f.config.work, "summary");
    await assert.rejects(execute(f.aws, f.config, async () => {}), /registration response unavailable/);
    await assert.rejects(finish(f.aws, f.config, async () => {}), /incomplete/);
    assert.equal(reads, 2);
    assert.equal(f.evidence().cleanup.definitionRecovery, "resolved");
    assert.equal(f.evidence().cleanup.deregistered, false);
    assert.ok(f.evidence().cleanup.errors.includes(malformed ? "Diagnostic definition discovery unconfirmed" : "Fixture discovery response unavailable"));
    assert.deepEqual(f.calls.filter((c) => c.action === "deregister-task-definition").map((c) => c.args[1]), [f.arn]);
    assert.ok(!f.calls.some((c) => c.action === "run-task"));
    assert.equal(f.evidence().success, false);
    assert.deepEqual(JSON.parse(readFileSync(join(f.config.work, "result.json"))), f.evidence());
    assert.equal(f.calls.at(-1).action, "put-object");
  }
});

test("definition cleanup is not required when registration was never attempted", async (t) => {
  const f = fixture(t, (action, result) => { if (action === "get-caller-identity") result.Account = "000000000000"; });
  const oldSummary = process.env.GITHUB_STEP_SUMMARY;
  process.env.GITHUB_STEP_SUMMARY = join(f.config.work, "summary");
  t.after(() => { if (oldSummary === undefined) delete process.env.GITHUB_STEP_SUMMARY; else process.env.GITHUB_STEP_SUMMARY = oldSummary; });
  await assert.rejects(execute(f.aws, f.config, async () => {}), /identity required/);
  await assert.rejects(finish(f.aws, f.config, async () => { assert.fail("No registration needs no discovery delay"); }), /incomplete/);
  assert.ok(!f.calls.some((c) => ["register-task-definition", "list-task-definitions", "deregister-task-definition", "run-task"].includes(c.action)));
  assert.deepEqual(f.evidence().cleanup.definitionArns, []);
  assert.equal(f.evidence().cleanup.definitionRecovery, "not-required");
  assert.equal(f.evidence().cleanup.deregistered, true);
  assert.equal(f.evidence().success, false);
  assert.deepEqual(JSON.parse(readFileSync(join(f.config.work, "result.json"))), f.evidence());
  assert.equal(f.calls.at(-1).action, "put-object");
});

test("a saved exact definition permits confirmed cleanup with empty active discovery", async (t) => {
  const f = fixture(t, (action, result) => { if (action === "list-task-definitions") result.taskDefinitionArns = []; });
  const oldSummary = process.env.GITHUB_STEP_SUMMARY;
  process.env.GITHUB_STEP_SUMMARY = join(f.config.work, "summary");
  t.after(() => { if (oldSummary === undefined) delete process.env.GITHUB_STEP_SUMMARY; else process.env.GITHUB_STEP_SUMMARY = oldSummary; });
  await execute(f.aws, f.config, async () => {});
  await finish(f.aws, f.config, async () => { assert.fail("Saved identity needs no discovery delay"); });
  assert.equal(f.calls.filter((c) => c.action === "list-task-definitions").length, 1);
  assert.deepEqual(f.calls.filter((c) => c.action === "deregister-task-definition").map((c) => c.args[1]), [f.arn]);
  assert.equal(f.evidence().cleanup.definitionRecovery, "resolved");
  assert.equal(f.evidence().cleanup.deregistered, true);
  assert.equal(f.evidence().success, true);
  assert.equal(f.calls.filter((c) => c.action === "run-task").length, 1);
});

test("definition cleanup confirms only matching inactive readbacks and privately archives unconfirmed responses", async (t) => {
  const oldSummary = process.env.GITHUB_STEP_SUMMARY;
  t.after(() => { if (oldSummary === undefined) delete process.env.GITHUB_STEP_SUMMARY; else process.env.GITHUB_STEP_SUMMARY = oldSummary; });
  for (const response of ["unavailable", "neighbor", "active"]) {
    const f = fixture(t, (action, result) => {
      if (action === "deregister-task-definition") {
        if (response === "unavailable") throw new Error("Fixture deregistration response unavailable");
        if (response === "neighbor") result.taskDefinition.taskDefinitionArn = `${ECS}:task-definition/${f.config.family}7:1`;
        if (response === "active") result.taskDefinition.status = "ACTIVE";
      }
    });
    process.env.GITHUB_STEP_SUMMARY = join(f.config.work, "summary");
    await execute(f.aws, f.config, async () => {});
    await assert.rejects(finish(f.aws, f.config, async () => {}), /incomplete/);
    assert.equal(f.evidence().cleanup.definitionRecovery, "resolved");
    assert.equal(f.evidence().cleanup.deregistered, false);
    assert.ok(f.evidence().cleanup.errors.includes("Diagnostic definition deregistration unconfirmed"));
    assert.equal(f.calls.filter((c) => c.action === "deregister-task-definition").length, 1);
    assert.equal(f.calls.filter((c) => c.action === "register-task-definition").length, 1);
    assert.equal(f.calls.filter((c) => c.action === "run-task").length, 1);
    assert.equal(f.evidence().success, false);
    assert.deepEqual(JSON.parse(readFileSync(join(f.config.work, "result.json"))), f.evidence());
    assert.equal(f.calls.at(-1).action, "put-object");
  }
});

test("deregistration validates saved and discovered definitions against the exact run family, account, region and revision", async (t) => {
  const oldSummary = process.env.GITHUB_STEP_SUMMARY;
  t.after(() => { if (oldSummary === undefined) delete process.env.GITHUB_STEP_SUMMARY; else process.env.GITHUB_STEP_SUMMARY = oldSummary; });
  const config = configuration(env());
  const invalid = [
    `${ECS}:task-definition/${config.family}7:1`,
    `${ECS}:task-definition/${config.family}-other:1`,
    `${ECS}:task-definition/warmbly-production-backend:1`,
    `arn:aws:ecs:us-west-2:${ACCOUNT}:task-definition/${config.family}:1`,
    `arn:aws:ecs:us-east-1:000000000000:task-definition/${config.family}:1`,
    `${ECS}:task-definition/${config.family}:latest`,
    `${ECS}:task-definition/${config.family}:1/suffix`,
    `${ECS}:task-definition/${config.family}:1\n`,
  ];
  for (const saved of invalid) {
    const f = fixture(t, (action, result, calls) => {
      if (action === "register-task-definition") throw new Error("Fixture lost response");
      if (action === "list-task-definitions") {
        assert.deepEqual(calls.at(-1).args, ["--family-prefix", config.family, "--status", "ACTIVE"]);
        result.taskDefinitionArns = [f.arn, ...invalid, `${ECS}:task-definition/${config.family}:2`, f.arn, null, 42];
      }
      if (action === "deregister-task-definition") result.taskDefinition.taskDefinitionArn = calls.at(-1).args[1];
    });
    process.env.GITHUB_STEP_SUMMARY = join(f.config.work, "summary");
    await assert.rejects(execute(f.aws, f.config, async () => {}), /lost response/);
    writeFileSync(join(f.config.work, "evidence.json"), JSON.stringify({ ...f.evidence(), definitionArn: saved }));
    await assert.rejects(finish(f.aws, f.config, async () => {}), /incomplete/);
    const expected = [f.arn, `${ECS}:task-definition/${config.family}:2`];
    assert.deepEqual(f.calls.filter((c) => c.action === "deregister-task-definition").map((c) => c.args),
      expected.map((arn) => ["--task-definition", arn]));
    assert.deepEqual(f.evidence().cleanup.definitionArns, expected);
    assert.equal(f.evidence().cleanup.deregistered, true);
    assert.equal(f.calls.at(-1).action, "put-object");
  }
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
  f.state.lastStatus = "RUNNING";
  await assert.rejects(finish(f.aws, f.config, async () => {}), /cleanup failed/);
  assert.deepEqual(f.calls.filter((c) => c.action === "deregister-task-definition").map((c) => c.args[1]), [f.arn]);
  assert.ok(f.evidence().cleanup.errors.includes("Fixture stop denied"));
  assert.equal(f.calls.at(-1).action, "put-object");
});
