package gateway

import (
	"container/list"
	"crypto/sha256"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync"
)

const defaultTransportRegistryCapacity = 64

var errTransportRegistryClosed = errors.New("upstream transport registry is closed")

type transportKey struct {
	proxyScheme    string
	proxyAddress   string
	proxyUserHash  [sha256.Size]byte
	fingerprint    string
	protocolPolicy string
}

type transportEntry struct {
	key       transportKey
	client    *http.Client
	transport *selectedAccountTransport
	lru       *list.Element
}

type transportRegistry struct {
	mu       sync.Mutex
	capacity int
	timeouts upstreamTimeouts
	template *http.Client
	entries  map[transportKey]*transportEntry
	bindings map[int64]transportKey
	lru      *list.List
	closed   bool
}

func newTransportRegistry(template *http.Client, timeouts upstreamTimeouts, capacity int) *transportRegistry {
	if template == nil {
		template = http.DefaultClient
	}
	if capacity <= 0 {
		capacity = defaultTransportRegistryCapacity
	}
	return &transportRegistry{
		capacity: capacity,
		timeouts: timeouts,
		template: template,
		entries:  make(map[transportKey]*transportEntry),
		bindings: make(map[int64]transportKey),
		lru:      list.New(),
	}
}

func (r *transportRegistry) ClientFor(selected SelectedAccount) (*http.Client, error) {
	key, proxyURL, err := selectedAccountTransportKey(selected)
	if err != nil {
		return nil, err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil, errTransportRegistryClosed
	}
	if selected.AccountID > 0 {
		if previous, ok := r.bindings[selected.AccountID]; ok && previous != key {
			r.releaseBindingLocked(selected.AccountID, previous)
		}
	}
	if entry, ok := r.entries[key]; ok {
		r.lru.MoveToFront(entry.lru)
		if selected.AccountID > 0 {
			r.bindings[selected.AccountID] = key
		}
		return entry.client, nil
	}

	transport := newSelectedAccountTransport(proxyURL, key.fingerprint, r.timeouts)
	client := &http.Client{
		Transport:     transport,
		Timeout:       r.template.Timeout,
		CheckRedirect: r.template.CheckRedirect,
		Jar:           r.template.Jar,
	}
	entry := &transportEntry{key: key, client: client, transport: transport}
	entry.lru = r.lru.PushFront(entry)
	r.entries[key] = entry
	if selected.AccountID > 0 {
		r.bindings[selected.AccountID] = key
	}
	for len(r.entries) > r.capacity {
		r.evictOldestLocked()
	}
	return client, nil
}

func (r *transportRegistry) InvalidateAccountTransport(accountID int64) {
	if r == nil || accountID <= 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	key, ok := r.bindings[accountID]
	if !ok {
		return
	}
	r.releaseBindingLocked(accountID, key)
}

func (r *transportRegistry) InvalidateAllAccountTransports() {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closeEntriesLocked()
	r.entries = make(map[transportKey]*transportEntry)
	r.bindings = make(map[int64]transportKey)
	r.lru.Init()
}

func (r *transportRegistry) Close() {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	r.closed = true
	r.closeEntriesLocked()
	r.entries = make(map[transportKey]*transportEntry)
	r.bindings = make(map[int64]transportKey)
	r.lru.Init()
}

func (r *transportRegistry) releaseBindingLocked(accountID int64, key transportKey) {
	delete(r.bindings, accountID)
	for _, boundKey := range r.bindings {
		if boundKey == key {
			return
		}
	}
	r.removeEntryLocked(key)
}

func (r *transportRegistry) evictOldestLocked() {
	oldest := r.lru.Back()
	if oldest == nil {
		return
	}
	entry := oldest.Value.(*transportEntry)
	for accountID, key := range r.bindings {
		if key == entry.key {
			delete(r.bindings, accountID)
		}
	}
	r.removeEntryLocked(entry.key)
}

func (r *transportRegistry) removeEntryLocked(key transportKey) {
	entry, ok := r.entries[key]
	if !ok {
		return
	}
	delete(r.entries, key)
	r.lru.Remove(entry.lru)
	entry.transport.CloseIdleConnections()
}

func (r *transportRegistry) closeEntriesLocked() {
	for _, entry := range r.entries {
		entry.transport.CloseIdleConnections()
	}
}

func selectedAccountTransportKey(selected SelectedAccount) (transportKey, *url.URL, error) {
	fingerprintValue := strings.TrimSpace(selected.FingerprintTLS)
	fingerprint := normalizeTLSFingerprintName(fingerprintValue)
	if fingerprintValue != "" && fingerprint == "" {
		return transportKey{}, nil, errors.New("unsupported upstream TLS fingerprint configuration")
	}
	key := transportKey{
		fingerprint:    fingerprint,
		protocolPolicy: "http1-fingerprint-v1",
	}
	proxyValue := strings.TrimSpace(selected.ProxyURL)
	if proxyValue == "" {
		return key, nil, nil
	}
	parsed, err := url.Parse(proxyValue)
	if err != nil || !parsed.IsAbs() || parsed.Hostname() == "" {
		return transportKey{}, nil, errors.New("invalid upstream proxy configuration")
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return transportKey{}, nil, errors.New("unsupported upstream proxy configuration")
	}
	key.proxyScheme = parsed.Scheme
	key.proxyAddress = canonicalProxyAddress(parsed)
	if parsed.User != nil {
		credential := parsed.User.String()
		key.proxyUserHash = sha256.Sum256([]byte("n2api:gateway:proxy-credential:v1\x00" + credential))
	}
	return key, parsed, nil
}
