const {
  ECSClient, DescribeServicesCommand, DescribeTasksCommand, UpdateServiceCommand, paginateListTasks,
} = require("@aws-sdk/client-ecs");
const { CloudWatchClient, DescribeAlarmsCommand } = require("@aws-sdk/client-cloudwatch");
const {
  configuration: deploymentConfiguration, describe, compareTags, register, same, waitForService,
  ATTACHMENT_PROXY, ATTACHMENT_PROXY_TAG,
} = require("./aws-deploy.cjs");

const tagValue = (tags, key) => tags.find((t) => t.key === key)?.value;
const canonicalTags = (tags) => JSON.stringify(tags.filter((t) => !t.key.startsWith("aws:")).sort((a, b) => a.key.localeCompare(b.key)));

function configuration(env) {
  const config = deploymentConfiguration({ ...env, DEPLOY_MODE: "redeploy", DEPLOY_SERVICES: "backend", DEPLOY_FLEET: "false", DEPLOY_BAKE_SECONDS: "300" });
  if (!config.tag || !config.images || config.images.tag !== config.tag || compareTags(config.tag, "v0.6.75") < 0) {
    throw new Error("A published v0.6.75 or newer release and its images.json are required");
  }
  const account = config.cluster.split(":")[4];
  const expected = env.CONFIG_EXPECTED_TASK_DEFINITION || "";
  const expectedPrefix = `arn:aws:ecs:us-east-1:${account}:task-definition/${config.prefix}-backend:`;
  if (!expected.startsWith(expectedPrefix) || !/^[1-9]\d*$/.test(expected.slice(expectedPrefix.length))) {
    throw new Error("CONFIG_EXPECTED_TASK_DEFINITION must identify this cluster's backend revision");
  }
  if (!["true", "false"].includes(env.CONFIG_ATTACHMENT_DOWNLOAD_PROXY)) throw new Error("CONFIG_ATTACHMENT_DOWNLOAD_PROXY must be true or false");
  return { ...config, expected, value: env.CONFIG_ATTACHMENT_DOWNLOAD_PROXY };
}

async function service(ecs, config) {
  const out = await ecs.send(new DescribeServicesCommand({ cluster: config.cluster, services: [`${config.prefix}-backend`] }));
  const live = out.services?.[0];
  if (out.failures?.length || out.services?.length !== 1 || live.status !== "ACTIVE") throw new Error("Backend service is unavailable");
  return live;
}

function stable(live, expected, count) {
  if (live.taskDefinition !== expected || live.desiredCount < 1 || live.runningCount !== live.desiredCount || live.pendingCount !== 0 ||
      live.deployments?.length !== 1 || live.deployments[0].rolloutState !== "COMPLETED" ||
      (count !== undefined && live.desiredCount !== count)) throw new Error("Backend must be stable on the expected revision and task count");
}

async function verifyTasks(ecs, config, live, definition) {
  stable(live, definition.taskDefinitionArn);
  const arns = [];
  for await (const page of paginateListTasks({ client: ecs, pageSize: 100, stopOnSameToken: true }, {
    cluster: config.cluster, serviceName: live.serviceName, desiredStatus: "RUNNING",
  })) arns.push(...(page.taskArns || []));
  if (new Set(arns).size !== live.desiredCount || arns.length !== live.desiredCount) throw new Error("Every running backend task must be accounted for");
  for (let offset = 0; offset < arns.length; offset += 100) {
    const batch = arns.slice(offset, offset + 100);
    const out = await ecs.send(new DescribeTasksCommand({ cluster: config.cluster, tasks: batch }));
    if (out.failures?.length || out.tasks?.length !== batch.length || new Set(out.tasks.map((t) => t.taskArn)).size !== batch.length) {
      throw new Error("Every running backend task must be described");
    }
    for (const task of out.tasks) {
      if (!batch.includes(task.taskArn) || task.taskDefinitionArn !== live.taskDefinition || task.group !== `service:${live.serviceName}` ||
          task.lastStatus !== "RUNNING" || task.desiredStatus !== "RUNNING" || task.containers?.length !== 1 || task.containers[0].name !== "backend" ||
          task.containers[0].image !== definition.containerDefinitions[0].image ||
          (definition.containerDefinitions[0].healthCheck && task.healthStatus !== "HEALTHY") ||
          task.overrides?.containerOverrides?.some((c) => Object.keys(c).some((k) => k !== "name"))) {
        throw new Error("Every backend task must use the verified image and unmodified service configuration");
      }
    }
  }
}

async function alarms(clients, config) {
  const out = await clients.cloudwatch.send(new DescribeAlarmsCommand({ AlarmNamePrefix: `${config.prefix}-backend-`, StateValue: "ALARM" }));
  if (out.MetricAlarms?.length || out.CompositeAlarms?.length) throw new Error("Backend alarm fired during configuration rollout");
}

async function observe(clients, config, definition, count, sleep, report) {
  for (let waited = 0; waited <= 300; waited += 15) {
    stable(await service(clients.ecs, config), definition.taskDefinitionArn, count);
    await alarms(clients, config);
    if (waited < 300) await sleep(15000);
  }
  await verifyTasks(clients.ecs, config, await service(clients.ecs, config), definition);
  report("Backend configuration baked for 300s; every running task verified");
}

async function configure(clients, config, sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms)), report = console.log) {
  const live = await service(clients.ecs, config);
  stable(live, live.taskDefinition);
  const source = await describe(clients.ecs, live.taskDefinition);
  const definition = source.definition;
  const container = definition.containerDefinitions?.[0];
  const variant = tagValue(source.tags, "warmbly-image-variant");
  if (tagValue(source.tags, "warmbly-release") !== config.tag || !["default", "kafka"].includes(variant) ||
      definition.family !== `${config.prefix}-backend` || definition.containerDefinitions?.length !== 1 || container.name !== "backend") {
    throw new Error("Live backend must identify the selected release, family and image variant");
  }
  const digest = config.images.images?.[`backend${variant === "kafka" ? "-kafka" : ""}`];
  if (config.images.registry !== "ghcr.io/warmbly/warmbly" || !/^sha256:[a-f0-9]{64}$/.test(digest || "") ||
      container.image !== `${config.images.registry}/backend@${digest}`) throw new Error("Live backend image must match the attested release digest");
  if (container.secrets?.some((s) => s.name === ATTACHMENT_PROXY)) throw new Error("Attachment proxy flag must not be secret-backed");
  const values = (container.environment || []).filter((e) => e.name === ATTACHMENT_PROXY);
  const managed = tagValue(source.tags, ATTACHMENT_PROXY_TAG);
  if (values.length > 1 || (values.length && !["true", "false"].includes(values[0].value)) ||
      (managed !== undefined && (values.length !== 1 || managed !== values[0].value))) throw new Error("Attachment proxy configuration is ambiguous");
  await verifyTasks(clients.ecs, config, live, definition);
  await alarms(clients, config);
  if ((values[0]?.value || "false") === config.value) {
    report("Backend already has the requested attachment configuration; no change");
    await observe(clients, config, definition, live.desiredCount, sleep, report);
    return { taskDefinition: live.taskDefinition, previous: live.taskDefinition, changed: false };
  }
  if (live.taskDefinition !== config.expected) throw new Error("Expected backend revision changed; inspect it before requesting a configuration change");
  const rendered = structuredClone(definition);
  rendered.containerDefinitions[0].environment = (container.environment || []).filter((e) => e.name !== ATTACHMENT_PROXY);
  rendered.containerDefinitions[0].environment.push({ name: ATTACHMENT_PROXY, value: config.value });
  const tags = source.tags.filter((t) => t.key !== ATTACHMENT_PROXY_TAG);
  tags.push({ key: ATTACHMENT_PROXY_TAG, value: config.value });
  const latest = await describe(clients.ecs, definition.family);
  const prepared = same(latest.definition, rendered) && canonicalTags(latest.tags) === canonicalTags(tags);
  const next = prepared ? latest.definition.taskDefinitionArn : await register(clients.ecs, source, rendered, tags, config.tag, report);
  stable(await service(clients.ecs, config), live.taskDefinition, live.desiredCount);
  try {
    await clients.ecs.send(new UpdateServiceCommand({ cluster: config.cluster, service: live.serviceName, taskDefinition: next }));
    await waitForService(clients.ecs, config, "backend", next, sleep);
    await observe(clients, config, { ...rendered, taskDefinitionArn: next }, live.desiredCount, sleep, report);
    return { taskDefinition: next, previous: live.taskDefinition, changed: true };
  } catch (error) {
    try {
      const current = await service(clients.ecs, config);
      if (![live.taskDefinition, next].includes(current.taskDefinition) || current.desiredCount !== live.desiredCount) {
        throw new Error("Backend changed concurrently; automatic rollback refused");
      }
      if (current.taskDefinition === next) await clients.ecs.send(new UpdateServiceCommand({ cluster: config.cluster, service: live.serviceName, taskDefinition: live.taskDefinition }));
      await waitForService(clients.ecs, config, "backend", live.taskDefinition, sleep);
      await verifyTasks(clients.ecs, config, await service(clients.ecs, config), definition);
      report("Restored and verified the exact previous backend revision");
    } catch {
      throw new Error("Configuration rollout failed and rollback could not be verified; inspect the backend before any further change", { cause: error });
    }
    throw error;
  }
}

if (require.main === module) {
  const region = { region: "us-east-1", maxAttempts: 3 };
  const clients = { ecs: new ECSClient(region), cloudwatch: new CloudWatchClient(region) };
  const lines = [];
  const report = (line) => { console.log(line); lines.push(line); };
  Promise.resolve().then(() => configure(clients, configuration(process.env), undefined, report)).catch((error) => {
    const message = error?.$metadata ? `AWS request failed: ${error.name}` : error.message;
    report(`Failed: ${message}`);
    process.exitCode = 1;
  }).finally(() => {
    if (process.env.GITHUB_STEP_SUMMARY) require("node:fs").appendFileSync(process.env.GITHUB_STEP_SUMMARY, `### Backend configuration\n\n\`\`\`\n${lines.join("\n")}\n\`\`\`\n`);
    for (const client of Object.values(clients)) client.destroy();
  });
}

module.exports = { configuration, configure };
