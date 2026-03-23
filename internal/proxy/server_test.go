package proxy

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lukemelnik/grove/internal/certs"
	"golang.org/x/net/websocket"
)

func startBackend(t *testing.T, handler http.Handler) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("starting backend listener: %v", err)
	}

	srv := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })

	return ln.Addr().String()
}

func TestServer_HTTPRouting(t *testing.T) {
	backendAddr := startBackend(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "hello from %s", r.Host)
	}))

	rt := NewRouteTable()
	rt.Update([]Route{
		{Hostname: "api.myapp.localhost", Target: backendAddr, Project: "myapp", Service: "api", Branch: "main"},
	})

	srv := NewServer(ServerConfig{
		ListenAddr: "127.0.0.1:0",
		TLSEnabled: false,
		RouteTable: rt,
	})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listening: %v", err)
	}

	go srv.httpServer.Serve(ln)
	t.Cleanup(func() { srv.Shutdown(context.Background()) })

	req, err := http.NewRequest("GET", fmt.Sprintf("http://%s/test", ln.Addr().String()), nil)
	if err != nil {
		t.Fatalf("creating request: %v", err)
	}
	req.Host = "api.myapp.localhost"

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("HTTP request failed: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if !strings.Contains(string(body), "hello from") {
		t.Errorf("unexpected body: %s", body)
	}
}

func TestServer_ForwardedHeaders(t *testing.T) {
	var gotHeaders http.Header
	backendAddr := startBackend(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header.Clone()
		w.WriteHeader(200)
	}))

	rt := NewRouteTable()
	rt.Update([]Route{
		{Hostname: "api.myapp.localhost", Target: backendAddr, Project: "myapp", Service: "api", Branch: "main"},
	})

	srv := NewServer(ServerConfig{
		ListenAddr: "127.0.0.1:0",
		TLSEnabled: false,
		RouteTable: rt,
	})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listening: %v", err)
	}

	go srv.httpServer.Serve(ln)
	t.Cleanup(func() { srv.Shutdown(context.Background()) })

	req, err := http.NewRequest("GET", fmt.Sprintf("http://%s/", ln.Addr().String()), nil)
	if err != nil {
		t.Fatalf("creating request: %v", err)
	}
	req.Host = "api.myapp.localhost"

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("HTTP request failed: %v", err)
	}
	resp.Body.Close()

	if gotHeaders.Get("X-Forwarded-Proto") != "http" {
		t.Errorf("X-Forwarded-Proto = %q, want %q", gotHeaders.Get("X-Forwarded-Proto"), "http")
	}
	if gotHeaders.Get("X-Forwarded-Host") == "" {
		t.Error("X-Forwarded-Host not set")
	}
	if gotHeaders.Get("X-Forwarded-For") == "" {
		t.Error("X-Forwarded-For not set")
	}
}

func TestServer_404_NoRoute(t *testing.T) {
	rt := NewRouteTable()
	rt.Update([]Route{
		{Hostname: "api.myapp.localhost", Target: "127.0.0.1:9999", Project: "myapp", Service: "api", Branch: "main"},
	})

	srv := NewServer(ServerConfig{
		ListenAddr: "127.0.0.1:0",
		TLSEnabled: false,
		RouteTable: rt,
	})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listening: %v", err)
	}

	go srv.httpServer.Serve(ln)
	t.Cleanup(func() { srv.Shutdown(context.Background()) })

	req, err := http.NewRequest("GET", fmt.Sprintf("http://%s/", ln.Addr().String()), nil)
	if err != nil {
		t.Fatalf("creating request: %v", err)
	}
	req.Host = "unknown.localhost"

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("HTTP request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 404 {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "No route for") {
		t.Errorf("404 body should mention 'No route for', got: %s", body)
	}
	if !strings.Contains(string(body), "api.myapp.localhost") {
		t.Errorf("404 body should list active routes, got: %s", body)
	}
}

func TestServer_502_BackendDown(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listening: %v", err)
	}
	closedAddr := ln.Addr().String()
	ln.Close()

	rt := NewRouteTable()
	rt.Update([]Route{
		{Hostname: "api.myapp.localhost", Target: closedAddr, Project: "myapp", Service: "api", Branch: "main"},
	})

	srv := NewServer(ServerConfig{
		ListenAddr: "127.0.0.1:0",
		TLSEnabled: false,
		RouteTable: rt,
	})

	proxyLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listening: %v", err)
	}

	go srv.httpServer.Serve(proxyLn)
	t.Cleanup(func() { srv.Shutdown(context.Background()) })

	req, err := http.NewRequest("GET", fmt.Sprintf("http://%s/", proxyLn.Addr().String()), nil)
	if err != nil {
		t.Fatalf("creating request: %v", err)
	}
	req.Host = "api.myapp.localhost"

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("HTTP request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 502 {
		t.Errorf("status = %d, want 502", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Service not reachable") {
		t.Errorf("502 body should mention 'Service not reachable', got: %s", body)
	}
}

func TestServer_HTTPS_Routing(t *testing.T) {
	dir := t.TempDir()
	certMgr, err := certs.EnsureCerts(dir)
	if err != nil {
		t.Fatalf("EnsureCerts failed: %v", err)
	}

	backendAddr := startBackend(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "secure hello")
	}))

	rt := NewRouteTable()
	rt.Update([]Route{
		{Hostname: "api.myapp.localhost", Target: backendAddr, Project: "myapp", Service: "api", Branch: "main"},
	})

	srv := NewServer(ServerConfig{
		ListenAddr:  "127.0.0.1:0",
		TLSEnabled:  true,
		CertManager: certMgr,
		RouteTable:  rt,
	})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listening: %v", err)
	}

	tlsLn := tls.NewListener(ln, srv.httpServer.TLSConfig)
	go srv.httpServer.Serve(tlsLn)
	t.Cleanup(func() { srv.Shutdown(context.Background()) })

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}

	req, err := http.NewRequest("GET", fmt.Sprintf("https://%s/", ln.Addr().String()), nil)
	if err != nil {
		t.Fatalf("creating request: %v", err)
	}
	req.Host = "api.myapp.localhost"

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("HTTPS request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if string(body) != "secure hello" {
		t.Errorf("body = %q, want %q", body, "secure hello")
	}
}

func TestServer_WebSocketUpgrade(t *testing.T) {
	wsSrv := httptest.NewServer(websocket.Handler(func(ws *websocket.Conn) {
		var msg string
		if err := websocket.Message.Receive(ws, &msg); err != nil {
			return
		}
		websocket.Message.Send(ws, "echo: "+msg)
	}))
	defer wsSrv.Close()

	backendAddr := strings.TrimPrefix(wsSrv.URL, "http://")

	rt := NewRouteTable()
	rt.Update([]Route{
		{Hostname: "ws.myapp.localhost", Target: backendAddr, Project: "myapp", Service: "ws", Branch: "main"},
	})

	srv := NewServer(ServerConfig{
		ListenAddr: "127.0.0.1:0",
		TLSEnabled: false,
		RouteTable: rt,
	})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listening: %v", err)
	}
	go srv.httpServer.Serve(ln)
	t.Cleanup(func() { srv.Shutdown(context.Background()) })

	proxyAddr := ln.Addr().String()
	wsCfg, err := websocket.NewConfig("ws://ws.myapp.localhost/", "http://ws.myapp.localhost/")
	if err != nil {
		t.Fatalf("creating ws config: %v", err)
	}

	conn, err := net.DialTimeout("tcp", proxyAddr, 5*time.Second)
	if err != nil {
		t.Fatalf("connecting to proxy: %v", err)
	}
	defer conn.Close()

	ws, err := websocket.NewClient(wsCfg, conn)
	if err != nil {
		t.Fatalf("WebSocket handshake failed: %v", err)
	}
	defer ws.Close()

	if err := websocket.Message.Send(ws, "hello"); err != nil {
		t.Fatalf("sending WebSocket message: %v", err)
	}

	var reply string
	if err := websocket.Message.Receive(ws, &reply); err != nil {
		t.Fatalf("receiving WebSocket message: %v", err)
	}

	if reply != "echo: hello" {
		t.Errorf("WebSocket reply = %q, want %q", reply, "echo: hello")
	}
}

func TestExtractHostname(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"api.myapp.localhost:1355", "api.myapp.localhost"},
		{"api.myapp.localhost", "api.myapp.localhost"},
		{"[::1]:1355", "::1"},
		{"localhost", "localhost"},
	}
	for _, tt := range tests {
		got := extractHostname(tt.input)
		if got != tt.want {
			t.Errorf("extractHostname(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestIsWebSocketUpgrade(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Connection", "Upgrade")
	r.Header.Set("Upgrade", "websocket")
	if !isWebSocketUpgrade(r) {
		t.Error("expected true for WebSocket upgrade")
	}

	r2 := httptest.NewRequest("GET", "/", nil)
	if isWebSocketUpgrade(r2) {
		t.Error("expected false for normal request")
	}
}

func TestServer_GracefulShutdown(t *testing.T) {
	rt := NewRouteTable()
	srv := NewServer(ServerConfig{
		ListenAddr: "127.0.0.1:0",
		TLSEnabled: false,
		RouteTable: rt,
	})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listening: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- srv.httpServer.Serve(ln)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown failed: %v", err)
	}

	select {
	case err := <-done:
		if err != nil && err != http.ErrServerClosed {
			t.Errorf("unexpected serve error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("server did not stop after shutdown")
	}
}
