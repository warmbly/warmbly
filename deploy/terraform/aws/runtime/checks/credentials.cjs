function credentialReadKeys(env, variable) {
  const values = JSON.parse(env[variable] || "[]");
  if (!Array.isArray(values) || values.length > 4 || values.some((value) => typeof value !== "string" || !value)) throw new Error("Invalid read keys");
  return values;
}

function verifyCredentialCipher(keys, encoded, encoding) {
  const crypto = require("node:crypto");
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

function verifyCredentialField(row, instanceKeys, organizationKeys) {
  if (row.mailbox && row.value.startsWith("dek1:")) {
    const body = row.value.slice(5);
    if (!body) return;
    const key = organizationKeys.get(row.organization_id);
    if (!key) throw new Error("Organization key unavailable");
    verifyCredentialCipher([key], body, "base64");
  } else {
    verifyCredentialCipher(instanceKeys, row.value, "hex");
  }
}

async function verifyStoredCredentials(db, kms, DecryptCommand, env) {
  const crypto = require("node:crypto");
  const keys = [env.CREDENTIALS_ENCRYPTION_KEY, ...credentialReadKeys(env, "CREDENTIALS_ENCRYPTION_READ_KEYS")].map((value) => Buffer.from(value, "hex"));
  const mfaKeys = [env.TWOFA_SECRET || env.AUTH_SECRET, ...credentialReadKeys(env, "TWOFA_READ_SECRETS")].map((value) => crypto.createHash("sha256").update(value).digest());
  const organizationKeys = new Map();
  try {
    const wrapped = await db.query("SELECT organization_id,encrypted_data_key FROM organization_encrypted_keys");
    if (!wrapped.rowCount) throw new Error("No data key available to verify");
    for (let offset = 0; offset < wrapped.rowCount; offset += 5) {
      const batch = await Promise.allSettled(wrapped.rows.slice(offset, offset + 5).map(async (row) => {
        const out = await kms.send(new DecryptCommand({ CiphertextBlob: Buffer.from(row.encrypted_data_key, "base64") }));
        try {
          if (out.Plaintext?.length !== 32) throw new Error("Invalid data key");
          organizationKeys.set(row.organization_id, Buffer.from(out.Plaintext));
        } finally { out.Plaintext?.fill(0); }
      }));
      if (batch.some((result) => result.status === "rejected")) throw new Error("Organization key verification failed");
    }
    await db.query(`DECLARE credential_fields NO SCROLL CURSOR FOR
      SELECT v AS value,true AS mailbox,false AS signing_secret,a.organization_id
      FROM email_accounts_smtp_imap m JOIN email_accounts a ON a.id=m.email_account_id
      CROSS JOIN LATERAL (VALUES (smtp_host),(smtp_user),(smtp_password),(imap_host),(imap_user),(imap_password)) fields(v)
      WHERE v IS NOT NULL AND v<>''
      UNION ALL
      SELECT v,true,false,a.organization_id
      FROM email_accounts_oauth m JOIN email_accounts a ON a.id=m.email_account_id
      CROSS JOIN LATERAL (VALUES(access_token),(refresh_token)) fields(v)
      WHERE v IS NOT NULL AND v<>''
      UNION ALL SELECT secret,false,true,NULL::uuid FROM webhook_endpoints WHERE secret IS NOT NULL AND secret<>''
      UNION ALL SELECT webhook_secret,false,true,NULL::uuid FROM oauth_applications WHERE webhook_secret IS NOT NULL AND webhook_secret<>''
      UNION ALL SELECT token,false,false,NULL::uuid FROM cloud_link WHERE token IS NOT NULL AND token<>''
      UNION ALL SELECT refresh_token,true,false,organization_id FROM mailbox_erasures WHERE refresh_token IS NOT NULL AND refresh_token<>''
    `);
    let legacySigningFields = 0, credentialFields = 0;
    for (;;) {
      const fields = await db.query("FETCH FORWARD 1000 FROM credential_fields");
      if (!fields.rowCount) break;
      credentialFields += fields.rowCount;
      for (const row of fields.rows) {
        if (row.signing_secret && row.value.startsWith("whsec_")) legacySigningFields++;
        else verifyCredentialField(row, keys, organizationKeys);
      }
    }
    await db.query("CLOSE credential_fields");
    const mfa = await db.query("SELECT totp_secret_sealed FROM user_totp_settings WHERE totp_secret_sealed IS NOT NULL AND totp_secret_sealed<>''");
    for (const row of mfa.rows) verifyCredentialCipher(mfaKeys, row.totp_secret_sealed, "base64");
    return { credential_fields: credentialFields - legacySigningFields, legacy_signing_fields: legacySigningFields, mfa_enrollments: mfa.rowCount, organization_keys: wrapped.rowCount, kms_decrypt_as_task_role: true };
  } finally {
    [...keys, ...mfaKeys, ...organizationKeys.values()].forEach((key) => key.fill(0));
  }
}

module.exports = { verifyCredentialCipher, verifyCredentialField, verifyStoredCredentials };
