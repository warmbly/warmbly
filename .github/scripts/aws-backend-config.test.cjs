const { test } = require("node:test");
const assert = require("node:assert/strict");
const { mkdtempSync, writeFileSync, rmSync } = require("node:fs");
const { tmpdir } = require("node:os");
const { join } = require("node:path");
const { configuration, configure } = require("./aws-backend-config.cjs");
const { REGISTER_FIELDS, ATTACHMENT_PROXY, ATTACHMENT_PROXY_TAG } = require("./aws-deploy.cjs");

const root = "arn:aws:ecs:us-east-1:000000000000:";
const arn = (revision) => `${root}task-definition/fixture-backend:${revision}`;
const task = (number) => `${root}task/fixture/${number.toString(16).padStart(32, "0")}`;
const runningTasks = (revision, count = 2) => Array.from({ length: count }, (_, i) => task(revision * 1000 + i + 1));
const image = `ghcr.io/warmbly/warmbly/backend@sha256:${"a".repeat(64)}`;

function fixture(t, options = {}) {
  const count = options.taskCount || 2;
  const dir = mkdtempSync(join(tmpdir(), "backend-config-"));
  t.after(() => rmSync(dir, { recursive: true, force: true }));
  const manifest = { tag: "v0.6.75", registry: "ghcr.io/warmbly/warmbly", images: { "backend-kafka": `sha256:${"a".repeat(64)}` } };
  writeFileSync(join(dir, "images.json"), JSON.stringify(manifest));
  const env = { AWS_ECS_CLUSTER: root + "cluster/fixture", DEPLOY_TAG: manifest.tag, DEPLOY_IMAGES_JSON: join(dir, "images.json"),
    CONFIG_EXPECTED_TASK_DEFINITION: arn(1), CONFIG_EXPECTED_TASKS: JSON.stringify(runningTasks(1, count)), CONFIG_ATTACHMENT_DOWNLOAD_PROXY: "true" };
  const cfg = configuration(env);
  const live = { serviceName: "fixture-backend", status: "ACTIVE", taskDefinition: arn(1), desiredCount: count, runningCount: count, pendingCount: 0,
    deployments: [{ rolloutState: "COMPLETED", taskDefinition: arn(1), desiredCount: count, runningCount: count, pendingCount: 0 }] };
  const base = { family: "fixture-backend", status: "ACTIVE", revision: 1, taskDefinitionArn: arn(1),
    taskRoleArn: root + "role/task", executionRoleArn: root + "role/execution", networkMode: "awsvpc", cpu: "256", memory: "512",
    requiresCompatibilities: ["FARGATE"], runtimePlatform: { cpuArchitecture: "ARM64", operatingSystemFamily: "LINUX" },
    ephemeralStorage: { sizeInGiB: 30 }, volumes: [{ name: "fixture" }],
    containerDefinitions: [{ name: "backend", image, stopTimeout: 120, readonlyRootFilesystem: true,
      environment: [{ name: "TLS_ONLY", value: "true" }],
      secrets: [{ name: "PRIMARY_DB", valueFrom: "fixture:immutable-version" }],
      healthCheck: { command: ["CMD", "/app/healthcheck"], interval: 30, retries: 3 },
      logConfiguration: { logDriver: "awslogs", options: { "awslogs-group": "/fixture" } } }] };
  const tags = [{ key: "warmbly-release", value: manifest.tag }, { key: "warmbly-image-variant", value: "kafka" }, { key: "Owner", value: "fixture" }];
  const defs = new Map([[arn(1), { definition: base, tags }]]);
  const calls = [];
  const sleeps = [];
  let latest = arn(1);
  const ecs = {};
  ecs.send = async (command) => {
    const name = command.constructor.name;
    calls.push({ name, input: structuredClone(command.input) });
    if (options.hook) await options.hook(name, { live, defs, calls });
    switch (name) {
    case "DescribeServicesCommand": return { services: [structuredClone(live)] };
    case "DescribeTaskDefinitionCommand": {
      const entry = defs.get(command.input.taskDefinition === "fixture-backend" ? latest : command.input.taskDefinition);
      return { taskDefinition: structuredClone(entry.definition), tags: structuredClone(entry.tags) };
    }
    case "DescribeTasksCommand": return { tasks: (options.missingTask ? command.input.tasks.slice(1) : command.input.tasks).map((taskArn) => ({
      taskArn, taskDefinitionArn: options.wrongTask ? arn(99) : live.taskDefinition,
      group: options.foreignGroup ? "service:fixture-foreign" : "service:fixture-backend", lastStatus: "RUNNING",
      desiredStatus: options.drainingTask || !runningTasks(Number(live.taskDefinition.split(":").pop()), count).includes(taskArn) ? "STOPPED" : "RUNNING",
      healthStatus: options.unhealthyTask ? "UNHEALTHY" : "HEALTHY",
      containers: [{ name: "backend", image }], overrides: { containerOverrides: options.override ? [{ name: "backend", command: ["other"] }] : [{ name: "backend" }] },
    })) };
    case "RegisterTaskDefinitionCommand": {
      latest = arn(defs.size + 1);
      const { tags: nextTags, ...input } = structuredClone(command.input);
      if (options.reorderEnvironment) input.containerDefinitions[0].environment.reverse();
      defs.set(latest, { definition: { ...input, status: "ACTIVE", taskDefinitionArn: latest }, tags: nextTags });
      return { taskDefinition: { taskDefinitionArn: latest } };
    }
    case "UpdateServiceCommand": {
      live.taskDefinition = command.input.taskDefinition;
      live.deployments = [{ rolloutState: options.failedRoll && live.taskDefinition !== arn(1) ? "FAILED" : "COMPLETED",
        taskDefinition: live.taskDefinition, desiredCount: count, runningCount: count, pendingCount: 0 }];
      return {};
    }
    default: throw new Error(`Unexpected ${name}`);
    }
  };
  const cloudwatch = { async send(command) {
    calls.push({ name: command.constructor.name, input: structuredClone(command.input) });
    return { MetricAlarms: options.alarm && live.taskDefinition !== arn(1) ? [{ AlarmName: "fixture-backend-5xx" }] : [] };
  } };
  const run = () => configure({ ecs, cloudwatch }, cfg, async (ms) => sleeps.push(ms), () => {});
  const prepare = (definition, nextTags) => {
    latest = arn(defs.size + 1);
    defs.set(latest, { definition: { ...structuredClone(definition), taskDefinitionArn: latest }, tags: structuredClone(nextTags) });
  };
  return { env, cfg, live, base, tags, defs, calls, sleeps, run, prepare };
}

const mutations = (f) => f.calls.filter((c) => ["RegisterTaskDefinitionCommand", "UpdateServiceCommand"].includes(c.name));

test("only the allowlisted flag and ownership tag change; every live task is checked and a full bake completes", async (t) => {
  const f = fixture(t);
  const previous = structuredClone(f.base);
  const result = await f.run();
  assert.equal(result.changed, true);
  const registration = f.calls.find((c) => c.name === "RegisterTaskDefinitionCommand").input;
  for (const field of REGISTER_FIELDS.filter((field) => field !== "containerDefinitions")) assert.deepEqual(registration[field], previous[field]);
  const container = structuredClone(registration.containerDefinitions[0]);
  assert.deepEqual(container.environment.pop(), { name: ATTACHMENT_PROXY, value: "true" });
  assert.deepEqual(container, previous.containerDefinitions[0]);
  assert.deepEqual(Object.fromEntries(registration.tags.map((tag) => [tag.key, tag.value])),
    Object.fromEntries([...f.tags, { key: ATTACHMENT_PROXY_TAG, value: "true" }].map((tag) => [tag.key, tag.value])));
  assert.deepEqual(f.calls.filter((c) => c.name === "UpdateServiceCommand").map((c) => c.input), [{ cluster: f.cfg.cluster, service: "fixture-backend", taskDefinition: arn(2) }]);
  assert.equal(f.sleeps.reduce((a, b) => a + b, 0), 300000);
  assert.equal(f.calls.filter((c) => c.name === "DescribeAlarmsCommand").length, 22);
  assert.ok(!f.calls.some((c) => c.name === "ListTasksCommand"), "the operation uses existing deployment permissions");
  const count = mutations(f).length;
  await assert.rejects(f.run(), /unmodified service configuration/, "an old task snapshot must be refreshed before retrying");
  f.cfg.tasks = runningTasks(2);
  assert.equal((await f.run()).changed, false);
  assert.equal(mutations(f).length, count, "repeated request is a no-op even with its original expected revision");
  assert.equal(f.sleeps.reduce((a, b) => a + b, 0), 600000, "an interrupted bake is always observed afresh without rolling again");
});

test("provider environment ordering preserves a successful full bake without rollback", async (t) => {
  const f = fixture(t, { reorderEnvironment: true });
  assert.equal((await f.run()).changed, true);
  assert.equal(f.live.taskDefinition, arn(2));
  assert.deepEqual(f.calls.filter((c) => c.name === "UpdateServiceCommand").map((c) => c.input.taskDefinition), [arn(2)]);
  assert.equal(f.sleeps.reduce((a, b) => a + b, 0), 300000);
});

test("a prepared definition with reordered environment bindings is reused", async (t) => {
  const f = fixture(t);
  const prepared = structuredClone(f.base);
  prepared.containerDefinitions[0].environment.unshift({ name: ATTACHMENT_PROXY, value: "true" });
  f.prepare(prepared, [...f.tags, { key: ATTACHMENT_PROXY_TAG, value: "true" }]);
  assert.equal((await f.run()).taskDefinition, arn(2));
  assert.ok(!f.calls.some((c) => c.name === "RegisterTaskDefinitionCommand"));
});

test("disabling is explicit and preserves the remainder of the running definition", async (t) => {
  const f = fixture(t);
  await f.run();
  f.cfg.expected = arn(2);
  f.cfg.tasks = runningTasks(2);
  f.cfg.value = "false";
  await f.run();
  const current = f.defs.get(arn(3));
  assert.equal(current.definition.containerDefinitions[0].environment.find((e) => e.name === ATTACHMENT_PROXY).value, "false");
  assert.equal(current.tags.find((tag) => tag.key === ATTACHMENT_PROXY_TAG).value, "false");
});

test("an enabled no-change request requires a deployment source that owns the setting", async (t) => {
  const f = fixture(t);
  f.prepare(f.base, f.tags);
  f.base.containerDefinitions[0].environment.push({ name: ATTACHMENT_PROXY, value: "true" });
  await assert.rejects(f.run(), /requires a management tag or an explicit Terraform value/);
  assert.deepEqual(mutations(f), []);
  assert.deepEqual(f.sleeps, []);
});

for (const value of ["true", "false"]) test(`an explicit Terraform ${value} value owns an enabled no-change request`, async (t) => {
  const f = fixture(t);
  const template = structuredClone(f.base);
  template.containerDefinitions[0].environment.push({ name: ATTACHMENT_PROXY, value });
  f.prepare(template, f.tags);
  f.base.containerDefinitions[0].environment.push({ name: ATTACHMENT_PROXY, value: "true" });
  assert.equal((await f.run()).changed, false);
  assert.deepEqual(mutations(f), []);
  assert.equal(f.sleeps.reduce((a, b) => a + b, 0), 300000);
});

test("a live managed enabled setting remains owned when the latest template omits it", async (t) => {
  const f = fixture(t);
  f.prepare(f.base, f.tags);
  f.base.containerDefinitions[0].environment.push({ name: ATTACHMENT_PROXY, value: "true" });
  f.tags.push({ key: ATTACHMENT_PROXY_TAG, value: "true" });
  assert.equal((await f.run()).changed, false);
  assert.deepEqual(mutations(f), []);
  assert.equal(f.sleeps.reduce((a, b) => a + b, 0), 300000);
});

test("an absent false setting needs no ownership change", async (t) => {
  const f = fixture(t);
  f.cfg.value = "false";
  assert.equal((await f.run()).changed, false);
  assert.deepEqual(mutations(f), []);
  assert.equal(f.sleeps.reduce((a, b) => a + b, 0), 300000);
});

test("every preflight task is described within the API batch limit", async (t) => {
  const f = fixture(t, { taskCount: 101 });
  await f.run();
  const descriptions = f.calls.filter((c) => c.name === "DescribeTasksCommand");
  assert.deepEqual(descriptions.map((c) => c.input.tasks.length), [100, 1]);
  assert.deepEqual(descriptions.flatMap((c) => c.input.tasks), f.cfg.tasks);
});

for (const [name, options, mutate] of [
  ["mixed task definitions", { wrongTask: true }], ["unhealthy task", { unhealthyTask: true }], ["task override", { override: true }],
  ["draining task", { drainingTask: true }],
  ["missing described task", { missingTask: true }], ["foreign task group", { foreignGroup: true }],
  ["incomplete preflight", {}, (f) => { f.cfg.tasks.pop(); }],
  ["duplicate preflight task", {}, (f) => { f.cfg.tasks[1] = f.cfg.tasks[0]; }],
  ["stale expected revision", {}, (f) => { f.cfg.expected = arn(99); }],
  ["wrong image", {}, (f) => { f.base.containerDefinitions[0].image += "bad"; }],
  ["wrong release", {}, (f) => { f.tags[0].value = "v0.6.74"; }],
  ["missing variant", {}, (f) => { f.tags.splice(1, 1); }],
  ["zero tasks", {}, (f) => { f.live.desiredCount = f.live.runningCount = 0; }],
  ["unknown desired count", {}, (f) => { f.live.desiredCount = f.live.runningCount = undefined; }],
  ["deployment count mismatch", {}, (f) => { f.live.deployments[0].runningCount = 1; }],
  ["deployment revision mismatch", {}, (f) => { f.live.deployments[0].taskDefinition = arn(99); }],
  ["active rollout", {}, (f) => { f.live.deployments[0].rolloutState = "IN_PROGRESS"; }],
  ["duplicate flag", {}, (f) => { f.base.containerDefinitions[0].environment.push(...["true", "false"].map((value) => ({ name: ATTACHMENT_PROXY, value }))); }],
  ["secret-backed flag", {}, (f) => { f.base.containerDefinitions[0].secrets.push({ name: ATTACHMENT_PROXY, valueFrom: "secret" }); }],
  ["mismatched ownership tag", {}, (f) => { f.tags.push({ key: ATTACHMENT_PROXY_TAG, value: "true" }); }],
]) test(`${name} is refused before mutations`, async (t) => {
  const f = fixture(t, options);
  mutate?.(f);
  await assert.rejects(f.run());
  assert.deepEqual(mutations(f), []);
});

for (const options of [{ alarm: true }, { failedRoll: true }]) test(`${JSON.stringify(options)} restores and verifies the exact prior revision`, async (t) => {
  const f = fixture(t, options);
  await assert.rejects(f.run());
  assert.equal(f.live.taskDefinition, arn(1));
  assert.deepEqual(f.calls.filter((c) => c.name === "UpdateServiceCommand").map((c) => c.input.taskDefinition), [arn(2), arn(1)]);
  assert.equal(f.calls.at(-1).name, "DescribeTaskDefinitionCommand");
});

test("a desired-count change during rollback prevents a successful restore report", async (t) => {
  const f = fixture(t, { alarm: true, hook: (name, { live, calls }) => {
    if (name === "DescribeServicesCommand" && calls.filter((c) => c.name === "UpdateServiceCommand").length === 2) {
      live.desiredCount = live.runningCount = 3;
      live.deployments[0].desiredCount = live.deployments[0].runningCount = 3;
    }
  } });
  await assert.rejects(f.run(), /rollback could not be verified/);
  assert.equal(f.live.taskDefinition, arn(1));
  assert.equal(f.live.desiredCount, 3, "another writer's count is preserved for operator inspection");
  assert.ok(f.calls.filter((c) => c.name === "UpdateServiceCommand").every((c) => !Object.hasOwn(c.input, "desiredCount")));
});

test("a concurrent change before update is preserved", async (t) => {
  const f = fixture(t, { hook: (name, { live, calls }) => {
    if (name === "DescribeServicesCommand" && calls.some((c) => c.name === "RegisterTaskDefinitionCommand")) live.taskDefinition = arn(99);
  } });
  await assert.rejects(f.run(), /expected revision/);
  assert.equal(f.calls.filter((c) => c.name === "UpdateServiceCommand").length, 0);
  assert.equal(f.live.taskDefinition, arn(99));
});

test("a concurrent change during the bake is never overwritten by rollback", async (t) => {
  const f = fixture(t, { hook: (name, { live, calls }) => {
    if (name === "DescribeServicesCommand" && calls.some((c) => c.name === "UpdateServiceCommand")) live.taskDefinition = arn(99);
  } });
  await assert.rejects(f.run(), /rollback could not be verified/);
  assert.equal(f.calls.filter((c) => c.name === "UpdateServiceCommand").length, 1);
  assert.equal(f.live.taskDefinition, arn(99));
});

test("a prepared identical revision is reused after interruption before the service update", async (t) => {
  let interrupt = true;
  const f = fixture(t, { hook: (name, { calls }) => {
    if (interrupt && name === "DescribeServicesCommand" && calls.some((c) => c.name === "RegisterTaskDefinitionCommand")) {
      interrupt = false;
      throw new Error("interrupted before update");
    }
  } });
  await assert.rejects(f.run(), /interrupted/);
  await f.run();
  assert.equal(f.calls.filter((c) => c.name === "RegisterTaskDefinitionCommand").length, 1);
});

test("configuration requires a supported release, exact backend ARN and explicit boolean", (t) => {
  const f = fixture(t);
  for (const overrides of [
    { DEPLOY_TAG: "v0.6.74" }, { DEPLOY_TAG: "" }, { CONFIG_ATTACHMENT_DOWNLOAD_PROXY: "yes" },
    { CONFIG_EXPECTED_TASK_DEFINITION: arn(1).replace("fixture-backend", "fixture-consumer") },
    { CONFIG_EXPECTED_TASK_DEFINITION: arn(1).replace("000000000000", "111111111111") },
    { CONFIG_EXPECTED_TASKS: "[]" }, { CONFIG_EXPECTED_TASKS: "null" },
    { CONFIG_EXPECTED_TASKS: JSON.stringify([task(1), task(1)]) },
    { CONFIG_EXPECTED_TASKS: JSON.stringify([task(1).replace("000000000000", "111111111111")]) },
    { CONFIG_EXPECTED_TASKS: JSON.stringify([task(1).replace("/fixture/", "/foreign/")]) },
  ]) assert.throws(() => configuration({ ...f.env, ...overrides }));
  assert.equal(configuration({ ...f.env, DEPLOY_BAKE_SECONDS: "0", DEPLOY_FLEET: "true" }).bake, 300);
  assert.equal(configuration(f.env).fleet, false);
});
