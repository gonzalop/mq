# Observability Best Practices

The `mq` library is designed to be highly observable while remaining completely free of heavy external dependencies (such as direct OpenTelemetry or Prometheus imports).

Observability—including **Logging**, **Metrics**, and **Distributed Tracing**—is fully supported through clean extension points:
1. **The `Logger` interface** for structured logging.
2. **`HandlerInterceptor` (inbound)** and **`PublishInterceptor` (outbound)** for metrics collection and trace context propagation.

---

## 1. Structured Logging

The client uses Go's standard structured logging conventions. You can inject any logger that implements the `mq.Logger` interface (which matches the standard `log/slog` methods).

```go
type Logger interface {
	Debug(msg string, args ...any)
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
}
```

### Example: Using `log/slog`
```go
import (
	"log/slog"
	"os"
	
	"github.com/gonzalop/mq"
)

func main() {
	slogLogger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	client, err := mq.Dial("tcp://localhost:1883",
		mq.WithLogger(slogLogger),
	)
	if err != nil {
		slog.Error("Failed to connect", "error", err)
		return
	}
	defer client.Disconnect(context.Background())
}
```

---

## 2. OpenTelemetry Metrics

Because `mq` uses interceptors, you can easily plug in OpenTelemetry (OTel) metrics to track message rates, payload sizes, processing durations, and delivery rates.

### Example: Outbound & Inbound Metrics Collection
```go
import (
	"context"
	"time"

	"github.com/gonzalop/mq"
	"go.opentelemetry.io/otel/metric"
)

type MetricsCollector struct {
	publishCounter  metric.Int64Counter
	deliveryLatency metric.Float64Histogram
	receiveCounter  metric.Int64Counter
}

func NewMetricsCollector(meter metric.Meter) (*MetricsCollector, error) {
	pub, err := meter.Int64Counter("mqtt.client.publish.count",
		metric.WithDescription("Total number of published messages"))
	if err != nil {
		return nil, err
	}

	lat, err := meter.Float64Histogram("mqtt.client.publish.latency",
		metric.WithDescription("Latency of message publishing in seconds"))
	if err != nil {
		return nil, err
	}

	rec, err := meter.Int64Counter("mqtt.client.receive.count",
		metric.WithDescription("Total number of received messages"))
	if err != nil {
		return nil, err
	}

	return &MetricsCollector{
		publishCounter:  pub,
		deliveryLatency: lat,
		receiveCounter:  rec,
	}, nil
}

// Outbound Publish Interceptor
func (mc *MetricsCollector) PublishInterceptor(next mq.PublishFunc) mq.PublishFunc {
	return func(ctx context.Context, topic string, payload []byte, opts ...mq.PublishOption) mq.Token {
		start := time.Now()
		
		// Execute the publish
		token := next(ctx, topic, payload, opts...)
		
		go func() {
			// Wait for the message acknowledgement (QoS > 0) or local enqueue (QoS 0)
			err := token.Wait(ctx)
			
			duration := time.Since(start).Seconds()
			
			status := "success"
			if err != nil {
				status = "error"
			}
			
			mc.publishCounter.Add(ctx, 1, metric.WithAttributes(
				attribute.String("topic", topic),
				attribute.String("status", status),
			))
			
			mc.deliveryLatency.Record(ctx, duration, metric.WithAttributes(
				attribute.String("topic", topic),
				attribute.String("status", status),
			))
		}()
		
		return token
	}
}

// Inbound Message Interceptor
func (mc *MetricsCollector) HandlerInterceptor(next mq.MessageHandler) mq.MessageHandler {
	return func(client *mq.Client, msg mq.Message) {
		ctx := context.Background()
		mc.receiveCounter.Add(ctx, 1, metric.WithAttributes(
			attribute.String("topic", msg.Topic),
			attribute.Int("qos", int(msg.QoS)),
		))
		
		next(client, msg)
	}
}
```

---

## 3. Distributed Tracing

Distributed tracing across message brokers requires context propagation. Since MQTT v5.0 supports custom **User Properties**, we can inject standard W3C trace context headers into the publish options and extract them from incoming messages.

With the context-aware API (`Publish(ctx, ...)`), the caller's trace context is cleanly carried through.

### W3C Context Propagator
We can define a helper using OpenTelemetry's `propagation.TextMapPropagator`.

```go
import (
	"context"

	"github.com/gonzalop/mq"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

// UserPropertiesCarrier adapts MQTT v5 User Properties for OTel propagation
type UserPropertiesCarrier struct {
	properties map[string]string
}

func (c *UserPropertiesCarrier) Get(key string) string {
	return c.properties[key]
}

func (c *UserPropertiesCarrier) Set(key string, value string) {
	c.properties[key] = value
}

func (c *UserPropertiesCarrier) Keys() []string {
	keys := make([]string, 0, len(c.properties))
	for k := range c.properties {
		keys = append(keys, k)
	}
	return keys
}
```

### Outbound: Injecting Traces via `PublishInterceptor`

Using a `PublishInterceptor`, we can automatically extract the trace context from `ctx`, convert it to trace headers, and append them as MQTT user properties:

```go
func OTelTracePublishInterceptor(propagator propagation.TextMapPropagator, tracer trace.Tracer) mq.PublishInterceptor {
	return func(next mq.PublishFunc) mq.PublishFunc {
		return func(ctx context.Context, topic string, payload []byte, opts ...mq.PublishOption) mq.Token {
			// Start a span for the publish operation
			ctx, span := tracer.Start(ctx, "mqtt.publish", 
				trace.WithSpanKind(trace.SpanKindProducer),
				trace.WithAttributes(attribute.String("messaging.system", "mqtt")),
				trace.WithAttributes(attribute.String("messaging.destination", topic)),
			)
			defer span.End()

			// Inject current span context into a temporary carrier
			carrier := &UserPropertiesCarrier{properties: make(map[string]string)}
			propagator.Inject(ctx, carrier)

			// Convert carrier to publish options
			for k, v := range carrier.properties {
				opts = append(opts, mq.WithUserProperty(k, v))
			}

			// Pass control to the next middleware or client publisher
			return next(ctx, topic, payload, opts...)
		}
	}
}
```

### Inbound: Extracting Traces via `HandlerInterceptor`

For incoming messages, we extract the W3C trace context from the message's `User Properties` (under `Properties.UserProperties`) and link the execution of the handler to the original publisher span:

```go
func OTelTraceHandlerInterceptor(propagator propagation.TextMapPropagator, tracer trace.Tracer) mq.HandlerInterceptor {
	return func(next mq.MessageHandler) mq.MessageHandler {
		return func(client *mq.Client, msg mq.Message) {
			// Extract trace context from User Properties
			carrier := &UserPropertiesCarrier{properties: make(map[string]string)}
			if msg.Properties != nil {
				for _, up := range msg.Properties.UserProperties {
					carrier.properties[up.Key] = up.Value
				}
			}

			// Extract trace context
			parentCtx := propagator.Extract(context.Background(), carrier)

			// Start a child span associated with the extracted context
			ctx, span := tracer.Start(parentCtx, "mqtt.receive", 
				trace.WithSpanKind(trace.SpanKindConsumer),
				trace.WithAttributes(attribute.String("messaging.system", "mqtt")),
				trace.WithAttributes(attribute.String("messaging.destination", msg.Topic)),
			)
			defer span.End()

			// Expose the active context containing the span to the handler
			msg.Context = ctx
			next(client, msg)
		}
	}
}
```

---

## 4. Hooking Observability into `mq.Dial`

To wire it all up:

```go
func main() {
	propagator := otel.GetTextMapPropagator()
	tracer := otel.Tracer("my-application")
	meter := otel.Meter("my-application")
	
	mc, _ := NewMetricsCollector(meter)

	client, err := mq.Dial("tcp://localhost:1883",
		mq.WithPublishInterceptor(mc.PublishInterceptor),
		mq.WithPublishInterceptor(OTelTracePublishInterceptor(propagator, tracer)),
		
		mq.WithHandlerInterceptor(mc.HandlerInterceptor),
		mq.WithHandlerInterceptor(OTelTraceHandlerInterceptor(propagator, tracer)),
	)
	if err != nil {
		panic(err)
	}
	defer client.Disconnect(context.Background())
}
```
