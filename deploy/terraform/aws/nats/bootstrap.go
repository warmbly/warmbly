//go:build ignore

package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"time"

	"github.com/nats-io/jwt/v2"
	"github.com/nats-io/nkeys"
)

func must[T any](value T, err error) T {
	if err != nil {
		panic(err)
	}
	return value
}

func main() {
	if len(os.Args) != 2 {
		panic("usage: go run deploy/terraform/aws/nats/bootstrap.go /private/new-directory")
	}
	dir := os.Args[1]
	if err := os.Mkdir(dir, 0700); err != nil {
		panic(err)
	}
	write := func(name string, value []byte) {
		if err := os.WriteFile(filepath.Join(dir, name), value, 0600); err != nil {
			panic(err)
		}
	}
	now := time.Now()
	caKey := must(ecdsa.GenerateKey(elliptic.P256(), rand.Reader))
	ca := &x509.Certificate{
		SerialNumber: must(rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))),
		Subject:      pkix.Name{CommonName: "Warmbly NATS private CA"},
		NotBefore:    now.Add(-time.Hour), NotAfter: now.AddDate(5, 0, 0),
		IsCA: true, BasicConstraintsValid: true,
		KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}
	caDER := must(x509.CreateCertificate(rand.Reader, ca, ca, &caKey.PublicKey, caKey))
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})
	write("ca.crt", caPEM)
	write("ca.key", pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: must(x509.MarshalECPrivateKey(caKey))}))
	operator := must(nkeys.CreateOperator())
	account := must(nkeys.CreateAccount())
	system := must(nkeys.CreateAccount())
	operatorClaim := jwt.NewOperatorClaims(must(operator.PublicKey()))
	operatorClaim.SystemAccount = must(system.PublicKey())
	operatorJWT := must(operatorClaim.Encode(operator))
	accountClaim := jwt.NewAccountClaims(must(account.PublicKey()))
	accountClaim.Limits.JetStreamLimits = jwt.JetStreamLimits{DiskStorage: 256 << 30, Streams: 20, Consumer: -1, MaxAckPending: -1, MaxBytesRequired: true}
	accountJWT := must(accountClaim.Encode(operator))
	systemJWT := must(jwt.NewAccountClaims(must(system.PublicKey())).Encode(operator))
	user := must(nkeys.CreateUser())
	userClaim := jwt.NewUserClaims(must(user.PublicKey()))
	userClaim.Pub.Allow.Add("warmbly.>", "$JS.ACK.>", "_INBOX.>",
		"$JS.API.INFO", "$JS.API.STREAM.INFO.warmbly", "$JS.API.STREAM.CREATE.warmbly", "$JS.API.STREAM.UPDATE.warmbly",
		"$JS.API.CONSUMER.CREATE.warmbly.>", "$JS.API.CONSUMER.INFO.warmbly.>", "$JS.API.CONSUMER.MSG.NEXT.warmbly.>")
	userClaim.Sub.Allow.Add("warmbly.>", "_INBOX.>")
	creds := must(jwt.FormatUserConfig(must(userClaim.Encode(account)), must(user.Seed())))
	write("client.creds", creds)
	client := map[string]string{
		"NATS_URL":        "tls://nats1.nats.warmbly.internal:4222,tls://nats2.nats.warmbly.internal:4222,tls://nats3.nats.warmbly.internal:4222",
		"NATS_CREDS_B64":  base64.StdEncoding.EncodeToString(creds),
		"NATS_TLS_CA_B64": base64.StdEncoding.EncodeToString(caPEM),
		"NATS_REPLICAS":   "3", "NATS_MAX_BYTES": "64GiB", "NATS_DISCARD_NEW": "true",
	}
	write("client.json", must(json.Marshal(client)))
	write("operator.seed", must(operator.Seed()))
	write("account.seed", must(account.Seed()))
	write("authority.json", must(json.Marshal(map[string]string{
		"ca.crt": string(caPEM), "ca.key": string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: must(x509.MarshalECPrivateKey(caKey))})),
		"operator.seed": string(must(operator.Seed())), "account.seed": string(must(account.Seed())),
	})))
	routePassword := fmt.Sprintf("%x", must(rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 256))))
	for i := 1; i <= 3; i++ {
		name := fmt.Sprintf("nats%d", i)
		host := name + ".nats.warmbly.internal"
		key := must(ecdsa.GenerateKey(elliptic.P256(), rand.Reader))
		cert := &x509.Certificate{
			SerialNumber: must(rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))),
			Subject:      pkix.Name{CommonName: host}, DNSNames: []string{host},
			NotBefore: now.Add(-time.Hour), NotAfter: now.AddDate(1, 0, 0),
			KeyUsage:    x509.KeyUsageDigitalSignature,
			ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		}
		certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: must(x509.CreateCertificate(rand.Reader, cert, ca, &key.PublicKey, caKey))})
		tls := `cert_file: "/etc/nats/server.crt", key_file: "/etc/nats/server.key", ca_file: "/etc/nats/ca.crt", timeout: 5`
		configuration := fmt.Sprintf(`server_name: %q
listen: 0.0.0.0:4222
client_advertise: %q
http: 127.0.0.1:8222
max_payload: 8MB
operator: %q
system_account: %q
resolver: MEMORY
resolver_preload: { %q: %q, %q: %q }
tls { %s }
jetstream { store_dir: "/var/lib/nats", max_file_store: 96GB, max_mem_store: 64MB, sync_interval: always }
cluster {
  name: "warmbly-production"
  listen: 0.0.0.0:6222
  authorization { user: route, password: %q }
  tls { %s, verify: true }
  routes: [
`, name, host+":4222", operatorJWT, must(system.PublicKey()), must(account.PublicKey()), accountJWT, must(system.PublicKey()), systemJWT, tls, routePassword, tls)
		for peer := 1; peer <= 3; peer++ {
			if peer != i {
				configuration += fmt.Sprintf("    nats-route://route:%s@nats%d.nats.warmbly.internal:6222\n", routePassword, peer)
			}
		}
		configuration += "  ]\n}\n"
		bundle := map[string]string{
			"server.conf": configuration, "ca.crt": string(caPEM), "server.crt": string(certPEM),
			"server.key": string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: must(x509.MarshalECPrivateKey(key))})),
		}
		write(name+".json", must(json.Marshal(bundle)))
	}
	fmt.Println("Created private NATS credentials and certificates; no secret values printed")
}
