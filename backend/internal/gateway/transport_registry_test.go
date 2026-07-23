package gateway

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestTransportKeyDoesNotExposeProxyCredentials(t *testing.T) {
	const (
		username = "plaintext-proxy-user"
		password = "plaintext-proxy-password"
	)
	selected := SelectedAccount{
		AccountID:      1,
		ProxyURL:       "http://" + username + ":" + password + "@proxy.example.test:8080",
		FingerprintTLS: "chrome",
	}

	key := mustTransportKey(t, selected)
	if key.proxyAddress != "proxy.example.test:8080" {
		t.Fatalf("proxy address = %q, want credential-free canonical address", key.proxyAddress)
	}
	if key.proxyUserHash == ([32]byte{}) {
		t.Fatal("proxy credential hash is empty")
	}
	rendered := fmt.Sprintf("%#v", key)
	for _, secret := range []string{username, password} {
		if strings.Contains(rendered, secret) {
			t.Fatalf("transport key contains plaintext proxy credential %q: %s", secret, rendered)
		}
	}
}

func TestTransportRegistrySeparatesCredentialsForCanonicalProxyAddress(t *testing.T) {
	registry := newTransportRegistry(http.DefaultClient, upstreamTimeouts{}, 4)
	t.Cleanup(registry.Close)
	first := SelectedAccount{
		AccountID:      1,
		ProxyURL:       "http://first-user:first-password@PROXY.example.test",
		FingerprintTLS: "chrome",
	}
	second := SelectedAccount{
		AccountID:      2,
		ProxyURL:       "http://second-user:second-password@proxy.example.test:80",
		FingerprintTLS: "chrome",
	}

	firstKey := mustTransportKey(t, first)
	secondKey := mustTransportKey(t, second)
	if firstKey.proxyAddress != secondKey.proxyAddress {
		t.Fatalf("canonical proxy addresses differ: %q != %q", firstKey.proxyAddress, secondKey.proxyAddress)
	}
	if firstKey.proxyUserHash == secondKey.proxyUserHash {
		t.Fatal("different proxy credentials produced the same credential hash")
	}
	firstClient := mustRegistryClient(t, registry, first)
	secondClient := mustRegistryClient(t, registry, second)
	if firstClient == secondClient || firstClient.Transport == secondClient.Transport {
		t.Fatal("different proxy credentials reused the same client or transport")
	}
	if len(registry.entries) != 2 {
		t.Fatalf("registry entries = %d, want 2 credential-isolated entries", len(registry.entries))
	}
}

func TestTransportRegistryCredentialRotationReplacesClient(t *testing.T) {
	registry := newTransportRegistry(http.DefaultClient, upstreamTimeouts{}, 4)
	t.Cleanup(registry.Close)
	before := SelectedAccount{
		AccountID:      7,
		ProxyURL:       "https://proxy-user:old-password@proxy.example.test:8443",
		FingerprintTLS: "firefox",
	}
	after := before
	after.ProxyURL = "https://proxy-user:new-password@proxy.example.test:8443"

	beforeKey := mustTransportKey(t, before)
	afterKey := mustTransportKey(t, after)
	if beforeKey.proxyAddress != afterKey.proxyAddress || beforeKey.proxyUserHash == afterKey.proxyUserHash {
		t.Fatalf("credential rotation keys = before:%+v after:%+v", beforeKey, afterKey)
	}
	beforeClient := mustRegistryClient(t, registry, before)
	afterClient := mustRegistryClient(t, registry, after)
	if beforeClient == afterClient || beforeClient.Transport == afterClient.Transport {
		t.Fatal("proxy credential rotation reused the old client or transport")
	}
	if registry.entries[beforeKey] != nil || registry.entries[afterKey] == nil {
		t.Fatalf("credential rotation entries = old:%v new:%v, want false/true", registry.entries[beforeKey] != nil, registry.entries[afterKey] != nil)
	}
	if got := registry.bindings[before.AccountID]; got != afterKey {
		t.Fatalf("account binding = %+v, want rotated credential key %+v", got, afterKey)
	}
	if len(registry.entries) != 1 {
		t.Fatalf("registry entries = %d, want only the rotated credential entry", len(registry.entries))
	}
}
