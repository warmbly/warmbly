const { test } = require("node:test");
const assert = require("node:assert/strict");
const crypto = require("node:crypto");
const { verifyCredentialField, verifyStoredCredentials } = require("./credentials.cjs");

function seal(key, value, encoding) {
  const nonce = crypto.randomBytes(12);
  const cipher = crypto.createCipheriv("aes-256-gcm", key, nonce);
  return Buffer.concat([nonce, cipher.update(value), cipher.final(), cipher.getAuthTag()]).toString(encoding);
}

test("mailbox credentials verify with their owning organization or an overlapping instance reader", () => {
  const instance = crypto.randomBytes(32), previous = crypto.randomBytes(32), org = crypto.randomBytes(32);
  const organizations = new Map([["org-a", org], ["org-b", crypto.randomBytes(32)]]);
  const value = "dek1:" + seal(org, "fixture", "base64");
  verifyCredentialField({ mailbox: true, organization_id: "org-a", value }, [instance], organizations);
  verifyCredentialField({ mailbox: true, value: seal(previous, "fixture", "hex") }, [instance, previous], organizations);
  verifyCredentialField({ mailbox: true, value: "dek1:" }, [instance], organizations);
  assert.throws(() => verifyCredentialField({ mailbox: true, organization_id: "org-b", value }, [instance], organizations));
  assert.throws(() => verifyCredentialField({ mailbox: true, organization_id: "missing", value }, [instance], organizations));
  assert.throws(() => verifyCredentialField({ mailbox: false, value }, [instance], organizations));
  const raw = Buffer.from(value.slice(5), "base64");
  raw[raw.length - 1] ^= 1;
  assert.throws(() => verifyCredentialField({ mailbox: true, organization_id: "org-a", value: "dek1:" + raw.toString("base64") }, [instance], organizations));
});

test("stored validation unwraps keys, verifies both domains and MFA, and clears KMS plaintext", async () => {
  const instance = crypto.randomBytes(32), org = crypto.randomBytes(32), mfa = crypto.createHash("sha256").update("fixture-auth").digest();
  const env = { CREDENTIALS_ENCRYPTION_KEY: instance.toString("hex"), AUTH_SECRET: "fixture-auth" };
  class DecryptCommand { constructor(input) { this.input = input; } }
  let plaintext;
  const kms = { async send() { plaintext = Buffer.from(org); return { Plaintext: plaintext }; } };
  let corrupt = false;
  const db = { async query(sql) {
    const rows = sql.includes("FROM organization_encrypted_keys") ? [{ organization_id: "org-a", encrypted_data_key: "Zml4dHVyZQ==" }]
      : sql.includes("FROM user_totp_settings") ? [{ totp_secret_sealed: seal(mfa, "fixture", "base64") }]
        : [{ mailbox: true, organization_id: "org-a", value: "dek1:" + seal(corrupt ? instance : org, "fixture", "base64") },
          { mailbox: false, value: seal(instance, "fixture", "hex") }, { signing_secret: true, value: "whsec_fixture" }];
    return { rows, rowCount: rows.length };
  } };
  assert.deepEqual(await verifyStoredCredentials(db, kms, DecryptCommand, env), {
    credential_fields: 2, legacy_signing_fields: 1, mfa_enrollments: 1, organization_keys: 1, kms_decrypt_as_task_role: true,
  });
  assert.ok(plaintext.every((byte) => byte === 0));
  corrupt = true;
  await assert.rejects(verifyStoredCredentials(db, kms, DecryptCommand, env), /Ciphertext verification failed/);
  assert.ok(plaintext.every((byte) => byte === 0));
});
