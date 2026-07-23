package main

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/admin"
	"github.com/KnowSky404/N2API/backend/internal/config"
	"github.com/KnowSky404/N2API/backend/internal/gateway"
	"github.com/KnowSky404/N2API/backend/internal/metrics"
	"github.com/KnowSky404/N2API/backend/internal/provider"
)

type captureMainTaskMetrics struct{ runs [][2]string }

func (m *captureMainTaskMetrics) BeginBackgroundTask(task string) func(string) {
	return func(outcome string) { m.runs = append(m.runs, [2]string{task, outcome}) }
}
func (m *captureMainTaskMetrics) ObserveBackgroundTaskRun(task, outcome string, _ time.Duration) {
	m.runs = append(m.runs, [2]string{task, outcome})
}

type fakeProviderAccountMetricsSource struct {
	accounts []provider.Account
	err      error
}

type slowUploadAuthenticator struct {
	authenticated chan struct{}
	once          sync.Once
}

func (a *slowUploadAuthenticator) AuthenticateAPIKey(context.Context, string) (admin.APIKey, error) {
	a.once.Do(func() { close(a.authenticated) })
	routingPoolID := int64(1)
	return admin.APIKey{ID: 1, RoutingPoolID: &routingPoolID}, nil
}

type unusedSlowUploadAccountProvider struct{}

func (unusedSlowUploadAccountProvider) SelectAccountForModel(context.Context, string, ...int64) (gateway.SelectedAccount, error) {
	return gateway.SelectedAccount{}, errors.New("account selection should not run while request body is blocked")
}

type postAuthenticationReadListener struct {
	net.Listener
	authenticated <-chan struct{}
	bodyRead      chan struct{}
}

func (l *postAuthenticationReadListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return &postAuthenticationReadConn{
		Conn:          conn,
		authenticated: l.authenticated,
		bodyRead:      l.bodyRead,
	}, nil
}

type postAuthenticationReadConn struct {
	net.Conn
	authenticated <-chan struct{}
	bodyRead      chan struct{}
	once          sync.Once
}

func (c *postAuthenticationReadConn) Read(buffer []byte) (int, error) {
	select {
	case <-c.authenticated:
		c.once.Do(func() { close(c.bodyRead) })
	default:
	}
	return c.Conn.Read(buffer)
}

func (s *fakeProviderAccountMetricsSource) ListAccounts(context.Context) ([]provider.Account, error) {
	return s.accounts, s.err
}

func TestNewHTTPServerAppliesInboundResourceBoundariesWithoutWriteTimeout(t *testing.T) {
	handler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	server := newHTTPServer(config.Config{
		Host:               "127.0.0.1",
		Port:               3000,
		HTTPIdleTimeout:    75 * time.Second,
		HTTPMaxHeaderBytes: 512 << 10,
	}, handler, context.Background())
	if server.Addr != "127.0.0.1:3000" || server.Handler == nil || server.ReadHeaderTimeout != 5*time.Second || server.IdleTimeout != 75*time.Second || server.MaxHeaderBytes != 512<<10 {
		t.Fatalf("server = %+v", server)
	}
	if server.WriteTimeout != 0 || server.ReadTimeout != 0 {
		t.Fatalf("global timeouts = read:%s write:%s, want zero", server.ReadTimeout, server.WriteTimeout)
	}
}

func TestHTTPServerCancelsActiveRequestsWhenBaseContextEnds(t *testing.T) {
	baseContext, cancelBase := context.WithCancel(context.Background())
	requestStarted := make(chan struct{})
	requestCanceled := make(chan struct{})
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(requestStarted)
		<-r.Context().Done()
		close(requestCanceled)
	})
	server := newHTTPServer(config.Config{}, handler, baseContext)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen returned error: %v", err)
	}
	serveErrors := make(chan error, 1)
	go func() { serveErrors <- server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })

	clientDone := make(chan struct{})
	go func() {
		defer close(clientDone)
		response, requestErr := (&http.Client{Timeout: 2 * time.Second}).Get("http://" + listener.Addr().String())
		if requestErr == nil {
			_, _ = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
		}
	}()
	select {
	case <-requestStarted:
	case <-time.After(time.Second):
		t.Fatal("active request did not start")
	}

	cancelBase()
	select {
	case <-requestCanceled:
	case <-time.After(time.Second):
		t.Fatal("active request was not canceled with server base context")
	}
	shutdownContext, cancelShutdown := context.WithTimeout(context.Background(), time.Second)
	defer cancelShutdown()
	if err := server.Shutdown(shutdownContext); err != nil {
		t.Fatalf("Shutdown returned error: %v", err)
	}
	select {
	case err := <-serveErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			t.Fatalf("Serve returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("server did not stop")
	}
	select {
	case <-clientDone:
	case <-time.After(time.Second):
		t.Fatal("client request did not stop")
	}
}

func TestHTTPServerCancellationUnblocksProxyReadingSlowUpload(t *testing.T) {
	baseContext, cancelBase := context.WithCancel(context.Background())
	authenticated := make(chan struct{})
	bodyRead := make(chan struct{})
	authenticator := &slowUploadAuthenticator{authenticated: authenticated}
	proxy := gateway.NewProxyWithClient(authenticator, unusedSlowUploadAccountProvider{}, gateway.Config{
		MaxAcceptedRequestBodyBytes: 1024,
	}, http.DefaultClient)
	handlerDone := make(chan struct{})
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxy.ServeHTTP(w, r)
		close(handlerDone)
	})
	server := newHTTPServer(config.Config{}, handler, baseContext)
	tcpListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen returned error: %v", err)
	}
	listener := &postAuthenticationReadListener{
		Listener:      tcpListener,
		authenticated: authenticated,
		bodyRead:      bodyRead,
	}
	serveErrors := make(chan error, 1)
	go func() { serveErrors <- server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })

	clientConn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("Dial returned error: %v", err)
	}
	t.Cleanup(func() { _ = clientConn.Close() })
	requestHeaders := "POST /v1/chat/completions HTTP/1.1\r\n" +
		"Host: " + listener.Addr().String() + "\r\n" +
		"Authorization: Bearer slow-upload-test\r\n" +
		"Content-Type: application/json\r\n" +
		"Content-Length: 1024\r\n" +
		"Connection: close\r\n\r\n"
	if _, err := io.WriteString(clientConn, requestHeaders); err != nil {
		t.Fatalf("WriteString returned error: %v", err)
	}
	clientDone := make(chan error, 1)
	go func() {
		_, readErr := io.Copy(io.Discard, clientConn)
		clientDone <- readErr
	}()

	select {
	case <-bodyRead:
	case <-time.After(time.Second):
		t.Fatal("proxy did not block reading the incomplete request body")
	}
	cancelBase()

	select {
	case <-handlerDone:
	case <-time.After(time.Second):
		t.Fatal("proxy request body read did not stop after base context cancellation")
	}
	select {
	case <-clientDone:
	case <-time.After(time.Second):
		t.Fatal("slow upload client did not stop after base context cancellation")
	}
	shutdownContext, cancelShutdown := context.WithTimeout(context.Background(), time.Second)
	defer cancelShutdown()
	if err := server.Shutdown(shutdownContext); err != nil {
		t.Fatalf("Shutdown returned error: %v", err)
	}
	select {
	case err := <-serveErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			t.Fatalf("Serve returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("server did not stop")
	}
}

func TestProviderAccountMetricsPreserveLastSnapshotAfterRefreshFailure(t *testing.T) {
	registry := metrics.New(nil)
	expired := time.Now().Add(-time.Minute)
	source := &fakeProviderAccountMetricsSource{accounts: []provider.Account{{
		AccountType:      provider.AccountTypeAPIUpstream,
		Enabled:          true,
		Status:           provider.AccountStatusRateLimited,
		RateLimitedUntil: &expired,
	}}}
	updateProviderAccountMetrics(context.Background(), source, registry)
	source.accounts = nil
	source.err = errors.New("database unavailable")
	updateProviderAccountMetrics(context.Background(), source, registry)

	families, err := registry.Gatherer().Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, family := range families {
		if family.GetName() != "n2api_provider_accounts" {
			continue
		}
		for _, metric := range family.Metric {
			labels := map[string]string{}
			for _, label := range metric.Label {
				labels[label.GetName()] = label.GetValue()
			}
			if labels["account_type"] == "api_key" && labels["provider_state"] == "active" {
				if metric.GetGauge().GetValue() != 1 {
					t.Fatalf("provider account gauge = %v, want 1", metric.GetGauge().GetValue())
				}
				return
			}
		}
	}
	t.Fatal("active api_key provider account metric not found")
}

func TestGatewayAccountProviderReportsAccountFailures(t *testing.T) {
	var _ gateway.AccountFailureReporter = gatewayAccountProvider{}
	var _ gateway.AccountAuthorizationRefresher = gatewayAccountProvider{}
	var _ gateway.AccountUsageRecorder = gatewayAccountProvider{}
	var _ gateway.AccountRecoveryRecorder = gatewayAccountProvider{}
}

func TestGatewayAccountProviderMapsDisplayNameForRequestLogs(t *testing.T) {
	source, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("ReadFile main.go returned error: %v", err)
	}
	text := string(source)
	for _, want := range []string{
		"DisplayName:",
		"selected.DisplayName",
		"MaxConcurrentRequests:",
		"selected.MaxConcurrentRequests",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("gatewayAccountProvider mapping missing %q", want)
		}
	}
}

func TestSelectedGatewayAccountPreservesDiagnosticsOnSelectionError(t *testing.T) {
	selected, err := selectedGatewayAccount(provider.SelectedAccount{
		RoutingPoolID:            7,
		RoutingPoolName:          "primary",
		RoutingPoolFallbackDepth: 1,
		RoutingPoolFallbackChain: "primary -> secondary",
		RoutingPoolError:         provider.RoutingPoolErrorExhausted,
	}, provider.ErrModelUnavailable)

	if !errors.Is(err, provider.ErrModelUnavailable) {
		t.Fatalf("error = %v, want ErrModelUnavailable", err)
	}
	if selected.RoutingPoolID != 7 ||
		selected.RoutingPoolName != "primary" ||
		selected.RoutingPoolFallbackDepth != 1 ||
		selected.RoutingPoolFallbackChain != "primary -> secondary" ||
		selected.RoutingPoolError != provider.RoutingPoolErrorExhausted {
		t.Fatalf("selected diagnostics = %+v, want provider routing pool diagnostics preserved", selected)
	}
}

func TestMainWiresProviderAccountAutoTestRunner(t *testing.T) {
	source, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("ReadFile main.go returned error: %v", err)
	}
	text := string(source)
	for _, want := range []string{
		"signal.NotifyContext",
		"store.TryAcquireInstanceLock",
		"instance_already_running",
		"unsafe_multi_instance_enabled",
		"provider.NewAutoTestRunnerWithConfigSource",
		"adminService.GetGatewaySettings",
		"admin.NewRequestLogRetentionRunner",
		"requestLogRetentionRunner.Run",
		"store.NewResponseAffinityRepository",
		"requestLogWriteMonitor := requestlog.NewWriteMonitor(slog.Default())",
		"RequestLogObserver:    requestLogWriteMonitor",
		"gateway.NewResponseAffinityRetentionRunner",
		"responseAffinityRetentionRunner.Run",
		"ResponseAffinityStore:           responseAffinityRepo",
		"ResponseAffinityTTL:             cfg.ResponseAffinityTTL",
		"admin.NewAPIKeyBudgetMonitor",
		"go apiKeyBudgetMonitor.Run(ctx)",
		"admin.NewRoutingExhaustionProjector",
		"go routingExhaustionProjector.Run(ctx)",
		"initialAlertSubscription, err = systemEventRepo.Subscribe(ctx)",
		"InitialSubscription: initialAlertSubscription",
		"ProviderAccountAutoTestEnabled",
		"ProviderAccountAutoTestInterval",
		"ProviderAccountAutoTestIntervalSeconds",
		"go autoTestRunner.Run(ctx)",
		"go runAPIKeyCleanup(ctx, adminService, systemEventRepo, time.Hour, taskMetrics)",
		"service.PurgeExpiredAPIKeys(ctx)",
		"go runSystemEventCleanup(ctx, systemEventRepo, cfg.SystemEventRetentionDays, 24*time.Hour, taskMetrics)",
		"runSystemEventCleanupCycle(ctx, events, retentionDays, slog.Default(), time.Now, observers...)",
		"autoTestRunner, requestLogRetentionRunner, responseAffinityRetentionRunner, requestLogWriteMonitor, os.DirFS(\"frontend/build\")",
		"server.Shutdown",
		"metrics.NewHTTPServer",
		"metricsServer.Shutdown",
		"updateProviderAccountMetrics",
		"systemEventRepo.SetWriteObserver(metricsRegistry)",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("main.go missing %q", want)
		}
	}
}

func TestMainWiresAlertDispatcherAfterDatabaseCommitNotifications(t *testing.T) {
	source, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("ReadFile main.go returned error: %v", err)
	}
	text := string(source)
	for _, want := range []string{
		"store.NewAlertingRepository",
		"alerting.NewService",
		"alerting.NewHTTPAdapter",
		"alerting.NewActionTester",
		"alerting.NewDispatcher",
		"cfg.AlertDeliveryEnabled",
		"systemEventRepo.Subscribe",
		"systemEventRepo.GetByID",
		"alertDispatcher.Start()",
		"build, alertDispatcher, alertingService, alertActionTester",
		"server.Shutdown",
		"alertDispatcher.Shutdown",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("main.go missing alert dispatcher wiring %q", want)
		}
	}
	if strings.Index(text, "server.Shutdown") > strings.Index(text, "alertDispatcher.Shutdown") {
		t.Fatal("alert dispatcher shutdown must follow HTTP shutdown")
	}
}
