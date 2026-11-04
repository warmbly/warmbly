const { CBC } = require("./origin-tls-client.cjs");

function refused(cipher = CBC[0]) {
  const suite = cipher === CBC[0] ? "c0 27" : "c0 28";
  const hello = `01 00 00 2b 03 03 ${"00 ".repeat(32)}00 00 04 ${suite} 00 ff 01 00`;
  return { status: 1, stdout: `>>> TLS 1.2, Handshake [length 002f], ClientHello
    ${hello}
<<< TLS 1.2, Alert [length 0002], fatal handshake_failure
    02 28
no peer certificate available
New, (NONE), Cipher is (NONE)
`, stderr: "SSL_connect:SSLv3/TLS write client hello\nSSL3 alert read:fatal:handshake failure\n" };
}

module.exports = { refused };
