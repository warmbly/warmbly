const { test } = require("node:test");
const assert = require("node:assert/strict");
const { CBC, GCM, TLS13, HOST, CA, invoke, classify, probe } = require("./origin-tls-client.cjs");
const trusted = (cipher = GCM[0]) => ({ status: 0, stdout: `-----BEGIN CERTIFICATE-----\nfixture chain\n-----END CERTIFICATE-----\nNew, ${cipher === TLS13 ? "TLSv1.3" : "TLSv1.2"}, Cipher is ${cipher}\nVerify return code: 0 (ok)\n`, stderr: "" });
const refused = () => ({ status: 1, stdout: "New, (NONE), Cipher is (NONE)\n", stderr: "error:0A000410:SSL routines:ssl3_read_bytes:sslv3 alert handshake failure: SSL alert number 40\n" });

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
    const result = { ...(CBC.includes(cipher) ? refused() : trusted(cipher)), command: ["openssl", ...args] };
    change(result, handshakes);
    return result;
  };
  return { rows, run };
}

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
      assert.ok(args.includes(row.cipher === TLS13 ? "-tls1_3" : "-tls1_2"));
    }
  }
  assert.deepEqual(f.rows.at(-1).success, true);
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
