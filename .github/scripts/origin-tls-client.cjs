const { spawnSync } = require("node:child_process");
const { existsSync } = require("node:fs");

const HOST = "api.warmbly.com";
const CA = "/etc/ssl/certs/ca-certificates.crt";
const CBC = ["ECDHE-RSA-AES128-SHA256", "ECDHE-RSA-AES256-SHA384"];
const GCM = ["ECDHE-RSA-AES128-GCM-SHA256", "ECDHE-RSA-AES256-GCM-SHA384"];
const TLS13 = "TLS_AES_128_GCM_SHA256";
const TRACE_HEADER = /^(>>>|<<<) TLS (1\.[0-3]), (RecordHeader|Handshake|Alert) \[length ([a-f\d]{4})\](?:, ([^\r\n]+))?$/i;
const OMITTED_MESSAGE = "[TLS message omitted]";
const validTargets = (targets) => Array.isArray(targets) && targets.length >= 2 && new Set(targets).size === targets.length &&
  targets.every((ip) => typeof ip === "string" && /^10\.42\.(?:0|1)\.(?:[1-9]\d?|1\d\d|2[0-4]\d|25[0-4])$/.test(ip));

function sanitize(output) {
  let session = false;
  let ticket = false;
  return output.split("\n").flatMap((line) => {
    if (/^SSL-Session:/.test(line)) session = true;
    if (/^---$/.test(line)) session = false;
    if (/^(?:>>>|<<<) .*\bNewSessionTicket\b/.test(line)) {
      ticket = true;
      return [OMITTED_MESSAGE];
    }
    if (TRACE_HEADER.test(line)) ticket = false;
    if (ticket) return [];
    return !session || /^\s*Verify return code:/.test(line) ? [line] : [];
  }).join("\n");
}

function invoke(args, spawn = spawnSync) {
  const result = spawn("openssl", args, { encoding: "utf8", input: "", timeout: 12000,
    killSignal: "SIGKILL", maxBuffer: 128 * 1024 });
  return { at: new Date().toISOString(), command: ["openssl", ...args], status: result.status,
    signal: result.signal || null, error: result.error?.code || null,
    stdout: sanitize(result.stdout || ""), stderr: sanitize(result.stderr || "") };
}

function traceMessages(output) {
  const lines = output.split("\n");
  const messages = [];
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    if (/^[ \t]*[<>]/.test(line)) {
      const header = TRACE_HEADER.exec(line);
      if (!header || i === lines.length - 1 || (header[3] === "RecordHeader" ? header[5] : !header[5])) return null;
      let hex = "";
      while (i + 1 < lines.length && /^[ \t]+\S/.test(lines[i + 1])) {
        const body = lines[++i];
        if (i === lines.length - 1 || !/^[ \t]+(?:[a-f\d]{2}[ \t]+)*[a-f\d]{2}[ \t]*$/i.test(body)) return null;
        hex += body.replace(/[ \t]/g, "");
      }
      const bytes = Buffer.from(hex, "hex");
      if (!bytes.length || bytes.length !== parseInt(header[4], 16)) return null;
      messages.push({ direction: header[1], version: header[2], kind: header[3], name: header[5], bytes });
    } else if (line === OMITTED_MESSAGE || (/^[ \t]+\S/.test(line) && !/^[ \t]+Verify return code:/.test(line)) ||
      /\b(?:RecordHeader|Handshake|Alert) \[length\b/.test(line) ||
      /^[a-f\d]+(?:[ \t]|$)/i.test(line)) return null;
  }
  return messages;
}

function completeClientHelloTail(sent, offset) {
  if (offset + 2 > sent.length || sent[offset] !== 1 || sent[offset + 1] !== 0) return false;
  offset += 2;
  if (offset === sent.length) return true;
  if (offset + 2 > sent.length || offset + 2 + sent.readUInt16BE(offset) !== sent.length) return false;
  offset += 2;
  const extensions = new Set();
  while (offset < sent.length) {
    if (offset + 4 > sent.length) return false;
    const kind = sent.readUInt16BE(offset);
    const length = sent.readUInt16BE(offset + 2);
    if (extensions.has(kind) || offset + 4 + length > sent.length) return false;
    if (kind === 43) {
      if (length < 3 || sent[offset + 4] !== length - 1 || (length - 1) % 2) return false;
      let tls12 = false;
      for (let at = offset + 5; at < offset + 4 + length; at += 2) {
        if (sent.readUInt16BE(at) === 0x0303) tls12 = true;
      }
      if (!tls12) return false;
    }
    extensions.add(kind);
    offset += 4 + length;
  }
  return true;
}

function peerRefusal(stdout, stderr, cipher) {
  const trace = traceMessages(stdout);
  const diagnosticTrace = traceMessages(stderr);
  if (!trace || !diagnosticTrace || diagnosticTrace.length) return false;
  const messages = trace.filter((message) => message.kind !== "RecordHeader");
  if (messages.length !== 2 || ![2, 4].includes(trace.length)) return false;
  if (trace.length === 4 && !messages.every((message, i) => {
    const record = trace[i * 2];
    return trace[i * 2 + 1] === message && record.kind === "RecordHeader" && record.direction === message.direction &&
      record.bytes.length === 5 && record.bytes[0] === (i === 0 ? 22 : 21) &&
      record.bytes.readUInt16BE(1) === 0x0301 + Number(record.version.at(-1)) &&
      record.bytes.readUInt16BE(3) === message.bytes.length;
  })) return false;
  const [hello, alert] = messages;
  const sent = hello.bytes;
  const received = alert.bytes;
  if (hello.direction !== ">>>" || hello.version !== "1.2" || hello.kind !== "Handshake" || hello.name !== "ClientHello" ||
    sent.length < 41 || sent[0] !== 1 || sent[38] > 32 ||
    sent.readUIntBE(1, 3) !== sent.length - 4 || sent.readUInt16BE(4) !== 0x0303) return false;
  const offset = 39 + sent[38];
  if (offset + 2 > sent.length) return false;
  const length = sent.readUInt16BE(offset);
  const offers = sent.subarray(offset + 2, offset + 2 + length).toString("hex");
  const intended = { [CBC[0]]: "c027", [CBC[1]]: "c028" }[cipher];
  // The sole negotiable suite may be followed by the renegotiation signalling value.
  return intended && length >= 2 && offset + 2 + length < sent.length &&
    completeClientHelloTail(sent, offset + 2 + length) &&
    (offers === intended || offers === intended + "00ff") &&
    alert.direction === "<<<" && alert.version === "1.2" && alert.kind === "Alert" && alert.name === "fatal handshake_failure" &&
    received.equals(Buffer.from([2, 40]));
}

function classify(result, version, cipher, negative = false) {
  const output = result.stdout + result.stderr;
  const negotiated = /New, (TLSv1\.[23]), Cipher is ([^\s]+)/.exec(output);
  if (negative) {
    if (/certificate verify failed|Verification error|verify error|no cipher match|unexpected eof|reset by peer|connect:errno/i.test(output)) return "inconclusive";
    const deadline = result.error === "ETIMEDOUT" && result.status === null && (!result.signal || result.signal === "SIGKILL");
    if (negotiated && negotiated[2] !== "(NONE)" && negotiated[2] !== "0000" &&
      ((!result.error && !result.signal) || deadline)) return "accepted";
    if (result.error || result.signal) return "inconclusive";
    return result.status === 1 && peerRefusal(result.stdout || "", result.stderr || "", cipher) &&
      !/-----BEGIN CERTIFICATE-----/.test(output) && /no peer certificate available/.test(output) &&
      /New, \(NONE\), Cipher is \(NONE\)/.test(output) ? "refused" : "inconclusive";
  }
  if (result.error || result.signal) return "inconclusive";
  return result.status === 0 && negotiated?.[1] === version && negotiated[2] === cipher &&
    /Verify return code: 0 \(ok\)/.test(output) && /-----BEGIN CERTIFICATE-----/.test(output) ? "trusted" : "inconclusive";
}

function probe(targets, run = invoke, emit = (row) => console.log(JSON.stringify(row)), now = () => performance.now()) {
  if (!validTargets(targets)) throw new Error("Unique pinned origin IPv4 targets required");
  const started = now();
  const bounded = (args) => {
    // Reserve one 12-second invocation within 180 seconds and the outer 210-second task limit.
    if (now() - started >= 168000) throw new Error("Origin TLS client runtime budget exhausted");
    return run(args);
  };
  const version = bounded(["version", "-a"]);
  emit({ kind: "client", ...version });
  if (version.status !== 0 || version.error || version.signal) throw new Error("OpenSSL unavailable");
  const support = bounded(["ciphers", "-s", "-tls1_2", CBC.join(":")]);
  emit({ kind: "support", ...support });
  if (support.status !== 0 || support.error || support.signal ||
    support.stdout.trim().split(":").sort().join(":") !== [...CBC].sort().join(":")) throw new Error("Both CBC offers must be locally supported");
  let success = true;
  for (const ip of targets) {
    const handshake = (cipher, negative = false) => {
      const tls13 = cipher === TLS13;
      const result = bounded(["s_client", "-connect", `${ip}:443`, "-servername", HOST, "-verify_hostname", HOST,
        "-verify_return_error", "-CAfile", CA, "-showcerts", "-no_ticket",
        ...(negative ? ["-ign_eof", "-state", "-msg"] : []), tls13 ? "-tls1_3" : "-tls1_2",
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
module.exports = { HOST, CA, CBC, GCM, TLS13, invoke, classify, probe, validTargets };
