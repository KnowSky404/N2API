package systemevent

import "context"

type WriteObserver interface {
	ObserveSystemEventWrite(err error)
}

type writeObserverContextKey struct{}

func WithWriteObserver(ctx context.Context, observer WriteObserver) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if observer == nil {
		return ctx
	}
	return context.WithValue(ctx, writeObserverContextKey{}, observer)
}

func WriteObserverFromContext(ctx context.Context) WriteObserver {
	if ctx == nil {
		return nil
	}
	observer, _ := ctx.Value(writeObserverContextKey{}).(WriteObserver)
	return observer
}
