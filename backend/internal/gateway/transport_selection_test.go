package gateway

import (
	"errors"
	"net/http"
	"sync"
	"testing"
)

func TestTransportRegistrySequentialReuse(t *testing.T) {
	registry := newTransportRegistry(http.DefaultClient, upstreamTimeouts{}, 4)
	t.Cleanup(registry.Close)
	selected := SelectedAccount{AccountID: 1, ProxyURL: "http://proxy.example.test:8080"}

	first, err := registry.ClientFor(selected)
	if err != nil {
		t.Fatalf("first ClientFor returned error: %v", err)
	}
	second, err := registry.ClientFor(selected)
	if err != nil {
		t.Fatalf("second ClientFor returned error: %v", err)
	}
	if first != second || first.Transport != second.Transport {
		t.Fatal("identical transport configuration did not reuse the client and transport")
	}
	if len(registry.entries) != 1 || registry.lru.Len() != 1 {
		t.Fatalf("registry entries/LRU = %d/%d, want 1/1", len(registry.entries), registry.lru.Len())
	}
}

func TestTransportRegistryConcurrentDedupe(t *testing.T) {
	registry := newTransportRegistry(http.DefaultClient, upstreamTimeouts{}, 4)
	t.Cleanup(registry.Close)
	selected := SelectedAccount{AccountID: 1, ProxyURL: "http://proxy.example.test:8080"}

	const callers = 32
	clients := make(chan *http.Client, callers)
	errorsSeen := make(chan error, callers)
	var wait sync.WaitGroup
	wait.Add(callers)
	for range callers {
		go func() {
			defer wait.Done()
			client, err := registry.ClientFor(selected)
			if err != nil {
				errorsSeen <- err
				return
			}
			clients <- client
		}()
	}
	wait.Wait()
	close(clients)
	close(errorsSeen)

	for err := range errorsSeen {
		t.Fatalf("concurrent ClientFor returned error: %v", err)
	}
	var shared *http.Client
	for client := range clients {
		if shared == nil {
			shared = client
			continue
		}
		if client != shared {
			t.Fatal("concurrent ClientFor created duplicate clients")
		}
	}
	if len(registry.entries) != 1 || registry.lru.Len() != 1 {
		t.Fatalf("registry entries/LRU = %d/%d, want 1/1", len(registry.entries), registry.lru.Len())
	}
}

func TestTransportRegistryCapacityEvictsLeastRecentlyUsed(t *testing.T) {
	registry := newTransportRegistry(http.DefaultClient, upstreamTimeouts{}, 2)
	t.Cleanup(registry.Close)
	first := SelectedAccount{AccountID: 1, ProxyURL: "http://proxy1.example:8080"}
	second := SelectedAccount{AccountID: 2, ProxyURL: "http://proxy2.example:8080"}
	third := SelectedAccount{AccountID: 3, ProxyURL: "http://proxy3.example:8080"}

	firstClient := mustRegistryClient(t, registry, first)
	secondClient := mustRegistryClient(t, registry, second)
	if reused := mustRegistryClient(t, registry, first); reused != firstClient {
		t.Fatal("recently used client was not reused")
	}
	mustRegistryClient(t, registry, third)

	firstKey := mustTransportKey(t, first)
	secondKey := mustTransportKey(t, second)
	thirdKey := mustTransportKey(t, third)
	if len(registry.entries) != 2 || registry.lru.Len() != 2 {
		t.Fatalf("registry entries/LRU = %d/%d, want 2/2", len(registry.entries), registry.lru.Len())
	}
	if registry.entries[firstKey] == nil || registry.entries[thirdKey] == nil || registry.entries[secondKey] != nil {
		t.Fatalf("capacity eviction retained keys = first:%v second:%v third:%v, want true/false/true", registry.entries[firstKey] != nil, registry.entries[secondKey] != nil, registry.entries[thirdKey] != nil)
	}
	if _, ok := registry.bindings[second.AccountID]; ok {
		t.Fatal("evicted account binding was retained")
	}
	if recreated := mustRegistryClient(t, registry, second); recreated == secondClient {
		t.Fatal("evicted client was unexpectedly reused")
	}
}

func TestTransportRegistryAccountConfigChangeReplacesBinding(t *testing.T) {
	registry := newTransportRegistry(http.DefaultClient, upstreamTimeouts{}, 4)
	t.Cleanup(registry.Close)
	before := SelectedAccount{AccountID: 7, ProxyURL: "http://proxy-a.example.test:8080"}
	after := SelectedAccount{AccountID: 7, ProxyURL: "https://proxy-b.example.test:8443"}

	beforeClient := mustRegistryClient(t, registry, before)
	afterClient := mustRegistryClient(t, registry, after)
	beforeKey := mustTransportKey(t, before)
	afterKey := mustTransportKey(t, after)
	if beforeClient == afterClient {
		t.Fatal("changed account transport configuration reused the previous client")
	}
	if registry.entries[beforeKey] != nil || registry.entries[afterKey] == nil {
		t.Fatalf("config change entries = old:%v new:%v, want false/true", registry.entries[beforeKey] != nil, registry.entries[afterKey] != nil)
	}
	if got := registry.bindings[before.AccountID]; got != afterKey {
		t.Fatalf("account binding = %+v, want updated key %+v", got, afterKey)
	}
}

func TestProxyTransportInvalidationPreservesSharedEntryUntilLastAccount(t *testing.T) {
	proxy := NewProxy(nil, nil, Config{})
	t.Cleanup(proxy.Close)
	first := SelectedAccount{AccountID: 1, ProxyURL: "http://proxy.example.test:8080"}
	second := first
	second.AccountID = 2

	firstClient, err := proxy.clientForSelectedAccount(first)
	if err != nil {
		t.Fatalf("first clientForSelectedAccount returned error: %v", err)
	}
	secondClient, err := proxy.clientForSelectedAccount(second)
	if err != nil {
		t.Fatalf("second clientForSelectedAccount returned error: %v", err)
	}
	if firstClient != secondClient {
		t.Fatal("accounts with identical transport configuration did not share a client")
	}
	key := mustTransportKey(t, first)

	proxy.InvalidateAccountTransport(first.AccountID)
	if _, ok := proxy.transports.bindings[first.AccountID]; ok {
		t.Fatal("invalidated account binding was retained")
	}
	if proxy.transports.entries[key] == nil {
		t.Fatal("shared entry was removed while another account remained bound")
	}

	proxy.InvalidateAccountTransport(second.AccountID)
	if proxy.transports.entries[key] != nil || len(proxy.transports.bindings) != 0 {
		t.Fatalf("last invalidation left entry/bindings = %v/%d, want false/0", proxy.transports.entries[key] != nil, len(proxy.transports.bindings))
	}
}

func TestProxyTransportInvalidationAllAllowsRecreation(t *testing.T) {
	proxy := NewProxy(nil, nil, Config{})
	t.Cleanup(proxy.Close)
	selected := SelectedAccount{AccountID: 1, ProxyURL: "http://proxy1.example:8080"}
	original, err := proxy.clientForSelectedAccount(selected)
	if err != nil {
		t.Fatalf("clientForSelectedAccount returned error: %v", err)
	}

	proxy.InvalidateAllAccountTransports()
	if len(proxy.transports.entries) != 0 || len(proxy.transports.bindings) != 0 || proxy.transports.lru.Len() != 0 || proxy.transports.closed {
		t.Fatalf("registry after invalidate all = entries:%d bindings:%d LRU:%d closed:%v", len(proxy.transports.entries), len(proxy.transports.bindings), proxy.transports.lru.Len(), proxy.transports.closed)
	}
	recreated, err := proxy.clientForSelectedAccount(selected)
	if err != nil {
		t.Fatalf("clientForSelectedAccount after invalidate all returned error: %v", err)
	}
	if recreated == original {
		t.Fatal("invalidate all reused the removed client")
	}
}

func TestProxyTransportShutdownClosesRegistry(t *testing.T) {
	proxy := NewProxy(nil, nil, Config{})
	selected := SelectedAccount{AccountID: 1, ProxyURL: "http://proxy1.example:8080"}
	if _, err := proxy.clientForSelectedAccount(selected); err != nil {
		t.Fatalf("clientForSelectedAccount returned error: %v", err)
	}

	proxy.Close()
	proxy.Close()
	if !proxy.transports.closed || len(proxy.transports.entries) != 0 || len(proxy.transports.bindings) != 0 || proxy.transports.lru.Len() != 0 {
		t.Fatalf("closed registry = closed:%v entries:%d bindings:%d LRU:%d", proxy.transports.closed, len(proxy.transports.entries), len(proxy.transports.bindings), proxy.transports.lru.Len())
	}
	if _, err := proxy.clientForSelectedAccount(selected); !errors.Is(err, errTransportRegistryClosed) {
		t.Fatalf("clientForSelectedAccount after close error = %v, want %v", err, errTransportRegistryClosed)
	}
}

func TestTransportRegistryRejectsUnsupportedConfiguration(t *testing.T) {
	registry := newTransportRegistry(http.DefaultClient, upstreamTimeouts{}, 4)
	t.Cleanup(registry.Close)
	tests := []SelectedAccount{
		{AccountID: 1, ProxyURL: "socks5://proxy.example.test:1080"},
	}
	for _, selected := range tests {
		if _, err := registry.ClientFor(selected); err == nil {
			t.Fatalf("ClientFor(%+v) accepted unsupported transport configuration", selected)
		}
	}
	if len(registry.entries) != 0 || len(registry.bindings) != 0 {
		t.Fatalf("rejected configurations changed registry state: entries=%d bindings=%d", len(registry.entries), len(registry.bindings))
	}
}

func mustRegistryClient(t *testing.T, registry *transportRegistry, selected SelectedAccount) *http.Client {
	t.Helper()
	client, err := registry.ClientFor(selected)
	if err != nil {
		t.Fatalf("ClientFor returned error: %v", err)
	}
	return client
}

func mustTransportKey(t *testing.T, selected SelectedAccount) transportKey {
	t.Helper()
	key, _, err := selectedAccountTransportKey(selected)
	if err != nil {
		t.Fatalf("selectedAccountTransportKey returned error: %v", err)
	}
	return key
}
