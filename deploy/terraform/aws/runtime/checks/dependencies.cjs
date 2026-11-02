const crypto = require("node:crypto");
const dns = require("node:dns/promises");
const https = require("node:https");
const { Client } = require("pg");
const { Kafka, logLevel } = require("kafkajs");
const { createClient } = require("redis");
const { connect, credsAuthenticator } = require("nats");
const { KMSClient, DecryptCommand } = require("@aws-sdk/client-kms");
const { S3Client, ListObjectsV2Command } = require("@aws-sdk/client-s3");

const results = [];
const config = { region: process.env.AWS_REGION, maxAttempts: 2 };
const kms = new KMSClient(config);
const s3 = new S3Client(config);
const timeout = setTimeout(() => {
  console.error("Dependency validation exceeded its time limit");
  process.exit(1);
}, 240000);

async function check(name, fn) {
  try {
    const detail = await fn();
    results.push({ name, ok: true, ...detail });
    console.log(JSON.stringify(results.at(-1)));
  } catch {
    results.push({ name, ok: false });
    console.error(JSON.stringify(results.at(-1)));
  }
}

function readSecrets(variable) {
  const values = JSON.parse(process.env[variable] || "[]");
  if (!Array.isArray(values) || values.length > 4 || values.some((value) => typeof value !== "string" || !value)) throw new Error("Invalid read keys");
  return values;
}

function verifyCipher(keys, encoded, encoding) {
  const raw = Buffer.from(encoded, encoding);
  if (raw.length < 28) throw new Error("Invalid ciphertext");
  for (const key of keys) {
    const parts = [];
    try {
      const decipher = crypto.createDecipheriv("aes-256-gcm", key, raw.subarray(0, 12));
      decipher.setAuthTag(raw.subarray(raw.length - 16));
      parts.push(decipher.update(raw.subarray(12, raw.length - 16)));
      parts.push(decipher.final());
      return;
    } catch {
      // Try the next explicitly configured reader.
    } finally {
      parts.forEach((part) => part.fill(0));
    }
  }
  throw new Error("Ciphertext verification failed");
}

async function withDatabase(connectionString, fn) {
  const client = new Client({ connectionString, connectionTimeoutMillis: 10000, query_timeout: 20000 });
  client.on("error", () => {});
  try {
    await client.connect();
    await client.query("BEGIN READ ONLY");
    await client.query("SET LOCAL statement_timeout = '15s'");
    return await fn(client);
  } finally {
    await client.end().catch(() => {});
  }
}

function requestJSON(url, headers = {}) {
  return new Promise((resolve, reject) => {
    const req = https.get(url, { headers, timeout: 15000 }, (res) => {
      let body = "";
      res.on("data", (chunk) => { body += chunk; });
      res.on("end", () => {
        if (res.statusCode !== 200) return reject(new Error("Unexpected response"));
        try { resolve(JSON.parse(body)); } catch { reject(new Error("Unexpected JSON")); }
      });
    });
    req.on("timeout", () => req.destroy());
    req.on("error", reject);
  });
}

async function main() {
  await check("planetscale_private_dns", async () => {
    const host = new URL(process.env.PRIMARY_DB).hostname;
    const addresses = await dns.resolve4(host);
    if (!addresses.length || addresses.some((ip) => !ip.startsWith("10."))) throw new Error("Private DNS required");
    return { private_addresses: addresses.length };
  });

  for (const [name, variable] of [["backend_database", "PRIMARY_DB"], ["consumer_database", "CONSUMER_DB"], ["realtime_database", "REALTIME_DB"]]) {
    await check(name, () => withDatabase(process.env[variable], async (db) => {
      const out = await db.query("SELECT count(*)::int AS readable_tables FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND c.relkind='r' AND has_table_privilege(c.oid,'SELECT')");
      const version = await db.query("SELECT version,dirty FROM schema_migrations");
      if (!out.rows[0].readable_tables || version.rows.some((r) => r.dirty)) throw new Error("Database validation failed");
      return { readable_tables: out.rows[0].readable_tables, migration_clean: true };
    }));
  }

  await check("stored_credentials_and_kms", () => withDatabase(process.env.PRIMARY_DB, async (db) => {
    const keys = [process.env.CREDENTIALS_ENCRYPTION_KEY, ...readSecrets("CREDENTIALS_ENCRYPTION_READ_KEYS")].map((value) => Buffer.from(value, "hex"));
    const mfaKeys = [process.env.TWOFA_SECRET || process.env.AUTH_SECRET, ...readSecrets("TWOFA_READ_SECRETS")].map((value) => crypto.createHash("sha256").update(value).digest());
    try {
      const fields = await db.query("SELECT v AS value,false AS signing_secret FROM email_accounts_smtp_imap CROSS JOIN LATERAL (VALUES (smtp_host),(smtp_user),(smtp_password),(imap_host),(imap_user),(imap_password)) fields(v) WHERE v IS NOT NULL AND v<>'' UNION ALL SELECT v,false FROM email_accounts_oauth CROSS JOIN LATERAL (VALUES(access_token),(refresh_token)) fields(v) WHERE v IS NOT NULL AND v<>'' UNION ALL SELECT secret,true FROM webhook_endpoints WHERE secret IS NOT NULL AND secret<>'' UNION ALL SELECT webhook_secret,true FROM oauth_applications WHERE webhook_secret IS NOT NULL AND webhook_secret<>'' UNION ALL SELECT token,false FROM cloud_link WHERE token IS NOT NULL AND token<>'' UNION ALL SELECT refresh_token,false FROM mailbox_erasures WHERE refresh_token IS NOT NULL AND refresh_token<>''");
      let legacySigningFields = 0;
      for (const row of fields.rows) {
        if (row.signing_secret && row.value.startsWith("whsec_")) legacySigningFields++;
        else verifyCipher(keys, row.value, "hex");
      }
      const mfa = await db.query("SELECT totp_secret_sealed FROM user_totp_settings WHERE totp_secret_sealed IS NOT NULL AND totp_secret_sealed<>''");
      for (const row of mfa.rows) verifyCipher(mfaKeys, row.totp_secret_sealed, "base64");
      const wrapped = await db.query("SELECT encrypted_data_key FROM organization_encrypted_keys");
      if (!wrapped.rowCount) throw new Error("No data key available to verify");
      for (let offset = 0; offset < wrapped.rowCount; offset += 5) {
        await Promise.all(wrapped.rows.slice(offset, offset + 5).map(async (row) => {
          const out = await kms.send(new DecryptCommand({ CiphertextBlob: Buffer.from(row.encrypted_data_key, "base64") }));
          try { if (out.Plaintext?.length !== 32) throw new Error("Invalid data key"); }
          finally { out.Plaintext?.fill(0); }
        }));
      }
      return { credential_fields: fields.rowCount - legacySigningFields, legacy_signing_fields: legacySigningFields, mfa_enrollments: mfa.rowCount, organization_keys: wrapped.rowCount, kms_decrypt_as_task_role: true };
    } finally {
      [...keys, ...mfaKeys].forEach((key) => key.fill(0));
    }
  }));

  await check("existing_shared_cache", async () => {
    const client = createClient({ url: process.env.REDIS, socket: { connectTimeout: 10000, reconnectStrategy: false } });
    client.on("error", () => {});
    try { await client.connect(); if (await client.ping() !== "PONG") throw new Error("Cache check failed"); }
    finally { client.destroy(); }
    return {};
  });
  await check("private_valkey", async () => {
    const client = createClient({ password: process.env.NEW_REDIS_PASSWORD, socket: { host: process.env.CACHE_HOST, port: 6379, tls: true, servername: process.env.CACHE_HOST, rejectUnauthorized: true, connectTimeout: 10000, reconnectStrategy: false } });
    client.on("error", () => {});
    try { await client.connect(); if (await client.ping() !== "PONG") throw new Error("Cache check failed"); }
    finally { client.destroy(); }
    return { tls_verified: true };
  });
  const provider = process.env.EVENTBUS_PROVIDER || "kafka";
  const codec = process.env.CODEC_PROVIDER || "avro";
  if (!["kafka", "nats"].includes(provider) || !["avro", "json"].includes(codec)) throw new Error("Invalid messaging provider");
  const enabled = (value) => ["1", "t", "T", "true", "TRUE", "True"].includes(value);
  if (provider === "kafka" || enabled(process.env.EVENTBUS_READ_BOTH)) await check("kafka_access", async () => {
    const kafka = new Kafka({ clientId: "aws-migration-validation", brokers: process.env.KAFKA_BOOTSTRAP_SERVERS.split(","), ssl: true, sasl: { mechanism: "plain", username: process.env.KAFKA_SASL_USERNAME, password: process.env.KAFKA_SASL_PASSWORD }, logLevel: logLevel.NOTHING, connectionTimeout: 10000, requestTimeout: 15000, retry: { retries: 1 } });
    const admin = kafka.admin({ allowAutoTopicCreation: false });
    try { await admin.connect(); const out = await admin.fetchTopicMetadata({ topics: ["tracking-events"] }); return { existing_topics: out.topics.length }; }
    finally { await admin.disconnect(); }
  });
  if (codec === "avro" || enabled(process.env.CODEC_READ_BOTH)) await check("schema_registry_access", async () => {
    const authorization = "Basic " + Buffer.from(process.env.SCHEMA_REGISTRY_KEY + ":" + process.env.SCHEMA_REGISTRY_SECRET).toString("base64");
    const subjects = await requestJSON(process.env.SCHEMA_REGISTRY_URL.replace(/\/$/, "") + "/subjects", { authorization });
    if (!Array.isArray(subjects)) throw new Error("Invalid Schema Registry response");
    return { subjects: subjects.length };
  });
  if (provider === "nats" || enabled(process.env.EVENTBUS_READ_BOTH)) await check("private_nats", async () => {
    const nc = await connect({
      servers: process.env.NATS_URL.split(","),
      authenticator: credsAuthenticator(Buffer.from(process.env.NATS_CREDS_B64, "base64")),
      tls: { ca: Buffer.from(process.env.NATS_TLS_CA_B64, "base64").toString("utf8") },
      maxReconnectAttempts: 0, timeout: 10000,
    });
    try {
      const manager = await nc.jetstreamManager();
      const info = await manager.streams.info(process.env.NATS_STREAM_NAME || "warmbly");
      const expected = Number(process.env.NATS_REPLICAS || "3");
      if (info.config.num_replicas !== expected || (info.cluster?.replicas || []).length !== expected - 1 || (info.cluster?.replicas || []).some((replica) => !replica.current)) throw new Error("NATS replicas not current");
      return { tls_verified: true, replicas: expected, read_only: true };
    } finally { await nc.close(); }
  });
  await check("blob_bucket_as_task_role", async () => {
    await s3.send(new ListObjectsV2Command({ Bucket: process.env.BLOB_BUCKET, MaxKeys: 1 }));
    return {};
  });
  kms.destroy();
  s3.destroy();
  clearTimeout(timeout);
  console.log(JSON.stringify({ checks: results.length, passed: results.filter((r) => r.ok).length, production_writes: false }));
  process.exit(results.every((r) => r.ok) ? 0 : 1);
}

main().catch(() => { console.error("Dependency validation failed; credential values omitted"); process.exit(1); });
