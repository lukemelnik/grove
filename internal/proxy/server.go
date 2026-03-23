package proxy

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/lukemelnik/grove/internal/certs"
	"golang.org/x/net/http2"
)

type Server struct {
	routeTable  *RouteTable
	certManager *certs.Manager
	httpServer  *http.Server
	transport   *http.Transport
	tlsEnabled  bool
	listenAddr  string
}

type ServerConfig struct {
	ListenAddr  string
	TLSEnabled  bool
	CertManager *certs.Manager
	RouteTable  *RouteTable
}

func NewServer(cfg ServerConfig) *Server {
	s := &Server{
		routeTable:  cfg.RouteTable,
		certManager: cfg.CertManager,
		tlsEnabled:  cfg.TLSEnabled,
		listenAddr:  cfg.ListenAddr,
		transport: &http.Transport{
			DialContext: (&net.Dialer{
				Timeout:   5 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			MaxIdleConns:          100,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   5 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		},
	}

	handler := http.HandlerFunc(s.handleRequest)

	s.httpServer = &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	if cfg.TLSEnabled && cfg.CertManager != nil {
		s.httpServer.TLSConfig = &tls.Config{
			GetCertificate: cfg.CertManager.GetCertificate,
			MinVersion:     tls.VersionTLS12,
			NextProtos:     []string{"h2", "http/1.1"},
		}
		if err := http2.ConfigureServer(s.httpServer, nil); err != nil {
			s.httpServer.TLSConfig.NextProtos = []string{"http/1.1"}
		}
	}

	return s
}

func (s *Server) ListenAndServe() error {
	ln, err := net.Listen("tcp", s.listenAddr)
	if err != nil {
		return fmt.Errorf("listening on %s: %w", s.listenAddr, err)
	}
	return s.Serve(ln)
}

func (s *Server) Serve(ln net.Listener) error {
	if s.tlsEnabled && s.httpServer.TLSConfig != nil {
		tlsLn := tls.NewListener(ln, s.httpServer.TLSConfig)
		return s.httpServer.Serve(tlsLn)
	}
	return s.httpServer.Serve(ln)
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}

func (s *Server) handleRequest(w http.ResponseWriter, r *http.Request) {
	hostname := extractHostname(r.Host)

	route, ok := s.routeTable.Lookup(hostname)
	if !ok {
		s.handleNotFound(w, r, hostname)
		return
	}

	if isWebSocketUpgrade(r) {
		s.handleWebSocket(w, r, route)
		return
	}

	s.handleProxy(w, r, route)
}

func (s *Server) handleProxy(w http.ResponseWriter, r *http.Request, route Route) {
	targetURL := &url.URL{
		Scheme: "http",
		Host:   route.Target,
	}

	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = targetURL.Scheme
			req.URL.Host = targetURL.Host
			req.Host = r.Host

			req.Header.Set("X-Forwarded-For", clientIP(r))
			if s.tlsEnabled {
				req.Header.Set("X-Forwarded-Proto", "https")
			} else {
				req.Header.Set("X-Forwarded-Proto", "http")
			}
			req.Header.Set("X-Forwarded-Host", r.Host)
		},
		ErrorHandler: func(rw http.ResponseWriter, req *http.Request, err error) {
			s.handleBadGateway(rw, req, route, err)
		},
		Transport: s.transport,
	}

	proxy.ServeHTTP(w, r)
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request, route Route) {
	targetConn, err := net.DialTimeout("tcp", route.Target, 5*time.Second)
	if err != nil {
		s.handleBadGateway(w, r, route, err)
		return
	}
	defer targetConn.Close()

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "WebSocket upgrade not supported", http.StatusInternalServerError)
		return
	}

	clientConn, clientBuf, err := hijacker.Hijack()
	if err != nil {
		http.Error(w, "WebSocket hijack failed", http.StatusInternalServerError)
		return
	}
	defer clientConn.Close()

	r.Header.Set("X-Forwarded-For", clientIP(r))
	if s.tlsEnabled {
		r.Header.Set("X-Forwarded-Proto", "https")
	} else {
		r.Header.Set("X-Forwarded-Proto", "http")
	}
	r.Header.Set("X-Forwarded-Host", r.Host)

	if err := r.Write(targetConn); err != nil {
		return
	}

	if clientBuf.Reader.Buffered() > 0 {
		buffered := make([]byte, clientBuf.Reader.Buffered())
		if _, err := clientBuf.Read(buffered); err == nil {
			if _, err := targetConn.Write(buffered); err != nil {
				return
			}
		}
	}

	done := make(chan struct{})
	go func() {
		io.Copy(targetConn, clientConn)
		close(done)
	}()
	io.Copy(clientConn, targetConn)
	<-done
}

func (s *Server) handleNotFound(w http.ResponseWriter, _ *http.Request, hostname string) {
	routes := s.routeTable.All()

	var sb strings.Builder
	sb.WriteString("404 — No route for ")
	sb.WriteString(hostname)
	sb.WriteString("\n\n")

	if len(routes) > 0 {
		sb.WriteString("Active routes:\n")

		byProject := make(map[string][]Route)
		for _, r := range routes {
			byProject[r.Project] = append(byProject[r.Project], r)
		}

		projects := make([]string, 0, len(byProject))
		for p := range byProject {
			projects = append(projects, p)
		}
		sort.Strings(projects)

		for _, p := range projects {
			sb.WriteString("\n  ")
			sb.WriteString(p)
			sb.WriteString(":\n")
			for _, r := range byProject[p] {
				sb.WriteString("    ")
				sb.WriteString(r.Hostname)
				sb.WriteString(" → ")
				sb.WriteString(r.Target)
				sb.WriteString("\n")
			}
		}
	} else {
		sb.WriteString("No routes registered. Start a grove project to see routes here.\n")
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	fmt.Fprint(w, sb.String())
}

func (s *Server) handleBadGateway(w http.ResponseWriter, _ *http.Request, route Route, proxyErr error) {
	var sb strings.Builder
	sb.WriteString("502 — Service not reachable\n\n")
	sb.WriteString("Route:   ")
	sb.WriteString(route.Hostname)
	sb.WriteString(" → ")
	sb.WriteString(route.Target)
	sb.WriteString("\n")
	sb.WriteString("Project: ")
	sb.WriteString(route.Project)
	sb.WriteString("\n")
	sb.WriteString("Service: ")
	sb.WriteString(route.Service)
	sb.WriteString("\n")
	sb.WriteString("Branch:  ")
	sb.WriteString(route.Branch)
	sb.WriteString("\n\n")
	sb.WriteString("The service is configured but not responding. Make sure it is running.\n")
	sb.WriteString("Error: ")
	sb.WriteString(proxyErr.Error())
	sb.WriteString("\n")

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusBadGateway)
	fmt.Fprint(w, sb.String())
}

func extractHostname(host string) string {
	h, _, err := net.SplitHostPort(host)
	if err != nil {
		return host
	}
	return h
}

func isWebSocketUpgrade(r *http.Request) bool {
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		return false
	}
	for _, v := range strings.Split(r.Header.Get("Connection"), ",") {
		if strings.EqualFold(strings.TrimSpace(v), "upgrade") {
			return true
		}
	}
	return false
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return xff
	}
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}
