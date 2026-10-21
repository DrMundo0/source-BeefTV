package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestTargetsRejectCredentialsAndGenerationPaths(t *testing.T) {
	for _, raw := range []string{"http://example.com/", "https://user:secret@example.com/", "https://example.com/?token=secret", "https://example.com/v1/images/generations", "https://example.com:8080/"} {
		if err := validateTarget(target{Name: "probe", URL: raw}); err == nil {
			t.Fatalf("accepted unsafe target %q", raw)
		}
	}
	if err := validateTarget(target{Name: "COS", URL: "https://example.com/"}); err != nil {
		t.Fatal(err)
	}
}

func TestProbeUsesOnlyHeadAndReportsPrivateBucket403AsConnectivity(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead || r.Header.Get("Authorization") != "" {
			t.Error("probe must be unauthenticated HEAD")
		}
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()
	r := probe(context.Background(), target{Name: "COS", URL: server.URL}, "direct", 1, nil)
	if r.ErrorCode != "" || r.HTTPStatus != 403 || r.ProxyUsed || r.Phase != "http_response" {
		t.Fatalf("wrong connectivity result: %+v", r)
	}
}

func TestProbeUsesSelectedProxyWithoutPersistingItsAddress(t *testing.T) {
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(401) }))
	defer proxy.Close()
	proxyURL, _ := url.Parse(proxy.URL)
	proxyURL.User = url.UserPassword("proxy-user", "proxy-secret")
	r := probe(context.Background(), target{Name: "API", URL: "http://never-resolve.invalid/v1/videos"}, "beeftv", 1, func(*http.Request) (*url.URL, error) { return proxyURL, nil })
	if !r.ProxyUsed || r.HTTPStatus != 401 || r.ErrorCode != "" {
		t.Fatalf("proxy not used: %+v", r)
	}
	data, _ := json.Marshal(r)
	for _, secret := range []string{"proxy-user", "proxy-secret", proxy.URL} {
		if strings.Contains(string(data), secret) {
			t.Fatal("report contains proxy configuration")
		}
	}
}

func TestProbeVerifiesTLSAndSanitizesNetworkErrors(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("untrusted TLS must not reach HTTP") }))
	defer server.Close()
	r := probe(context.Background(), target{Name: "TLS", URL: server.URL}, "direct", 1, nil)
	if r.ErrorCode != "tls_certificate_error" {
		t.Fatalf("untrusted certificate accepted: %+v", r)
	}
	r = probe(context.Background(), target{Name: "API", URL: "https://example.com/"}, "beeftv", 1, func(*http.Request) (*url.URL, error) { return nil, fmt.Errorf("secret proxy config") })
	if r.ErrorCode != "proxy_configuration_error" {
		t.Fatal(r)
	}
	data, _ := json.Marshal(r)
	if strings.Contains(string(data), "secret") {
		t.Fatal("raw error leaked")
	}
}

func TestDirectProbeIgnoresEnvironmentProxy(t *testing.T) {
	var calls atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(200)
	}))
	defer proxy.Close()
	for _, name := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"} {
		t.Setenv(name, proxy.URL)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	r := probe(ctx, target{Name: "direct", URL: "http://must-not-resolve.invalid/"}, "direct", 1, func(*http.Request) (*url.URL, error) {
		t.Error("direct must not consult desktop proxy")
		return url.Parse(proxy.URL)
	})
	if r.ProxyUsed || r.ErrorCode == "" || calls.Load() != 0 {
		t.Fatalf("direct unexpectedly used environment proxy: %+v calls=%d", r, calls.Load())
	}
}
