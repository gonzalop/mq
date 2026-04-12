# TLS/SSL Example

Demonstrates secure MQTT connections using TLS/SSL encryption.

## Features Demonstrated

- TLS/SSL encrypted connections
- Custom CA certificate loading
- Mutual TLS (mTLS) with client certificates
- Secure-by-default configuration

## Prerequisites

You need TLS certificates to run this example securely.

### Option 1: Public Server (easiest)
Public servers like `test.mosquitto.org` use valid certificates signed by trusted CAs (like Let's Encrypt). The example uses your system's root certificates by default.

```bash
go run main.go -server tls://test.mosquitto.org:8883
```

### Option 2: Self-Signed Certificates (Local Testing)
If you are running a local broker with self-signed certificates, you should provide the CA certificate to verify the server.

1. **Generate certificates:**
   ```bash
   # Generate CA
   openssl req -new -x509 -days 365 -keyout ca.key -out ca.crt -subj "/CN=MyLocalCA" -nodes
   # Generate Server Key & Cert
   openssl genrsa -out server.key 2048
   openssl req -new -key server.key -out server.csr -subj "/CN=localhost"
   openssl x509 -req -in server.csr -CA ca.crt -CAkey ca.key -CAcreateserial -out server.crt -days 365
   ```

2. **Run with CA verification:**
   ```bash
   go run main.go -server tls://localhost:8883 -ca-file ca.crt
   ```

### Option 3: Mutual TLS (mTLS)
Some servers (like AWS IoT) require the client to provide its own certificate.

```bash
go run main.go \
  -server tls://your-endpoint.iot.us-east-1.amazonaws.com:8883 \
  -ca-file AmazonRootCA1.pem \
  -cert-file device.cert.pem \
  -key-file device.private.key
```

## Usage

```bash
go run main.go [flags] [positional_args]
```

### Flags
- `-server`: MQTT server address (default: `tls://localhost:8883`)
- `-ca-file`: Path to CA certificate (PEM) to verify the server
- `-cert-file`: Path to client certificate (PEM) for mTLS
- `-key-file`: Path to client private key (PEM) for mTLS
- `-insecure`: Skip server certificate verification (**UNSAFE - Testing only**)
- `-username`: Username for authentication
- `-password`: Password for authentication
- `-topic`: Topic to use (default: `test/tls`)

### Unsafe Testing
If you absolutely cannot provide a CA certificate for a local test, you can use the `-insecure` flag. **Never use this in production.**

```bash
go run main.go -server tls://localhost:8883 -insecure
```

## Security Best Practices

- ✅ **Always verify certificates** in production (avoid `-insecure`)
- ✅ **Provide a CA certificate** when using self-signed certs
- ✅ **Use TLS 1.2 or higher** (the example enforces 1.2+)
- ✅ **Consider mutual TLS** for device authentication
- ❌ **Never commit private keys** to version control
