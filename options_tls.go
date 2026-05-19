package mq

import "crypto/tls"

// WithTLS sets the TLS configuration for secure connections.
// Pass nil for default TLS settings, or provide a custom *tls.Config.
// The server URL should use "tls://", "ssl://", or "mqtts://" scheme, or this option
// will enable TLS for "tcp://" URLs as well.
func WithTLS(config *tls.Config) Option {
	return func(o *clientOptions) {
		o.TLSConfig = config
	}
}
