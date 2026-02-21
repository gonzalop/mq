package mq

// HandlerInterceptor is a function that wraps a MessageHandler.
// It allows cross-cutting concerns like logging, metrics, or tracing
// to be applied to all message processing.
//
// Example (Logging):
//
//	func LoggingInterceptor(next mq.MessageHandler) mq.MessageHandler {
//	    return func(client *mq.Client, msg mq.Message) {
//	        log.Printf("Received message on topic %s", msg.Topic)
//	        next(client, msg)
//	    }
//	}
type HandlerInterceptor func(MessageHandler) MessageHandler

// PublishFunc matches the signature of Client.Publish.
type PublishFunc func(topic string, payload []byte, opts ...PublishOption) Token

// PublishInterceptor is a function that wraps a PublishFunc.
// It allows cross-cutting concerns to be applied to all outbound messages.
//
// Example (Tracing):
//
//	func TracingInterceptor(next mq.PublishFunc) mq.PublishFunc {
//	    return func(topic string, payload []byte, opts ...mq.PublishOption) mq.Token {
//	        // Inject tracing headers into opts or log the publish
//	        return next(topic, payload, opts...)
//	    }
//	}
type PublishInterceptor func(PublishFunc) PublishFunc

// applyHandlerInterceptors wraps a MessageHandler with multiple interceptors.
func applyHandlerInterceptors(handler MessageHandler, interceptors []HandlerInterceptor) MessageHandler {
	for i := len(interceptors) - 1; i >= 0; i-- {
		handler = interceptors[i](handler)
	}
	return handler
}

// applyPublishInterceptors wraps a PublishFunc with multiple interceptors.
func applyPublishInterceptors(publish PublishFunc, interceptors []PublishInterceptor) PublishFunc {
	for i := len(interceptors) - 1; i >= 0; i-- {
		publish = interceptors[i](publish)
	}
	return publish
}
