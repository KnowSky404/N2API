package gateway

import (
	"context"
	"strings"
)

type tlsFingerprintContextKey struct{}

func contextWithTLSFingerprint(ctx context.Context, fingerprint string) context.Context {
	fingerprint = normalizeTLSFingerprintName(fingerprint)
	if fingerprint == "" {
		return ctx
	}
	return context.WithValue(ctx, tlsFingerprintContextKey{}, fingerprint)
}

func tlsFingerprintFromContext(ctx context.Context) string {
	value, _ := ctx.Value(tlsFingerprintContextKey{}).(string)
	return normalizeTLSFingerprintName(value)
}

func normalizeTLSFingerprintName(fingerprint string) string {
	fingerprint = strings.TrimSpace(strings.ToLower(fingerprint))
	fingerprint = strings.ReplaceAll(fingerprint, "_", "-")
	fingerprint = strings.ReplaceAll(fingerprint, " ", "-")
	switch fingerprint {
	case "chrome", "hellochrome", "hellochrome-auto", "chrome-auto":
		return "chrome"
	case "firefox", "hellofirefox", "hellofirefox-auto", "firefox-auto":
		return "firefox"
	case "safari", "hellosafari", "hellosafari-auto", "safari-auto":
		return "safari"
	case "ios", "helloios", "helloios-auto", "ios-auto":
		return "ios"
	case "android", "android-okhttp", "helloandroid", "helloandroid-11-okhttp":
		return "android"
	case "edge", "helloedge", "helloedge-auto", "edge-auto":
		return "edge"
	case "random", "randomized", "hellorandomized":
		return "randomized"
	case "randomized-alpn", "hellorandomizedalpn":
		return "randomized-alpn"
	case "randomized-no-alpn", "hellorandomizednoalpn":
		return "randomized-no-alpn"
	case "go", "golang", "hello-golang", "hellogolang":
		return "golang"
	default:
		return ""
	}
}
