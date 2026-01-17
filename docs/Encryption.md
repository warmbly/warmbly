# Generate RSA Private Key
2048-bit RSA private key in PKCS#1 format (private.pem)
```sh
openssl genpkey -algorithm RSA -out private.pem -pkeyopt rsa_keygen_bits:2048
```

# Extract Public Key
Extract public key in PKIX / X.509 format (public.pem)
```sh
openssl rsa -pubout -in private.pem -out public.pem
```
