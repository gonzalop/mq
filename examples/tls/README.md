# TLS/SSL Example

Demonstrates secure MQTT connections using TLS/SSL encryption.

## Features Demonstrated

- TLS/SSL encrypted connections
- Custom TLS configuration
- Certificate verification
- Secure authentication

## Prerequisites

You need TLS certificates to run this example. You can:

1. **Use a public server with TLS** (easiest):
   - **Quick Test:** Run `go run main.go tls://test.mosquitto.org:8883`. The default `Unsafe for Testing Only` block in `main.go` skips verification, so it works immediately.
   - **Secure (Production-like):** Comment out the `Unsafe for Testing Only` block in `main.go`. Since public servers use valid CAs, it will connect securely using your system's root certificates.

2. **Generate self-signed certificates** (for testing):
   ```bash
   # Generate CA certificate
   openssl req -new -x509 -days 365 -extensions v3_ca \
     -keyout ca.key -out ca.crt -subj "/CN=MyCA"

   # Generate server certificate
   openssl genrsa -out server.key 2048
   openssl req -new -key server.key -out server.csr -subj "/CN=localhost"
   openssl x509 -req -in server.csr -CA ca.crt -CAkey ca.key \
     -CAcreateserial -out server.crt -days 365
   ```
   - **Quick Test:** Run `go run main.go tls://localhost:8883`. The default `Unsafe for Testing Only` block skips verification.
   - **Secure:** You would need to add code to load `ca.crt` into `tls.Config{RootCAs: ...}`.

3. **Use Let's Encrypt** (for production):
   - Treat this like Option 1. Comment out the `Unsafe for Testing Only` block to verify the valid Let's Encrypt certificate.

4. **AWS IoT** (Mutual TLS):
   - Get the connect device package ZIP file from AWS.
   - Create a folder named `private` under this directory and unzip the file there.
   - Uncomment the lines in `main.go` between `BEGIN - Real Cert` / `END - Real Cert`.
   - In `main.go`, comment out the lines between `BEGIN - Unsafe for Testing Only` / `END - Unsafe for Testing Only`.
   - Replace `<CHANGEME>` in the call to `tls.LoadX509KeyPair` with the actual file names you have.
   - (Optional) You might want to change the client ID, but it's not necessary.
   - (Optional) Change the log level to `slog.LevelDebug` if you want to see what's going on.
   - Run: `go run main.go mqtts://<your-domain>.iot.<aws-region>.amazonaws.com:8883` (you get the actual domain name from AWS).

## Running the Example

```bash
go run main.go [server] [username] [password]
```

Examples:
```bash
# Public test server (no auth)
go run main.go tls://test.mosquitto.org:8883

# Local server with credentials
go run main.go tls://localhost:8883 myuser mypass
```

## What It Does

1. Configures TLS settings (Insecure or Client Certs based on code toggles)
2. Connects to server over TLS
3. Subscribes to `test/tls`
4. Publishes encrypted messages
5. Receives messages over secure connection

## Example Output

```
TLS/SSL Example
===============

Connecting to tls://test.mosquitto.org:8883...
Using TLS encryption
✓ Connected securely!

Subscribing to 'tls/test'...
✓ Subscribed

Publishing encrypted message...
✓ Published

📨 Received: This message was sent over TLS!

Test completed successfully! 🎉
```

## TLS Configuration Options

The `main.go` file contains two mutually exclusive configuration blocks:

1. **Client Certificates (Mutual TLS)** - *Commented out by default*
   ```go
   // Load cert/key pair
   cert, _ := tls.LoadX509KeyPair("cert.pem", "key.pem")
   tlsConfig := &tls.Config{
       Certificates: []tls.Certificate{cert},
   }
   ```

2. **Unsafe / Testing** - *Enabled by default*
   ```go
   tlsConfig := &tls.Config{
       InsecureSkipVerify: true, // ⚠️ For local testing only!
   }
   ```

To use **System Root CAs** (e.g., for Let's Encrypt or public servers), simply remove `InsecureSkipVerify: true` (or set it to `false`) and do not provide `Certificates` if client auth is not needed. The Go `tls` package uses system roots by default.

client, err := mq.Dial(server, mq.WithTLS(tlsConfig))


## Security Best Practices

- ✅ **Always verify certificates** in production (`InsecureSkipVerify: false`)
- ✅ **Use TLS 1.2 or higher** (`MinVersion: tls.VersionTLS12`)
- ✅ **Keep certificates up to date**
- ✅ **Use strong cipher suites**
- ✅ **Consider mutual TLS** for critical systems
- ❌ **Never commit private keys** to version control

## Troubleshooting

### "x509: certificate signed by unknown authority"
- Add the CA certificate to your trust store
- Or provide it via `RootCAs` in TLS config

### "tls: first record does not look like a TLS handshake"
- Check you're using `tls://` not `tcp://`
- Verify the server port supports TLS

### Connection timeout
- Check firewall rules
- Verify server is listening on TLS port
- Check server TLS configuration
