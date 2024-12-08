package mqtt

import "context"

type contextKey string

const publishKey = contextKey("mqtt:publish")

// WithPublishFunc adds a PublishFunc to the context.
func WithPublishFunc(ctx context.Context, fn PublishFunc) context.Context {
	return context.WithValue(ctx, publishKey, fn)
}

// GetPublishFunc returns the PublishFunc associated with the context, if any.
func GetPublishFunc(ctx context.Context) (PublishFunc, bool) {
	fn, ok := ctx.Value(publishKey).(PublishFunc)
	return fn, ok
}
