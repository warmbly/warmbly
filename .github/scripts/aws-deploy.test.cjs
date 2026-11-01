const { test } = require("node:test");
const assert = require("node:assert/strict");
const { mkdtempSync, writeFileSync } = require("node:fs");
const { join } = require("node:path");
const { tmpdir } = require("node:os");
const { configuration, deploy, runTask, compareTags, parseRef } = require("./aws-deploy.cjs");

const root = "arn:aws:ecs:us-east-1:000000000000:";
const registry = "ghcr.io/warmbly/warmbly";
const d = (c) => "sha256:" + c.repeat(64);
const KAFKA = ["backend", "consumer", "tracking", "migration", "ops"];
const FAMILIES = ["backend", "consumer", "forms", "tracking", "realtime", "migration", "validation", "ops"];
const secretArn = (svc) => `arn:aws:secretsmanager:us-east-1:000000000000:secret:fixture/${svc}-AbCdEf`;

function imagesFile(tag = "v1.3.0") {
  const dir = mkdtempSync(join(tmpdir(), "aws-deploy-"));
  const images = {};
  for (const svc of ["backend", "consumer", "worker", "forms", "tracking", "realtime"]) images[svc] = d("b");
  for (const svc of ["backend", "consumer", "worker", "tracking"]) images[`${svc}-kafka`] = d("c");
  writeFileSync(join(dir, "images.json"), JSON.stringify({ tag, registry, images }));
  return join(dir, "images.json");
}

function config(env = {}) {
  return configuration({
    AWS_ECS_CLUSTER: root + "cluster/fixture", DEPLOY_TAG: "v1.3.0", DEPLOY_IMAGES_JSON: imagesFile(env.DEPLOY_TAG || "v1.3.0"),
    DEPLOY_BAKE_SECONDS: "30", ...env,
  });
}

function definition(family, revision, options) {
  const secretSvc = ["migration", "validation", "ops"].includes(family) ? "backend" : family;
  const version = family === "backend" && revision === 1 ? "rotated" : options.versions?.[family] || "terraform";
  const secrets = family === "migration"
    ? [{ name: "PRIMARY_DB", valueFrom: `${secretArn("backend")}:PRIMARY_DB_DIRECT::${version}` }]
    : [
      { name: "PRIMARY_DB_DIRECT", valueFrom: `${secretArn(secretSvc)}:PRIMARY_DB_DIRECT::${version}` },
      { name: "REDIS_PASSWORD", valueFrom: `${secretArn(secretSvc)}:REDIS_PASSWORD::` },
    ];
  if (family === "backend" && (revision === 1 || !options.dropReadKeys)) secrets.push({ name: "CREDENTIALS_ENCRYPTION_READ_KEYS", valueFrom: `${secretArn("backend")}:CREDENTIALS_ENCRYPTION_READ_KEYS::${version}` });
  const image = family === "validation" ? "node:22-alpine"
    : `${options.registry || registry}/${["migration", "ops"].includes(family) ? "backend" : family}@${d("a")}`;
  return {
    taskDefinitionArn: `${root}task-definition/fixture-${family}:${revision}`, family: `fixture-${family}`, revision, status: "ACTIVE",
    networkMode: "awsvpc", cpu: "256", memory: "512", requiresCompatibilities: ["FARGATE"],
    containerDefinitions: [{
      name: family, image, secrets,
      entryPoint: family === "migration" ? ["/app/migrate"] : undefined,
      environment: [{ name: "DB_MIGRATIONS_ON_START", value: "false" }],
      logConfiguration: { logDriver: "awslogs", options: { "awslogs-group": "/fixture", "awslogs-stream-prefix": family } },
    }],
  };
}

function fixture(options = {}) {
  const calls = [];
  const defs = {};
  const tagsOf = {};
  for (const family of FAMILIES) {
    for (const revision of [1, 2]) {
      const arn = `${root}task-definition/fixture-${family}:${revision}`;
      defs[arn] = definition(family, revision, options);
      const tags = [{ key: "Environment", value: "production" }];
      if (!(options.noVariant && family === "backend" && revision === 2)) tags.push({ key: "warmbly-image-variant", value: KAFKA.includes(family) ? "kafka" : "default" });
      if (revision === 1) tags.push({ key: "warmbly-release", value: options.liveTag || "v1.2.0" });
      tagsOf[arn] = tags;
    }
  }
  const latest = (family) => Object.values(defs).filter((x) => x.family === `fixture-${family}`).sort((a, b) => b.revision - a.revision)[0];
  const services = ["backend", "consumer", "forms", "tracking", "realtime"].filter((n) => !options.absent?.includes(n)).map((name) => ({
    serviceName: `fixture-${name}`, status: "ACTIVE", desiredCount: 2, runningCount: 2, pendingCount: 0,
    taskDefinition: `${root}task-definition/fixture-${name}:1`, deployments: [{ rolloutState: "COMPLETED" }],
    networkConfiguration: { awsvpcConfiguration: { subnets: ["subnet-fixture"], securityGroups: ["sg-fixture"], assignPublicIp: "DISABLED" } },
  }));
  const tasks = {};
  const record = (client) => ({ async send(command) {
    const name = command.constructor.name;
    calls.push({ client, name, input: structuredClone(command.input) });
    switch (name) {
    case "DescribeTaskDefinitionCommand": {
      const ref = command.input.taskDefinition;
      const def = ref.startsWith("arn:") ? defs[ref] : latest(ref.replace(/^fixture-/, ""));
      if (!def) throw new Error(`no task definition ${ref}`);
      return { taskDefinition: structuredClone(def), tags: structuredClone(tagsOf[def.taskDefinitionArn]) };
    }
    case "RegisterTaskDefinitionCommand": {
      const prev = latest(command.input.family.replace(/^fixture-/, ""));
      const arn = `${root}task-definition/${command.input.family}:${prev.revision + 1}`;
      defs[arn] = { ...structuredClone(command.input), revision: prev.revision + 1, taskDefinitionArn: arn, status: "ACTIVE" };
      delete defs[arn].tags;
      tagsOf[arn] = command.input.tags;
      return { taskDefinition: { taskDefinitionArn: arn } };
    }
    case "DescribeServicesCommand": {
      const found = services.filter((s) => command.input.services.includes(s.serviceName));
      const failures = command.input.services.filter((n) => !found.some((s) => s.serviceName === n)).map((arn) => ({ arn, reason: "MISSING" }));
      return { services: found.map((s) => structuredClone(s)), failures };
    }
    case "UpdateServiceCommand": {
      const service = services.find((s) => s.serviceName === command.input.service);
      const svc = service.serviceName.replace("fixture-", "");
      if (options.updateFailure === svc && !command.input.taskDefinition.endsWith(":1")) throw new Error(`${svc} update failed`);
      service.taskDefinition = command.input.taskDefinition;
      if (command.input.desiredCount !== undefined) service.desiredCount = command.input.desiredCount;
      service.deployments = [{ rolloutState: options.rolloutFailure === svc && !command.input.taskDefinition.endsWith(":1") ? "FAILED" : "COMPLETED" }];
      if (options.circuitBreaker === svc) service.taskDefinition = `${root}task-definition/fixture-${svc}:1`;
      return {};
    }
    case "RunTaskCommand": {
      if (options.placementFailure) return { failures: [{ reason: "fixture" }] };
      const family = /fixture-(\w+):/.exec(command.input.taskDefinition)[1];
      const arn = `${root}task/fixture/${family}-${Object.keys(tasks).length}`;
      tasks[arn] = family;
      return { tasks: [{ taskArn: arn }] };
    }
    case "DescribeTasksCommand": {
      const family = tasks[command.input.tasks[0]];
      const exitCode = options.taskFailure === family ? 1 : 0;
      return { tasks: [{ lastStatus: "STOPPED", stopCode: "EssentialContainerExited", containers: [{ name: family, exitCode }] }] };
    }
    case "GetLogEventsCommand":
      return { events: [{ message: `log line from ${command.input.logStreamName}` }] };
    case "DescribeAlarmsCommand": {
      const firing = options.alarm && command.input.AlarmNamePrefix === `fixture-${options.alarm}-`;
      return { MetricAlarms: firing ? [{ AlarmName: `fixture-${options.alarm}-5xx` }] : [] };
    }
    case "ListSecretVersionIdsCommand":
      return { Versions: [
        { VersionId: "terraform", CreatedDate: new Date("2026-01-01") },
        { VersionId: "rotated", CreatedDate: new Date("2026-02-01") },
        { VersionId: "stale", CreatedDate: new Date("2025-01-01") },
      ] };
    case "DescribeSecretCommand":
      return { VersionIdsToStages: { terraform: ["AWSPREVIOUS"], "current-version-id-000000000000000000": ["AWSCURRENT"] } };
    default:
      throw new Error(`Unexpected command ${name}`);
    }
  } });
  const clients = { ecs: record("ecs"), secrets: record("secrets"), cloudwatch: record("cloudwatch"), logs: record("logs") };
  return { clients, calls, defs, tagsOf, services };
}

const run = (f, cfg, lines = []) => deploy(f.clients, cfg, async () => {}, (l) => lines.push(l));
const registered = (f) => f.calls.filter((c) => c.name === "RegisterTaskDefinitionCommand").map((c) => c.input);
const updates = (f) => f.calls.filter((c) => c.name === "UpdateServiceCommand").map((c) => c.input);
const runs = (f) => f.calls.filter((c) => c.name === "RunTaskCommand").map((c) => c.input);
const valueFrom = (def, key) => def.containerDefinitions[0].secrets.find((s) => s.name === key).valueFrom;

test("configuration refuses prereleases, bad modes and secret pins outside refresh-secrets", () => {
  assert.throws(() => config({ DEPLOY_TAG: "v1.3.0-rc.1" }), /prereleases never deploy/);
  assert.throws(() => config({ DEPLOY_MODE: "yolo" }), /DEPLOY_MODE/);
  for (const value of ["0", "299", "3601", "NaN", "300.5"]) {
    assert.throws(() => config({ DEPLOY_FLEET_BAKE_SECONDS: value }), /DEPLOY_FLEET_BAKE_SECONDS/);
  }
  assert.equal(config().fleetBake, 300);
  assert.equal(config({ DEPLOY_FLEET_BAKE_SECONDS: "1800" }).fleetBake, 1800);
  assert.throws(() => config({ DEPLOY_SERVICES: "backend,worker" }), /DEPLOY_SERVICES/);
  assert.throws(() => config({ DEPLOY_SECRET_VERSIONS: JSON.stringify({ backend: "a".repeat(36) }) }), /refresh-secrets/);
  assert.throws(() => config({ DEPLOY_MODE: "refresh-secrets", DEPLOY_SECRET_VERSIONS: JSON.stringify({ backend: "x" }) }), /secret version id/);
  assert.ok(compareTags("v1.10.0", "v1.9.9") > 0);
  assert.deepEqual(parseRef(`${secretArn("backend")}:K::v`), { arn: secretArn("backend"), key: "K", stage: "", version: "v" });
});

test("a release renders from the latest revision, swaps variant digests and carries tags", async () => {
  const f = fixture();
  await run(f, config());
  const byFamily = Object.fromEntries(registered(f).map((r) => [r.family.replace("fixture-", ""), r]));
  assert.equal(byFamily.backend.containerDefinitions[0].image, `${registry}/backend@${d("c")}`);
  assert.equal(byFamily.forms.containerDefinitions[0].image, `${registry}/forms@${d("b")}`);
  assert.equal(byFamily.tracking.containerDefinitions[0].image, `${registry}/tracking@${d("c")}`);
  assert.equal(byFamily.migration.containerDefinitions[0].image, `${registry}/backend@${d("c")}`);
  assert.equal(byFamily.ops.containerDefinitions[0].image, `${registry}/backend@${d("c")}`);
  assert.equal(byFamily.validation.containerDefinitions[0].image, "node:22-alpine");
  for (const r of Object.values(byFamily)) {
    assert.ok(r.tags.some((t) => t.key === "Environment"));
    assert.deepEqual(r.tags.filter((t) => t.key === "warmbly-release"), [{ key: "warmbly-release", value: "v1.3.0" }]);
    assert.equal(r.cpu, "256");
  }
});

test("the newest pinned secret version wins so a stale Terraform pin cannot undo a rotation", async () => {
  const f = fixture();
  await run(f, config());
  const backend = registered(f).find((r) => r.family === "fixture-backend");
  const migration = registered(f).find((r) => r.family === "fixture-migration");
  assert.ok(valueFrom(backend, "PRIMARY_DB_DIRECT").endsWith("::rotated"));
  assert.ok(valueFrom(migration, "PRIMARY_DB").endsWith("::rotated"));
  assert.ok(valueFrom(backend, "REDIS_PASSWORD").endsWith("REDIS_PASSWORD::"));
  const consumer = registered(f).find((r) => r.family === "fixture-consumer");
  assert.ok(valueFrom(consumer, "PRIMARY_DB_DIRECT").endsWith("::terraform"));
});

test("refresh-secrets pins every secret to AWSCURRENT and keeps the live images", async () => {
  const f = fixture();
  await run(f, configuration({ AWS_ECS_CLUSTER: root + "cluster/fixture", DEPLOY_MODE: "refresh-secrets", DEPLOY_BAKE_SECONDS: "0" }));
  const backend = registered(f).find((r) => r.family === "fixture-backend");
  const consumer = registered(f).find((r) => r.family === "fixture-consumer");
  assert.ok(valueFrom(backend, "PRIMARY_DB_DIRECT").endsWith("::current-version-id-000000000000000000"));
  assert.ok(valueFrom(consumer, "PRIMARY_DB_DIRECT").endsWith("::current-version-id-000000000000000000"));
  assert.equal(backend.containerDefinitions[0].image, `${registry}/backend@${d("a")}`);
  assert.ok(backend.tags.some((t) => t.key === "warmbly-release" && t.value === "v1.2.0"));
  assert.ok(!runs(f).some((r) => r.overrides), "refresh-secrets starts no fleet rollout");
});

test("explicit secret versions roll only what they name; other secrets keep their newest pin", async () => {
  const f = fixture();
  await run(f, configuration({ AWS_ECS_CLUSTER: root + "cluster/fixture", DEPLOY_MODE: "refresh-secrets", DEPLOY_BAKE_SECONDS: "0",
    DEPLOY_SERVICES: "consumer", DEPLOY_SECRET_VERSIONS: JSON.stringify({ consumer: "explicit-version-id-0000000000000000" }) }));
  const consumer = registered(f).find((r) => r.family === "fixture-consumer");
  const validation = registered(f).find((r) => r.family === "fixture-validation");
  assert.ok(valueFrom(consumer, "PRIMARY_DB_DIRECT").endsWith("::explicit-version-id-0000000000000000"));
  assert.ok(valueFrom(validation, "PRIMARY_DB_DIRECT").endsWith("::rotated"));
  assert.ok(!f.calls.some((c) => c.name === "DescribeSecretCommand"));
  assert.deepEqual(updates(f).map((u) => u.service), ["fixture-consumer"]);
});

test("validation and migration gate every service update", async () => {
  const f = fixture();
  await run(f, config());
  const firstUpdate = f.calls.findIndex((c) => c.name === "UpdateServiceCommand");
  const taskRuns = f.calls.map((c, i) => [c, i]).filter(([c]) => c.name === "RunTaskCommand");
  assert.deepEqual(taskRuns.slice(0, 2).map(([c]) => /fixture-(\w+):/.exec(c.input.taskDefinition)[1]), ["validation", "migration"]);
  assert.ok(taskRuns[1][1] < firstUpdate);
  assert.deepEqual(updates(f).map((u) => u.service), ["fixture-backend", "fixture-consumer", "fixture-forms", "fixture-tracking", "fixture-realtime"]);
  assert.ok(updates(f).every((u) => !Object.hasOwn(u, "desiredCount")));
});

for (const [failure, message] of [
  [{ taskFailure: "migration" }, /Migration failed/], [{ taskFailure: "validation" }, /Dependency check failed/],
  [{ placementFailure: true }, /task-placement failure/], [{ noVariant: true }, /no warmbly-image-variant tag/],
  [{ registry: "docker.io/someone" }, /does not run a digest from ghcr/],
]) {
  test(`${JSON.stringify(failure)} leaves every service untouched`, async () => {
    const f = fixture(failure);
    await assert.rejects(run(f, config()), message);
    assert.equal(updates(f).length, 0);
  });
}

test("a failed migration prints its log tail", async () => {
  const f = fixture({ taskFailure: "migration" });
  const lines = [];
  await assert.rejects(run(f, config(), lines), /Migration failed/);
  assert.ok(lines.some((l) => l.includes("log line from migration/migration/")));
});

test("an older tag is refused unless the mode is rollback, and rollback skips the migration", async () => {
  await assert.rejects(run(fixture({ liveTag: "v1.4.0" }), config()), /older than the live v1.4.0/);
  await assert.rejects(run(fixture(), config({ DEPLOY_MODE: "rollback" })), /newer than the live/);
  const f = fixture({ liveTag: "v1.4.0" });
  await run(f, config({ DEPLOY_MODE: "rollback" }));
  assert.ok(!runs(f).some((r) => r.taskDefinition.includes("fixture-migration")));
  assert.ok(runs(f).some((r) => r.overrides), "rollback still moves the fleet");
});

test("a revision that drops a secret the live service reads is refused", async () => {
  const f = fixture({ dropReadKeys: true });
  await assert.rejects(run(f, config()), /would lose CREDENTIALS_ENCRYPTION_READ_KEYS/);
  assert.equal(registered(f).length, 0);
  const g = fixture({ dropReadKeys: true });
  await run(g, config({ DEPLOY_ALLOW_DROPPED_SECRETS: "true" }));
});

test("an alarm during the bake restores every updated service in reverse order", async () => {
  const f = fixture({ alarm: "consumer" });
  await assert.rejects(run(f, config()), /fixture-consumer-5xx/);
  const restores = updates(f).filter((u) => u.taskDefinition.endsWith(":1"));
  assert.deepEqual(restores.map((u) => u.service), ["fixture-consumer", "fixture-backend"]);
  assert.ok(restores.every((u) => u.desiredCount === 2));
  assert.ok(!updates(f).some((u) => u.service === "fixture-forms"));
});

test("a failure in the parallel wave waits for its siblings and then restores all of them", async () => {
  const f = fixture({ rolloutFailure: "forms" });
  await assert.rejects(run(f, config()), /forms deployment failed/);
  const forward = updates(f).filter((u) => !u.taskDefinition.endsWith(":1")).map((u) => u.service);
  const restores = updates(f).filter((u) => u.taskDefinition.endsWith(":1")).map((u) => u.service);
  assert.equal(forward.length, 5);
  assert.deepEqual([...restores].sort(), [...forward].sort());
  assert.deepEqual(restores.slice(-2), ["fixture-consumer", "fixture-backend"]);
});

test("a circuit breaker rollback cannot be reported as a successful deployment", async () => {
  const f = fixture({ circuitBreaker: "backend" });
  await assert.rejects(run(f, config()), /backend deployment rolled back/);
});

test("the fleet rollout gets the worker and consumer digests per variant", async () => {
  const f = fixture();
  await run(f, config());
  const ops = runs(f).find((r) => r.overrides);
  const command = ops.overrides.containerOverrides[0].command;
  assert.deepEqual(command.slice(0, 4), ["fleet", "rollout", "start", "v1.3.0"]);
  assert.equal(command[command.indexOf("--bake") + 1], "300s");
  assert.equal(command[command.indexOf("--digests") + 1], `worker=${d("b")},worker-kafka=${d("c")},consumer=${d("b")},consumer-kafka=${d("c")}`);
  assert.equal(ops.overrides.containerOverrides[0].name, "ops");
});

test("hosted fleet rollout carries a configured longer bake", async () => {
  const f = fixture();
  await run(f, config({ DEPLOY_FLEET_BAKE_SECONDS: "1800" }));
  const command = runs(f).find((r) => r.overrides).overrides.containerOverrides[0].command;
  assert.equal(command[command.indexOf("--bake") + 1], "1800s");
});

test("fleet startup retries only confirmed image-pull failures before execution, with stable retry tokens", async () => {
  const pullFailure = { lastStatus: "STOPPED", stopCode: "TaskFailedToStart", stoppedReason: "CannotPullContainerError: manifest not found", containers: [{ name: "ops", runtimeId: "allocated-before-pull" }] };
  const success = { lastStatus: "STOPPED", stopCode: "EssentialContainerExited", containers: [{ name: "ops", exitCode: 0 }] };
  async function attempt(statuses, options = {}) {
    const requests = [], sleeps = [];
    const clients = {
      ecs: { async send(command) {
        if (command.constructor.name === "RunTaskCommand") {
          requests.push(command.input);
          return { tasks: [{ taskArn: `${root}task/fixture/${requests.length}` }] };
        }
        return { tasks: [statuses[Math.min(requests.length - 1, statuses.length - 1)]] };
      } },
      logs: { async send() { return { events: [] }; } },
    };
    const execute = () => runTask(clients, config(), {}, "fixture-ops:3", definition("ops", 3, {}), "Fleet rollout", async (ms) => sleeps.push(ms), () => {}, { command: ["fleet", "rollout", "start", "v1.3.0"], imagePullRetries: 2, ...options });
    return { execute, requests, sleeps };
  }
  const recovered = await attempt([pullFailure, success]);
  await recovered.execute();
  assert.equal(recovered.requests.length, 2);
  assert.deepEqual(recovered.sleeps, [30000]);
  assert.deepEqual(recovered.requests[0].overrides, recovered.requests[1].overrides);
  assert.equal(recovered.requests[0].taskDefinition, recovered.requests[1].taskDefinition);
  assert.notEqual(recovered.requests[0].clientToken, recovered.requests[1].clientToken);
  const replay = await attempt([pullFailure, success]);
  await replay.execute();
  assert.deepEqual(replay.requests.map((r) => r.clientToken), recovered.requests.map((r) => r.clientToken));
  const exhausted = await attempt([pullFailure]);
  await assert.rejects(exhausted.execute(), /Fleet rollout failed/);
  assert.equal(exhausted.requests.length, 3);
  assert.deepEqual(exhausted.sleeps, [30000, 60000]);
  for (const failure of [
    { ...success, containers: [{ name: "ops", exitCode: 1 }] },
    { ...pullFailure, startedAt: new Date() },
    { ...pullFailure, containers: [{ name: "ops", exitCode: 1 }] },
    { ...pullFailure, containers: [] },
    { ...pullFailure, stoppedReason: "ResourceInitializationError: unavailable" },
  ]) {
    const failed = await attempt([failure]);
    await assert.rejects(failed.execute(), /Fleet rollout failed/);
    assert.equal(failed.requests.length, 1);
  }
  const migration = await attempt([pullFailure], { imagePullRetries: 0 });
  await assert.rejects(migration.execute(), /failed/);
  assert.equal(migration.requests.length, 1);
});

test("an unchanged definition reuses its revision and redeploy forces a fresh deployment", async () => {
  const f = fixture();
  await run(f, config({ DEPLOY_SERVICES: "forms", DEPLOY_FLEET: "false" }));
  const formsArn = updates(f)[0].taskDefinition;
  f.services.find((s) => s.serviceName === "fixture-forms").taskDefinition = formsArn;
  const before = registered(f).length;
  await run(f, config({ DEPLOY_SERVICES: "forms", DEPLOY_MODE: "redeploy", DEPLOY_FLEET: "false" }));
  assert.equal(registered(f).length, before);
  assert.deepEqual(updates(f).at(-1), { cluster: root + "cluster/fixture", service: "fixture-forms", taskDefinition: formsArn, forceNewDeployment: true });
});

test("no secret value or version-free reference is ever read", async () => {
  const f = fixture();
  await run(f, config());
  assert.ok(!f.calls.some((c) => c.name === "GetSecretValueCommand"));
});

test("an empty bake setting means the default, not no bake", () => {
  assert.equal(config({ DEPLOY_BAKE_SECONDS: "" }).bake, 300);
  assert.equal(config({ DEPLOY_BAKE_SECONDS: "  " }).bake, 300);
  assert.equal(config({ DEPLOY_BAKE_SECONDS: "0" }).bake, 0);
});

test("all deploys the services this install runs, and a named missing service is refused", async () => {
  const f = fixture({ absent: ["forms", "tracking", "realtime"] });
  await run(f, config());
  assert.deepEqual(updates(f).map((u) => u.service).sort(), ["fixture-backend", "fixture-consumer"]);
  await assert.rejects(run(fixture({ absent: ["forms"] }), config({ DEPLOY_SERVICES: "backend,forms" })), /forms must already exist/);
});
