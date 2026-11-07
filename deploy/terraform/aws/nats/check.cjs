const { randomUUID } = require("node:crypto");
const { connect, credsAuthenticator, StringCodec, nanos, RetentionPolicy, StorageType, DiscardPolicy, AckPolicy } = require("nats");

const timeout = setTimeout(() => { console.error(JSON.stringify({ ok: false, reason: "validation_timeout" })); process.exit(1); }, 300000);
const subject = "warmbly.verification." + randomUUID();
const durable = "verify-" + randomUUID();
const codec = StringCodec();
let nc, manager, consumerCreated = false;
let phase = "connect";

async function main() {
  nc = await connect({
    servers: process.env.NATS_URL.split(","),
    authenticator: credsAuthenticator(Buffer.from(process.env.NATS_CREDS_B64, "base64")),
    tls: { ca: Buffer.from(process.env.NATS_TLS_CA_B64, "base64").toString("utf8") },
    maxReconnectAttempts: -1,
    timeout: 10000,
  });
  manager = await nc.jetstreamManager();
  phase = "stream";
  const js = nc.jetstream();
  let info;
  try { info = await manager.streams.info("warmbly"); }
  catch (error) {
    if (error.code !== "404") throw error;
    info = await manager.streams.add({ name: "warmbly", subjects: ["warmbly.>"], num_replicas: 3, max_bytes: 64 * 1024 ** 3, max_age: nanos(7 * 86400 * 1000), retention: RetentionPolicy.Limits, storage: StorageType.File, discard: DiscardPolicy.New });
  }
  for (let attempt = 0; attempt < 20 && (info.cluster?.replicas?.length !== 2 || info.cluster.replicas.some((r) => !r.current)); attempt++) {
    await new Promise((resolve) => setTimeout(resolve, 500));
    info = await manager.streams.info("warmbly");
  }
  phase = "replicas";
  if (info.config.num_replicas !== 3 || info.cluster?.replicas?.length !== 2 || info.cluster.replicas.some((r) => !r.current)) throw new Error("replica_check_failed");
  phase = "consumer";
  await manager.consumers.add("warmbly", { durable_name: durable, filter_subject: subject, ack_policy: AckPolicy.Explicit, ack_wait: nanos(1000), max_deliver: 10 });
  consumerCreated = true;
  const consumer = await js.consumers.get("warmbly", durable);
  await js.publish(subject, codec.encode("redelivery"));
  const first = await consumer.next({ expires: 10000 });
  if (!first || codec.decode(first.data) !== "redelivery") throw new Error("delivery_failed");
  first.nak(1000);
  const retry = await consumer.next({ expires: 10000 });
  phase = "redelivery";
  if (!retry || !retry.info.redelivered || codec.decode(retry.data) !== "redelivery" || !(await retry.ackAck())) throw new Error("redelivery_failed");
  console.log(JSON.stringify({ phase: "ready_for_failover", tls_verified: true, replicas: 3, redelivery: true }));
  const count = Number(process.env.CHECK_MESSAGES || "180");
  for (let sequence = 0; sequence < count; sequence++) {
    await js.publish(subject, codec.encode(String(sequence)), { timeout: 15000 });
    const message = await consumer.next({ expires: 15000 });
    if (!message || codec.decode(message.data) !== String(sequence) || !(await message.ackAck())) throw new Error("ordered_delivery_failed");
    if (sequence % 30 === 0) console.log(JSON.stringify({ phase: "progress", acknowledged: sequence + 1 }));
    await new Promise((resolve) => setTimeout(resolve, 500));
  }
  info = await manager.streams.info("warmbly");
  console.log(JSON.stringify({ phase: "complete", ok: true, published: count, acknowledged: count, leader: info.cluster.leader, replicas_current: info.cluster.replicas.every((r) => r.current) }));
}

main().catch((error) => { console.error(JSON.stringify({ ok: false, phase, code: error.code || "validation_failed" })); process.exitCode = 1; }).finally(async () => {
  if (consumerCreated) await manager.consumers.delete("warmbly", durable).catch(() => {});
  await nc?.drain().catch(() => {});
  clearTimeout(timeout);
});
