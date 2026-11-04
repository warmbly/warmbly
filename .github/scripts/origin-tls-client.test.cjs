const { test } = require("node:test");
const assert = require("node:assert/strict");
const { spawnSync } = require("node:child_process");
const { mkdtempSync, readFileSync, rmSync } = require("node:fs");
const { tmpdir } = require("node:os");
const { join } = require("node:path");
const { Worker } = require("node:worker_threads");
const { CBC, GCM, TLS13, HOST, CA, invoke, classify, probe } = require("./origin-tls-client.cjs");
const { refused } = require("./origin-tls-client.fixture.cjs");
const trusted = (cipher = GCM[0]) => ({ status: 0, stdout: `-----BEGIN CERTIFICATE-----\nfixture chain\n-----END CERTIFICATE-----\nNew, ${cipher === TLS13 ? "TLSv1.3" : "TLSv1.2"}, Cipher is ${cipher}\nVerify return code: 0 (ok)\n`, stderr: "" });

test("positive proof requires exact negotiation, chain, verification and successful bounded execution", () => {
  assert.equal(classify(trusted(), "TLSv1.2", GCM[0]), "trusted");
  for (const result of [
    { ...trusted(), status: 1 }, { ...trusted(), error: "ETIMEDOUT" }, { ...trusted(), signal: "SIGKILL" },
    { ...trusted(), stdout: trusted().stdout.replace("return code: 0 (ok)", "return code: 62 (hostname mismatch)") },
    { ...trusted(), stdout: trusted().stdout.replace("-----BEGIN CERTIFICATE-----", "") },
    trusted(GCM[1]), trusted(TLS13),
  ]) assert.equal(classify(result, "TLSv1.2", GCM[0]), "inconclusive");
});

test("only an explicit peer handshake refusal without negotiation establishes a negative", () => {
  assert.equal(classify(refused(), "TLSv1.2", CBC[0], true), "refused");
  assert.equal(classify(trusted(CBC[0]), "TLSv1.2", CBC[0], true), "accepted");
  for (const result of [
    { ...refused(), signal: "SIGTERM" }, { ...refused(), error: "ETIMEDOUT" },
    { status: 1, stdout: "", stderr: "Connection reset by peer" },
    { status: 1, stdout: "", stderr: "connect:errno=110" },
    { status: 1, stdout: "", stderr: "no cipher match" },
    { status: 1, stdout: "", stderr: "certificate verify failed" },
    { status: 1, stdout: "", stderr: "tlsv1 alert protocol version:SSL alert number 70" },
    { ...refused(), stdout: "", status: 0 },
  ]) assert.equal(classify(result, "TLSv1.2", CBC[0], true), "inconclusive");
});

function fixture(change = () => {}) {
  const rows = [];
  let handshakes = 0;
  const run = (args) => {
    if (args[0] === "version") return { status: 0, stdout: "OpenSSL 3.0 fixture", stderr: "" };
    if (args[0] === "ciphers") return { status: 0, stdout: CBC.join(":"), stderr: "" };
    handshakes++;
    const cipher = args.at(-1);
    const result = { ...(CBC.includes(cipher) ? refused(cipher) : trusted(cipher)), command: ["openssl", ...args] };
    change(result, handshakes);
    return result;
  };
  return { rows, run };
}

test("completed negative negotiations retain their outcome at the process deadline", () => {
  for (const cipher of CBC) {
    const negotiated = { ...trusted(cipher), status: null, error: "ETIMEDOUT", signal: "SIGKILL" };
    assert.equal(classify(negotiated, "TLSv1.2", cipher, true), "accepted");
    assert.equal(classify({ ...negotiated, signal: null }, "TLSv1.2", cipher, true), "accepted");
    for (const altered of [
      { ...negotiated, error: "ENOBUFS" }, { ...negotiated, error: "EIO" },
      { ...negotiated, error: null }, { ...negotiated, signal: "SIGTERM" }, { ...negotiated, status: 0 },
      { ...negotiated, stderr: "certificate verify failed" },
      { ...negotiated, stdout: refused(cipher).stdout },
    ]) assert.equal(classify(altered, "TLSv1.2", cipher, true), "inconclusive");
    assert.equal(classify(negotiated, "TLSv1.2", cipher), "inconclusive");
  }
  const f = fixture((row, index) => {
    if (index === 4) Object.assign(row, trusted(CBC[0]), { status: null, error: "ETIMEDOUT", signal: "SIGKILL" });
  });
  assert.equal(probe(["10.42.0.85", "10.42.1.12"], f.run, (row) => f.rows.push(row)), false);
  assert.equal(f.rows.find((row) => row.kind === "handshake" && row.negative).outcome, "accepted");
  assert.equal(f.rows.at(-1).success, false);
});

test("every private target has fresh trusted positives around both explicit CBC offers", () => {
  const f = fixture();
  assert.equal(probe(["10.42.0.85", "10.42.1.12"], f.run, (row) => f.rows.push(row)), true);
  const hs = f.rows.filter((r) => r.kind === "handshake");
  assert.equal(hs.length, 14);
  for (const ip of ["10.42.0.85", "10.42.1.12"]) {
    const own = hs.filter((r) => r.ip === ip);
    assert.deepEqual(own.map((r) => r.cipher), [GCM[0], GCM[1], TLS13, ...CBC, GCM[0], TLS13]);
    for (const row of own) {
      const args = row.command;
      for (const [flag, value] of [["-connect", `${ip}:443`], ["-servername", HOST], ["-verify_hostname", HOST], ["-CAfile", CA]]) {
        assert.equal(args[args.indexOf(flag) + 1], value);
      }
      assert.ok(args.includes("-verify_return_error"));
      assert.ok(args.includes("-showcerts"));
      assert.ok(args.includes("-no_ticket"));
      for (const flag of ["-ign_eof", "-state", "-msg"]) assert.equal(args.includes(flag), row.negative);
      assert.ok(args.includes(row.cipher === TLS13 ? "-tls1_3" : "-tls1_2"));
    }
  }
  assert.deepEqual(f.rows.at(-1).success, true);
});

test("all current private interfaces receive the full handshake sequence", () => {
  const targets = ["10.42.0.85", "10.42.1.12", "10.42.0.86", "10.42.1.13"];
  const f = fixture();
  assert.equal(probe(targets, f.run, (row) => f.rows.push(row)), true);
  const handshakes = f.rows.filter((r) => r.kind === "handshake");
  assert.equal(handshakes.length, 28);
  for (const ip of targets) {
    assert.deepEqual(handshakes.filter((r) => r.ip === ip).map((r) => r.cipher),
      [GCM[0], GCM[1], TLS13, ...CBC, GCM[0], TLS13]);
  }
  assert.equal(f.rows.at(-1).success, true);
});

test("larger target sets share one fixed runtime budget and cannot complete with partial evidence", () => {
  const f = fixture();
  let elapsed = 0;
  let calls = 0;
  const run = (args) => {
    calls++;
    elapsed += 12000;
    return f.run(args);
  };
  assert.throws(() => probe(["10.42.0.85", "10.42.1.12", "10.42.0.86", "10.42.1.13"],
    run, (row) => f.rows.push(row), () => elapsed), /runtime budget exhausted/);
  assert.equal(calls, 14);
  assert.equal(elapsed, 168000);
  assert.ok(!f.rows.some((r) => r.kind === "complete"));
});

test("timeout or broken trailing positive makes the whole run inconclusive", () => {
  for (const index of [4, 6, 14]) {
    const f = fixture((result, n) => { if (n === index) result.error = "ETIMEDOUT"; });
    assert.equal(probe(["10.42.0.85", "10.42.1.12"], f.run, (row) => f.rows.push(row)), false);
    assert.equal(f.rows.at(-1).success, false);
  }
});

test("local CBC support is checked before network traffic and arbitrary destinations are refused", () => {
  const f = fixture();
  assert.throws(() => probe(["1.1.1.1", "10.42.1.12"], f.run), /pinned/);
  assert.throws(() => probe(["10.42.0.85", "10.42.0.85"], f.run), /pinned/);
  assert.throws(() => probe(["10.42.0.85", "10.42.1.12"], (args) => ({ status: 0, stdout: args[0] === "version" ? "OpenSSL" : CBC[0] }), () => {}), /Both CBC/);
});

test("OpenSSL processes get empty stdin, a hard timeout, bounded output and no shell", () => {
  const result = invoke(["version"], (command, args, options) => {
    assert.equal(command, "openssl");
    assert.deepEqual(args, ["version"]);
    assert.equal(options.input, "");
    assert.equal(options.timeout, 12000);
    assert.equal(options.killSignal, "SIGKILL");
    assert.equal(options.shell, undefined);
    assert.equal(options.maxBuffer, 128 * 1024);
    return { status: null, error: { code: "ETIMEDOUT" }, signal: "SIGKILL" };
  });
  assert.equal(result.error, "ETIMEDOUT");
  assert.equal(result.signal, "SIGKILL");
});

test("negative proof binds inbound alert bytes to the sole transmitted CBC suite", () => {
  for (const cipher of CBC) assert.equal(classify(refused(cipher), "TLSv1.2", cipher, true), "refused");
  for (const change of [
    (s) => s.replace("<<<", ">>>"),
    (s) => s.replace("02 28", "01 00"),
    (s) => s.replace("fatal handshake_failure", "warning close_notify"),
    (s) => s.replace("c0 27", "c0 28"),
    (s) => s.replace("c0 27", "c0 2f"),
    (s) => s.replace("00 04 c0 27 00 ff", "00 04 c0 27 c0 2f"),
    (s) => s.replace("length 002f", "length 0030"),
    (s) => s.replace("01 00 00 2b", "01 00 00 2c"),
    (s) => s.replace("New, (NONE)", "-----BEGIN CERTIFICATE-----\nNew, (NONE)"),
    (s) => s.replace("no peer certificate available", ""),
    (s) => s + "Connection reset by peer\n",
    (s) => s + "unexpected eof while reading\n",
    (s) => s + "<<< TLS 1.2, Handshake [length 0004], ServerHello\n    02 00 00 00\n",
    (s) => s.replace(/>>>[\s\S]*?<<< /, "<<< "),
  ]) assert.equal(classify({ ...refused(), stdout: change(refused().stdout) }, "TLSv1.2", CBC[0], true), "inconclusive");
  for (const status of [0, null, 2]) assert.equal(classify({ ...refused(), status }, "TLSv1.2", CBC[0], true), "inconclusive");
  for (const stdout of ["CONNECTED\nclosed\nno peer certificate available\nNew, (NONE), Cipher is (NONE)\n",
    "SSL handshake has read 7 bytes\nNew, (NONE), Cipher is (NONE)\n"]) {
    assert.equal(classify({ status: 0, stdout, stderr: "" }, "TLSv1.2", CBC[0], true), "inconclusive");
  }
});

test("archived output retains certificate and refusal evidence without session secrets or ticket bodies", () => {
  const proof = trusted().stdout + refused().stdout;
  const session = "SSL-Session:\n    Protocol  : TLSv1.2\n    Master-Key: SECRET_KEY\n    Resumption PSK: SECRET_PSK\n    TLS session ticket:\n    0000 - ab cd ef   ...\n    Verify return code: 0 (ok)\n---\n";
  const ticket = "<<< TLS 1.2, Handshake [length 0004], NewSessionTicket\n    04 00 00 00\n";
  const result = invoke(["version"], () => ({ status: 0, stdout: proof + session + ticket + refused().stdout, stderr: session }));
  assert.ok(result.stdout.includes(proof));
  assert.ok(result.stdout.includes("Verify return code: 0 (ok)"));
  assert.ok(result.stdout.endsWith(refused().stdout));
  assert.doesNotMatch(result.stdout + result.stderr, /SECRET|Master-Key|Resumption PSK|session ticket|NewSessionTicket|04 00 00 00|ab cd ef/);
});

test("captured ticket and session regions retain only approved evidence at every cutoff", () => {
  const prefix = trusted().stdout;
  const header = "<<< TLS 1.2, Handshake [length 000e], NewSessionTicket\n";
  const body = "    04 00 00 0a 71 82 93 a4\n    b5 c6 d7 e8 f9 0a\ncapture-tail";
  const resumed = "<<< TLS 1.2, Alert [length 0002], warning close_notify\n    01 00\n";
  const session = "SSL-Session:\n    Master-Key: SYNTHETIC_MAPLE\n    Resumption PSK: SYNTHETIC_BIRCH\n";
  for (const stream of ["stdout", "stderr"]) {
    for (const execution of [{ status: 0 }, { status: null, error: { code: "ENOBUFS" } },
      { status: null, error: { code: "ETIMEDOUT" }, signal: "SIGKILL" }]) {
      const capture = (output) => invoke([], () => ({ stdout: "", stderr: "", ...execution, [stream]: output }));
      for (let cutoff = 0; cutoff <= body.length; cutoff++) {
        const output = prefix + header + body.slice(0, cutoff);
        assert.equal(capture(output)[stream], prefix + "[TLS message omitted]", `${stream} ticket cutoff ${cutoff}`);
        assert.equal(capture(output + "\n" + resumed)[stream], prefix + "[TLS message omitted]\n" + resumed,
          `${stream} trace recovery ${cutoff}`);
      }
      for (let cutoff = 0; cutoff <= session.length; cutoff++) {
        const output = prefix + session.slice(0, cutoff);
        if (cutoff < "SSL-Session:\n".length) continue;
        assert.equal(capture(output)[stream], prefix.trimEnd(), `${stream} session cutoff ${cutoff}`);
      }
      const verification = "    Verify return code: 0 (ok)\n";
      assert.equal(capture(prefix + session + verification + "---\n" + resumed)[stream],
        prefix + verification + "---\n" + resumed);
      assert.equal(classify(capture(prefix + session + verification), "TLSv1.2", GCM[0]),
        execution.status === 0 ? "trusted" : "inconclusive");
    }
  }
});

test("loopback TLS and alert peers preserve strict refusal and trusted positive behavior", { timeout: 20000 }, async (t) => {
  const dir = mkdtempSync(join(tmpdir(), "origin-tls-local-"));
  t.after(() => rmSync(dir, { recursive: true, force: true }));
  const cert = join(dir, "cert.pem");
  const key = join(dir, "key.pem");
  const generated = spawnSync("openssl", ["req", "-x509", "-newkey", "rsa:2048", "-nodes", "-keyout", key,
    "-out", cert, "-days", "1", "-subj", "/CN=localhost", "-addext", "subjectAltName=DNS:localhost"], { timeout: 10000, encoding: "utf8" });
  assert.equal(generated.status, 0, generated.stderr);
  const f = fixture();
  probe(["10.42.0.85", "10.42.1.12"], f.run, (row) => f.rows.push(row));
  for (const mode of ["tls", "tls_held", "fatal", "close_notify", "eof", "reset", "silent"]) {
    await t.test(mode, async () => {
      const worker = new Worker(`
        const { parentPort, workerData: d } = require("node:worker_threads");
        const net = require("node:net");
        const tls = require("node:tls");
        const server = d.mode.startsWith("tls") ? tls.createServer({ key: d.key, cert: d.cert,
          minVersion: "TLSv1.2", maxVersion: "TLSv1.2", ciphers: d.cipher }, (s) => {
            s.on("error", () => {});
            if (d.mode === "tls") s.end();
          }) : net.createServer((s) => {
          let input = Buffer.alloc(0);
          s.on("error", () => {});
          s.on("data", (b) => {
            input = Buffer.concat([input, b]);
            if (input.length < 5 || input.length < 5 + input.readUInt16BE(3)) return;
            if (d.mode === "silent") return;
            if (d.mode === "reset") { s.resetAndDestroy(); return; }
            setTimeout(() => s.end(d.mode === "eof" ? Buffer.alloc(0) :
              Buffer.from([21, 3, 3, 0, 2, ...(d.mode === "fatal" ? [2, 40] : [1, 0])])), 50);
          });
        });
        server.on("tlsClientError", () => {});
        server.listen(0, "127.0.0.1", () => parentPort.postMessage(server.address().port));
      `, { eval: true, workerData: { mode, key: readFileSync(key), cert: readFileSync(cert), cipher: mode === "tls_held" ? CBC.join(":") : GCM[0] } });
      try {
        const port = await new Promise((resolve, reject) => { worker.once("message", resolve); worker.once("error", reject); });
        const localArgs = (cipher) => {
          const args = f.rows.find((r) => r.kind === "handshake" && r.cipher === cipher).command.slice(1);
          for (const [flag, value] of [["-connect", `127.0.0.1:${port}`], ["-servername", "localhost"],
            ["-verify_hostname", "localhost"], ["-CAfile", cert]]) args[args.indexOf(flag) + 1] = value;
          return args;
        };
        for (const cipher of CBC) {
          const result = invoke(localArgs(cipher), (command, args, options) => spawnSync(command, args,
            { ...options, timeout: mode === "tls_held" ? 500 : mode === "silent" ? 300 : options.timeout }));
          assert.equal(classify(result, "TLSv1.2", cipher, true), mode === "tls_held" ? "accepted" : ["tls", "fatal"].includes(mode) ? "refused" : "inconclusive",
            JSON.stringify(result));
          if (mode === "tls_held") {
            assert.equal(result.error, "ETIMEDOUT");
            assert.equal(result.signal, "SIGKILL");
            assert.ok(result.stdout.includes(`New, TLSv1.2, Cipher is ${cipher}`));
            assert.match(result.stdout, /Verify return code: 0 \(ok\)/);
          }
          if (mode === "close_notify") {
            assert.match(result.stdout, /<<< TLS 1.2, Alert \[length 0002\], warning close_notify\n\s+01 00/);
            const summaryOnly = invoke(localArgs(cipher).filter((arg) => !["-ign_eof", "-state", "-msg"].includes(arg)));
            assert.equal(summaryOnly.status, 0);
            assert.match(summaryOnly.stdout, /SSL handshake has read 7 bytes/);
            assert.equal(classify(summaryOnly, "TLSv1.2", cipher, true), "inconclusive");
          }
          if (mode === "silent") assert.equal(result.error, "ETIMEDOUT");
          assert.doesNotMatch(result.stdout + result.stderr, /Master-Key|Resumption PSK|TLS session ticket:/);
        }
        if (mode === "tls") {
          const positive = invoke(localArgs(GCM[0]));
          assert.equal(classify(positive, "TLSv1.2", GCM[0]), "trusted", JSON.stringify(positive));
          const accepted = invoke([...localArgs(CBC[0]).slice(0, -1), GCM[0]]);
          assert.equal(classify(accepted, "TLSv1.2", CBC[0], true), "accepted");
          assert.doesNotMatch(positive.stdout + positive.stderr + accepted.stdout + accepted.stderr, /Master-Key|Resumption PSK|TLS session ticket:/);
        }
      } finally { await worker.terminate(); }
    });
  }
});
