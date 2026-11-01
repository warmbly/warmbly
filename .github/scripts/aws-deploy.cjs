// Deploys one published release to the AWS control plane: render every task
// definition from the revision Terraform last registered, swap in the release
// digests, gate on a dependency check and the migration, roll the services in
// waves with a bake, then hand the workers a canary rollout. It never builds,
// never reads a secret value, and restores everything it changed on failure.
const {
  ECSClient, DescribeTaskDefinitionCommand, DescribeServicesCommand, RegisterTaskDefinitionCommand,
  RunTaskCommand, DescribeTasksCommand, UpdateServiceCommand,
} = require("@aws-sdk/client-ecs");
const { SecretsManagerClient, DescribeSecretCommand, ListSecretVersionIdsCommand } = require("@aws-sdk/client-secrets-manager");
const { CloudWatchClient, DescribeAlarmsCommand } = require("@aws-sdk/client-cloudwatch");
const { CloudWatchLogsClient, GetLogEventsCommand } = require("@aws-sdk/client-cloudwatch-logs");
const { createHash } = require("node:crypto");
const { readFileSync } = require("node:fs");

const SERVICES = ["backend", "consumer", "forms", "tracking", "realtime"];
const WAVES = [["backend"], ["consumer"], ["forms", "tracking", "realtime"]];
const MODES = ["deploy", "rollback", "redeploy", "refresh-secrets"];
const RELEASE_TAG = "warmbly-release";
const VARIANT_TAG = "warmbly-image-variant";
const TAG_RE = /^v(\d+)\.(\d+)\.(\d+)$/;
const DIGEST_RE = /^sha256:[a-f0-9]{64}$/;
const VERSION_RE = /^[A-Za-z0-9-]{32,64}$/;

function configuration(env) {
  const cluster = env.AWS_ECS_CLUSTER;
  const match = /^arn:aws:ecs:us-east-1:(\d{12}):cluster\/([\w-]+)$/.exec(cluster || "");
  if (!match) throw new Error("AWS_ECS_CLUSTER must be a us-east-1 cluster ARN");
  const prefix = env.AWS_ECS_SERVICE_PREFIX || match[2];
  if (!/^[\w-]+$/.test(prefix)) throw new Error("Invalid service prefix");
  const mode = env.DEPLOY_MODE || "deploy";
  if (!MODES.includes(mode)) throw new Error(`DEPLOY_MODE must be one of ${MODES.join(", ")}`);
  const tag = (env.DEPLOY_TAG || "").trim();
  if (tag && !TAG_RE.test(tag)) throw new Error("DEPLOY_TAG must be a stable vX.Y.Z tag; prereleases never deploy");
  if (!tag && (mode === "deploy" || mode === "rollback")) throw new Error(`${mode} needs DEPLOY_TAG`);
  const raw = (env.DEPLOY_SERVICES || "all").trim();
  const services = raw === "all" ? [...SERVICES] : raw.split(",").map((s) => s.trim()).filter(Boolean);
  if (!services.length || services.some((s) => !SERVICES.includes(s))) throw new Error(`DEPLOY_SERVICES must be all or a list of ${SERVICES.join(", ")}`);
  let secretVersions = {};
  if (env.DEPLOY_SECRET_VERSIONS) {
    secretVersions = JSON.parse(env.DEPLOY_SECRET_VERSIONS);
    for (const [k, v] of Object.entries(secretVersions)) {
      if (!SERVICES.includes(k) || !VERSION_RE.test(v)) throw new Error("DEPLOY_SECRET_VERSIONS maps a service to a secret version id");
    }
    if (mode !== "refresh-secrets") throw new Error("DEPLOY_SECRET_VERSIONS is only accepted with refresh-secrets");
  }
  const bake = Number(String(env.DEPLOY_BAKE_SECONDS ?? "").trim() || 300);
  if (!Number.isInteger(bake) || bake < 0 || bake > 3600) throw new Error("DEPLOY_BAKE_SECONDS must be 0 through 3600");
  const fleetBake = Number(String(env.DEPLOY_FLEET_BAKE_SECONDS ?? "").trim() || 300);
  if (!Number.isInteger(fleetBake) || fleetBake < 300 || fleetBake > 3600) throw new Error("DEPLOY_FLEET_BAKE_SECONDS must be 300 through 3600");
  return {
    cluster, prefix, mode, tag, services, allServices: raw === "all", secretVersions, bake, fleetBake,
    fleet: env.DEPLOY_FLEET !== "false",
    allowDroppedSecrets: env.DEPLOY_ALLOW_DROPPED_SECRETS === "true",
    images: env.DEPLOY_IMAGES_JSON ? JSON.parse(readFileSync(env.DEPLOY_IMAGES_JSON, "utf8")) : null,
    run: `${env.GITHUB_RUN_ID || "operator"}/${env.GITHUB_RUN_ATTEMPT || "1"}`,
  };
}

function compareTags(a, b) {
  const x = TAG_RE.exec(a).slice(1).map(Number);
  const y = TAG_RE.exec(b).slice(1).map(Number);
  for (let i = 0; i < 3; i++) if (x[i] !== y[i]) return x[i] - y[i];
  return 0;
}

function assertResponse(out) {
  if (out.failures?.length) throw new Error("ECS reported a resource or task-placement failure");
  return out;
}

const tagValue = (tags, key) => tags?.find((t) => t.key === key)?.value;

// parseRef splits arn:aws:secretsmanager:<region>:<acct>:secret:<name>:<key>:<stage>:<version>.
function parseRef(valueFrom) {
  const parts = (valueFrom || "").split(":");
  if (parts[2] !== "secretsmanager" || parts.length !== 10) return null;
  return { arn: parts.slice(0, 7).join(":"), key: parts[7], stage: parts[8], version: parts[9] };
}

// secretService names the service a secret belongs to: <prefix>/<service>-XXXXXX.
function secretService(arn, prefix) {
  const name = arn.split(":")[6] || "";
  const m = new RegExp(`^${prefix}/([a-z]+)-[A-Za-z0-9]{6}$`).exec(name);
  return m ? m[1] : null;
}

const REGISTER_FIELDS = [
  "family", "taskRoleArn", "executionRoleArn", "networkMode", "containerDefinitions", "volumes",
  "placementConstraints", "requiresCompatibilities", "cpu", "memory", "pidMode", "ipcMode",
  "proxyConfiguration", "inferenceAccelerators", "ephemeralStorage", "runtimePlatform", "enableFaultInjection",
];

async function describe(client, taskDefinition) {
  const out = await client.send(new DescribeTaskDefinitionCommand({ taskDefinition, include: ["TAGS"] }));
  if (!out.taskDefinition || out.taskDefinition.status !== "ACTIVE") throw new Error(`${taskDefinition} is not an active task definition`);
  return { definition: out.taskDefinition, tags: out.tags || [] };
}

async function resolveVersions(clients, config, refs) {
  const pins = {};
  for (const [arn, seen] of Object.entries(refs)) {
    const service = secretService(arn, config.prefix);
    if (config.secretVersions[service]) {
      pins[arn] = config.secretVersions[service];
      continue;
    }
    // Explicit versions mean a targeted roll; everyone else keeps the newest pin.
    if (config.mode === "refresh-secrets" && !Object.keys(config.secretVersions).length) {
      const out = await clients.secrets.send(new DescribeSecretCommand({ SecretId: arn }));
      const current = Object.entries(out.VersionIdsToStages || {}).find(([, stages]) => stages.includes("AWSCURRENT"));
      if (!current) throw new Error(`${service || "a"} secret has no AWSCURRENT version`);
      pins[arn] = current[0];
      continue;
    }
    // Rotation pins newer versions than Terraform's, so the newest pin anyone uses wins.
    const out = await clients.secrets.send(new ListSecretVersionIdsCommand({ SecretId: arn, IncludeDeprecated: true, MaxResults: 100 }));
    const created = new Map((out.Versions || []).map((v) => [v.VersionId, new Date(v.CreatedDate).getTime()]));
    const known = [...seen].filter((v) => created.has(v));
    if (!known.length) throw new Error(`no pinned version of the ${service || ""} secret still exists`);
    pins[arn] = known.sort((a, b) => created.get(b) - created.get(a))[0];
  }
  return pins;
}

function repin(container, pins) {
  for (const secret of container.secrets || []) {
    const ref = parseRef(secret.valueFrom);
    if (ref && ref.version && pins[ref.arn]) {
      secret.valueFrom = `${ref.arn}:${ref.key}:${ref.stage}:${pins[ref.arn]}`;
    }
  }
}

function same(a, b) {
  return JSON.stringify(REGISTER_FIELDS.map((f) => a[f] ?? null)) === JSON.stringify(REGISTER_FIELDS.map((f) => b[f] ?? null));
}

async function register(client, source, rendered, tags, release, report) {
  const carried = tags.filter((t) => t.key !== RELEASE_TAG && !t.key.startsWith("aws:"));
  if (release) carried.push({ key: RELEASE_TAG, value: release });
  if (same(source.definition, rendered) && tagValue(source.tags, RELEASE_TAG) === release) return source.definition.taskDefinitionArn;
  const input = {};
  for (const f of REGISTER_FIELDS) if (rendered[f] !== undefined && rendered[f] !== null) input[f] = rendered[f];
  input.tags = carried;
  const out = await client.send(new RegisterTaskDefinitionCommand(input));
  const arn = out.taskDefinition?.taskDefinitionArn;
  if (!arn) throw new Error(`registering ${rendered.family} returned no revision`);
  report(`Registered ${arn.split("/").pop()}`);
  return arn;
}

async function logTail(clients, definition, task, report, quiet) {
  const container = definition.containerDefinitions[0];
  const options = container.logConfiguration?.options || {};
  if (!options["awslogs-group"] || !options["awslogs-stream-prefix"]) return [];
  try {
    const out = await clients.logs.send(new GetLogEventsCommand({
      logGroupName: options["awslogs-group"],
      logStreamName: `${options["awslogs-stream-prefix"]}/${container.name}/${task.split("/").pop()}`,
      startFromHead: false, limit: 60,
    }));
    const lines = (out.events || []).map((e) => String(e.message).slice(0, 2000));
    if (!quiet) for (const line of lines) report(`  | ${line}`);
    return lines;
  } catch {
    report("  | (no log events could be read for this task)");
    return [];
  }
}

// runTask runs a one-off task to completion; capture may be a predicate the log tail must meet.
async function runTask(clients, config, network, arn, definition, label, sleep, report, options = {}) {
  const { command, environment, capture, imagePullRetries = 0, pullAttempt = 0 } = Array.isArray(options) ? { command: options } : options;
  const identity = `${config.run}/${arn}/${label}/${JSON.stringify(command || [])}/${JSON.stringify(environment || [])}`;
  const token = createHash("sha256").update(identity + (pullAttempt ? `/pull-retry-${pullAttempt}` : "")).digest("hex");
  const name = definition.containerDefinitions[0].name;
  const override = { name, ...(command ? { command } : {}), ...(environment ? { environment } : {}) };
  const started = assertResponse(await clients.ecs.send(new RunTaskCommand({
    cluster: config.cluster, taskDefinition: arn, launchType: "FARGATE", platformVersion: "1.4.0", count: 1,
    networkConfiguration: network, clientToken: token.slice(0, 64), startedBy: `${label.replace(/\W+/g, "-")}-${token.slice(0, 20)}`.slice(0, 128),
    ...(command || environment ? { overrides: { containerOverrides: [override] } } : {}),
  })));
  const task = started.tasks?.[0]?.taskArn;
  if (!task) throw new Error(`${label} task was not created`);
  report(`${label} task: ${task.split("/").pop()}`);
  for (let attempt = 0; attempt < 240; attempt++) {
    const out = assertResponse(await clients.ecs.send(new DescribeTasksCommand({ cluster: config.cluster, tasks: [task] })));
    const status = out.tasks?.[0];
    if (!status) throw new Error(`${label} task is missing`);
    if (status.lastStatus === "STOPPED") {
      const container = status.containers?.find((c) => c.name === name);
      const ok = status.stopCode === "EssentialContainerExited" && container?.exitCode === 0;
      let lines = !ok || command || capture ? await logTail(clients, definition, task, report, ok && capture) : [];
      if (!ok) {
        const neverStarted = !status.startedAt && status.containers?.length > 0 && status.containers.every((c) => c.exitCode === undefined);
        const pullFailure = status.stopCode === "TaskFailedToStart" && /^CannotPullContainerError:/.test(status.stoppedReason || "");
        if (neverStarted && pullFailure && pullAttempt < imagePullRetries) {
          report(`${label} image pull failed before execution; retry ${pullAttempt + 1}/${imagePullRetries} with the same definition and overrides`);
          await sleep(30000 * (pullAttempt + 1));
          return runTask(clients, config, network, arn, definition, label, sleep, report, { ...options, pullAttempt: pullAttempt + 1 });
        }
        throw new Error(`${label} failed (${status.stopCode || "unknown stop code"}); inspect stopped task ${task}; its log tail is above`);
      }
      // Log delivery can trail the task stopping by a few seconds.
      for (let retry = 0; typeof capture === "function" && !capture(lines) && retry < 6; retry++) {
        await sleep(5000);
        lines = await logTail(clients, definition, task, report, true);
      }
      return lines;
    }
    await sleep(5000);
  }
  throw new Error(`${label} is unresolved after 20 minutes. Inspect the task and the service state before retrying`);
}

async function waitForService(client, config, name, definition, sleep) {
  for (let attempt = 0; attempt < 180; attempt++) {
    const out = assertResponse(await client.send(new DescribeServicesCommand({ cluster: config.cluster, services: [`${config.prefix}-${name}`] })));
    const service = out.services?.[0];
    if (!service || service.deployments?.some((d) => d.rolloutState === "FAILED")) throw new Error(`${name} deployment failed`);
    if (service.deployments?.length === 1 && service.deployments[0].rolloutState === "COMPLETED") {
      if (service.taskDefinition !== definition) throw new Error(`${name} deployment rolled back`);
      if (service.runningCount === service.desiredCount && service.pendingCount === 0) return;
    }
    await sleep(5000);
  }
  throw new Error(`${name} deployment did not become healthy within 15 minutes`);
}

// bake watches a stable service and its alarms; ECS rolling back counts as failure.
async function bake(clients, config, name, definition, sleep, report) {
  for (let waited = 0; waited < config.bake; waited += 15) {
    const alarms = await clients.cloudwatch.send(new DescribeAlarmsCommand({ AlarmNamePrefix: `${config.prefix}-${name}-`, StateValue: "ALARM" }));
    const firing = (alarms.MetricAlarms || []).map((a) => a.AlarmName);
    if (firing.length) throw new Error(`${name} raised ${firing.join(", ")} during the bake`);
    const out = assertResponse(await clients.ecs.send(new DescribeServicesCommand({ cluster: config.cluster, services: [`${config.prefix}-${name}`] })));
    if (out.services?.[0]?.taskDefinition !== definition) throw new Error(`${name} was rolled back during the bake`);
    await sleep(15000);
  }
  if (config.bake) report(`${name} baked for ${config.bake}s with no alarm`);
}

function imageFor(config, key, fallback) {
  if (!config.images) return fallback;
  const digest = config.images.images?.[key];
  if (!DIGEST_RE.test(digest || "")) throw new Error(`images.json for ${config.tag} has no ${key} digest`);
  return `${config.images.registry}/${key.replace(/-kafka$/, "")}@${digest}`;
}

async function deploy(clients, config, sleep = (ms) => new Promise((r) => setTimeout(r, ms)), report = console.log) {
  const ecs = clients.ecs;
  const names = SERVICES.map((s) => `${config.prefix}-${s}`);
  const current = await ecs.send(new DescribeServicesCommand({ cluster: config.cluster, services: names }));
  if (current.failures?.some((f) => f.reason !== "MISSING")) throw new Error("ECS reported a resource or task-placement failure");
  const live = {};
  for (const s of current.services || []) if (s.status === "ACTIVE") live[s.serviceName.slice(config.prefix.length + 1)] = s;
  // all is every service this install runs; a service named explicitly has to exist.
  if (config.allServices) config = { ...config, services: SERVICES.filter((s) => live[s]) };
  for (const name of config.services) {
    if (!live[name]) throw new Error(`${name} must already exist as an ECS service`);
  }
  const backend = live.backend;
  if (!backend?.networkConfiguration?.awsvpcConfiguration) throw new Error("Backend task networking is missing");
  const liveDefs = {};
  for (const [name, s] of Object.entries(live)) liveDefs[name] = await describe(ecs, s.taskDefinition);

  const liveTag = tagValue(liveDefs.backend.tags, RELEASE_TAG);
  const tag = config.tag || liveTag;
  if (!tag) throw new Error("The live backend carries no release tag yet; pass DEPLOY_TAG");
  if (config.tag && liveTag && TAG_RE.test(liveTag) && compareTags(config.tag, liveTag) < 0 && config.mode !== "rollback") {
    throw new Error(`${config.tag} is older than the live ${liveTag}; use mode=rollback to go back`);
  }
  if (config.mode === "rollback" && liveTag && TAG_RE.test(liveTag) && compareTags(config.tag, liveTag) > 0) {
    throw new Error(`rollback target ${config.tag} is newer than the live ${liveTag}`);
  }
  if (config.images && config.images.tag !== tag) throw new Error(`images.json describes ${config.images.tag}, not ${tag}`);
  if (!config.images && config.mode !== "refresh-secrets") throw new Error("DEPLOY_IMAGES_JSON is required unless refreshing secrets");
  report(`Release ${tag} (${config.mode}); live backend reports ${liveTag || "no release"}`);

  const deployBackend = config.services.includes("backend");
  const families = [...config.services];
  const extra = {};
  for (const name of ["migration", "validation", "ops"]) {
    if (name === "migration" && (!deployBackend || config.mode === "rollback")) continue;
    try {
      extra[name] = await describe(ecs, `${config.prefix}-${name}`);
      families.push(name);
    } catch (error) {
      if (name === "migration") throw error;
      report(`No ${name} task definition; skipping it`);
    }
  }
  const sources = {};
  for (const name of config.services) sources[name] = await describe(ecs, `${config.prefix}-${name}`);
  Object.assign(sources, extra);

  const refs = {};
  const collect = (def) => {
    for (const c of def.containerDefinitions || []) for (const s of c.secrets || []) {
      const ref = parseRef(s.valueFrom);
      if (ref?.version) (refs[ref.arn] ||= new Set()).add(ref.version);
    }
  };
  for (const name of families) collect(sources[name].definition);
  for (const def of Object.values(liveDefs)) collect(def.definition);
  const pins = await resolveVersions(clients, config, refs);

  const variant = (name) => {
    const v = tagValue(sources[name].tags, VARIANT_TAG);
    if (v !== "kafka" && v !== "default") throw new Error(`${config.prefix}-${name} has no ${VARIANT_TAG} tag; apply the runtime Terraform first`);
    return v;
  };
  const backendKey = deployBackend || families.includes("ops") ? `backend${variant(deployBackend ? "backend" : "ops") === "kafka" ? "-kafka" : ""}` : null;
  const rendered = {};
  for (const name of families) {
    const def = structuredClone(sources[name].definition);
    const container = def.containerDefinitions?.[0];
    if (!container || def.containerDefinitions.length !== 1) throw new Error(`${name} must have exactly one container`);
    repin(container, pins);
    if (SERVICES.includes(name)) {
      if (container.name !== name) throw new Error(`${name} container is named ${container.name}`);
      const key = `${name}${variant(name) === "kafka" ? "-kafka" : ""}`;
      if (config.images && !container.image.startsWith(`${config.images.registry}/${name}@`)) {
        throw new Error(`${name} does not run a digest from ${config.images.registry}; fix its image in Terraform first`);
      }
      container.image = imageFor(config, key, liveDefs[name].definition.containerDefinitions[0].image);
    } else if (name !== "validation") {
      container.image = imageFor(config, backendKey, liveDefs.backend.definition.containerDefinitions[0].image);
    }
    rendered[name] = def;
  }

  for (const name of config.services) {
    const before = new Set((liveDefs[name].definition.containerDefinitions[0].secrets || []).map((s) => s.name));
    const after = new Set((rendered[name].containerDefinitions[0].secrets || []).map((s) => s.name));
    const dropped = [...before].filter((k) => !after.has(k));
    if (dropped.length && !config.allowDroppedSecrets) {
      throw new Error(`${name} would lose ${dropped.join(", ")}; restore them in Terraform or set DEPLOY_ALLOW_DROPPED_SECRETS=true`);
    }
  }
  if (rendered.migration) {
    const m = rendered.migration.containerDefinitions[0];
    const b = rendered.backend.containerDefinitions[0];
    if (m.image !== b.image || m.entryPoint?.join(" ") !== "/app/migrate") throw new Error("Migration must use /app/migrate from the exact backend image");
    const direct = b.secrets?.find((s) => s.name === "PRIMARY_DB_DIRECT");
    if (!direct || direct.valueFrom !== m.secrets?.find((s) => s.name === "PRIMARY_DB")?.valueFrom) throw new Error("Migration and backend must reference the same direct database secret version");
    if (!b.environment?.some((e) => e.name === "DB_MIGRATIONS_ON_START" && e.value === "false")) throw new Error("Backend startup migrations must be disabled for this deployment");
  }

  const arns = {};
  for (const name of families) arns[name] = await register(ecs, sources[name], rendered[name], sources[name].tags, tag, report);

  const network = backend.networkConfiguration;
  if (arns.validation) await runTask(clients, config, network, arns.validation, rendered.validation, "Dependency check", sleep, report);
  if (arns.migration) {
    await runTask(clients, config, network, arns.migration, rendered.migration, "Migration", sleep, report);
    report("Migration completed successfully");
  }

  const updated = [];
  try {
    for (const wave of WAVES) {
      const group = wave.filter((n) => config.services.includes(n));
      // allSettled: a sibling still rolling must finish before anything is restored.
      const results = await Promise.allSettled(group.map(async (name) => {
        const previous = live[name];
        const unchanged = previous.taskDefinition === arns[name];
        if (unchanged && config.mode !== "redeploy") {
          report(`${name} already runs ${arns[name].split("/").pop()}`);
          return;
        }
        updated.push(previous);
        await ecs.send(new UpdateServiceCommand({ cluster: config.cluster, service: previous.serviceName, taskDefinition: arns[name], forceNewDeployment: unchanged }));
        await waitForService(ecs, config, name, arns[name], sleep);
        report(`${name} is stable on ${arns[name].split("/").pop()}`);
        await bake(clients, config, name, arns[name], sleep, report);
      }));
      const failed = results.find((r) => r.status === "rejected");
      if (failed) throw failed.reason;
    }
  } catch (error) {
    for (const previous of updated.reverse()) {
      try {
        await ecs.send(new UpdateServiceCommand({ cluster: config.cluster, service: previous.serviceName, taskDefinition: previous.taskDefinition, desiredCount: previous.desiredCount }));
        report(`Restored ${previous.serviceName} to ${previous.taskDefinition.split("/").pop()}; verify its health`);
      } catch {
        report(`Rollback needs operator attention for ${previous.serviceName}`);
      }
    }
    throw error;
  }

  if (config.fleet && arns.ops && config.images && (config.mode === "deploy" || config.mode === "rollback")) {
    const digests = ["worker", "worker-kafka", "consumer", "consumer-kafka"]
      .filter((k) => DIGEST_RE.test(config.images.images?.[k] || ""))
      .map((k) => `${k}=${config.images.images[k]}`);
    const command = ["fleet", "rollout", "start", tag, "--canary", "1", "--waves", "100", "--bake", `${config.fleetBake}s`];
    if (digests.length) command.push("--digests", digests.join(","));
    await runTask(clients, config, network, arns.ops, rendered.ops, "Fleet rollout", sleep, report, { command, imagePullRetries: 2 });
    report(`Workers move to ${tag}: one canary, then all remaining hosts, with ${config.fleetBake}s of healthy uptime per stage plus update time; follow with \`warmblyctl fleet rollout status\``);
  }
  return { tag, arns, pins: Object.fromEntries(Object.entries(pins).map(([arn, v]) => [secretService(arn, config.prefix) || arn, v])) };
}

if (require.main === module) {
  const region = { region: "us-east-1", maxAttempts: 3 };
  const clients = { ecs: new ECSClient(region), secrets: new SecretsManagerClient(region), cloudwatch: new CloudWatchClient(region), logs: new CloudWatchLogsClient(region) };
  const lines = [];
  const report = (line) => { console.log(line); lines.push(line); };
  Promise.resolve().then(() => deploy(clients, configuration(process.env), undefined, report)).then((out) => {
    report(`Deployed ${out.tag}. Secret versions: ${Object.entries(out.pins).map(([k, v]) => `${k}=${v.slice(0, 8)}`).join(", ") || "none"}`);
  }).catch((error) => {
    const message = error?.$metadata ? `AWS request failed: ${error.name}` : error.message;
    console.error(message);
    lines.push(`**Failed:** ${message}`);
    process.exitCode = 1;
  }).finally(() => {
    if (process.env.GITHUB_STEP_SUMMARY) {
      require("node:fs").appendFileSync(process.env.GITHUB_STEP_SUMMARY, `### AWS deploy\n\n\`\`\`\n${lines.join("\n")}\n\`\`\`\n`);
    }
    for (const c of Object.values(clients)) c.destroy();
  });
}

module.exports = { configuration, deploy, runTask, describe, compareTags, parseRef, secretService, SERVICES };
