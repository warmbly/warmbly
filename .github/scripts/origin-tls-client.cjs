const { spawnSync } = require("node:child_process");
const { existsSync } = require("node:fs");

const HOST = "api.warmbly.com";
const CA = "/etc/ssl/certs/ca-certificates.crt";
const CBC = ["ECDHE-RSA-AES128-SHA256", "ECDHE-RSA-AES256-SHA384"];
const GCM = ["ECDHE-RSA-AES128-GCM-SHA256", "ECDHE-RSA-AES256-GCM-SHA384"];
const TLS13 = "TLS_AES_128_GCM_SHA256";

function invoke(args, spawn = spawnSync) {
  const result = spawn("openssl", args, { encoding: "utf8", input: "", timeout: 12000,
    killSignal: "SIGKILL", maxBuffer: 128 * 1024 });
  return { at: new Date().toISOString(), command: ["openssl", ...args], status: result.status,
    signal: result.signal || null, error: result.error?.code || null,
    stdout: result.stdout || "", stderr: result.stderr || "" };
}

function classify(result, version, cipher, negative = false) {
  if (result.error || result.signal) return "inconclusive";
  const output = result.stdout + result.stderr;
  const negotiated = /New, (TLSv1\.[23]), Cipher is ([^\s]+)/.exec(output);
  if (negative) {
    if (/certificate verify failed|Verification error|verify error|no cipher match/i.test(output)) return "inconclusive";
    if (negotiated && negotiated[2] !== "(NONE)" && negotiated[2] !== "0000") return "accepted";
    return result.status !== 0 && /(?:sslv3|tlsv1) alert handshake failure|SSL alert number 40/i.test(output) &&
      /Cipher (?:is|:) (?:\(NONE\)|0000)/.test(output) ? "refused" : "inconclusive";
  }
  return result.status === 0 && negotiated?.[1] === version && negotiated[2] === cipher &&
    /Verify return code: 0 \(ok\)/.test(output) && /-----BEGIN CERTIFICATE-----/.test(output) ? "trusted" : "inconclusive";
}

function probe(targets, run = invoke, emit = (row) => console.log(JSON.stringify(row))) {
  if (!Array.isArray(targets) || targets.length !== 2 || new Set(targets).size !== 2 ||
    targets.some((ip) => !/^10\.42\.(?:0|1)\.(?:[1-9]\d?|1\d\d|2[0-4]\d|25[0-4])$/.test(ip))) throw new Error("Two pinned origin IPv4 targets required");
  const version = run(["version", "-a"]);
  emit({ kind: "client", ...version });
  if (version.status !== 0 || version.error || version.signal) throw new Error("OpenSSL unavailable");
  const support = run(["ciphers", "-s", "-tls1_2", CBC.join(":")]);
  emit({ kind: "support", ...support });
  if (support.status !== 0 || support.error || support.signal ||
    support.stdout.trim().split(":").sort().join(":") !== [...CBC].sort().join(":")) throw new Error("Both CBC offers must be locally supported");
  let success = true;
  for (const ip of targets) {
    const handshake = (cipher, negative = false) => {
      const tls13 = cipher === TLS13;
      const result = run(["s_client", "-connect", `${ip}:443`, "-servername", HOST, "-verify_hostname", HOST,
        "-verify_return_error", "-CAfile", CA, "-showcerts", tls13 ? "-tls1_3" : "-tls1_2",
        tls13 ? "-ciphersuites" : "-cipher", cipher]);
      const outcome = classify(result, tls13 ? "TLSv1.3" : "TLSv1.2", cipher, negative);
      emit({ kind: "handshake", ip, cipher, negative, outcome, ...result });
      return outcome;
    };
    const before = [handshake(GCM[0]), handshake(GCM[1]), handshake(TLS13)];
    const negatives = CBC.map((cipher) => handshake(cipher, true));
    const after = [handshake(GCM[0]), handshake(TLS13)];
    const passed = [...before, ...after].every((v) => v === "trusted") && negatives.every((v) => v === "refused");
    emit({ kind: "target", ip, outcome: passed ? "verified" : "inconclusive" });
    success = success && passed;
  }
  emit({ kind: "complete", success, at: new Date().toISOString() });
  return success;
}

if (process.env.ORIGIN_TLS_TASK === "1") {
  try {
    if (!existsSync(CA)) throw new Error("System public CA bundle missing");
    process.exitCode = probe(JSON.parse(process.env.ORIGIN_TLS_TARGETS)) ? 0 : 1;
  } catch (error) {
    console.log(JSON.stringify({ kind: "inconclusive", message: error.message }));
    process.exitCode = 1;
  }
}
module.exports = { HOST, CA, CBC, GCM, TLS13, invoke, classify, probe };
