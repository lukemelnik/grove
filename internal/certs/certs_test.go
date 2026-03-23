package certs

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestEnsureCerts_GeneratesCA(t *testing.T) {
	dir := t.TempDir()
	m, err := EnsureCerts(dir)
	if err != nil {
		t.Fatalf("EnsureCerts failed: %v", err)
	}

	certPath := filepath.Join(dir, CACertFile)
	keyPath := filepath.Join(dir, caKeyFile)

	if _, err := os.Stat(certPath); err != nil {
		t.Fatalf("CA cert file not created: %v", err)
	}
	if _, err := os.Stat(keyPath); err != nil {
		t.Fatalf("CA key file not created: %v", err)
	}

	if m.caCert == nil {
		t.Fatal("CA cert not loaded into manager")
	}
	if m.caKey == nil {
		t.Fatal("CA key not loaded into manager")
	}
	if !m.caCert.IsCA {
		t.Error("CA cert does not have IsCA flag")
	}
	if m.caCert.Subject.CommonName != CACommonName {
		t.Errorf("CA CN = %q, want %q", m.caCert.Subject.CommonName, CACommonName)
	}
}

func TestEnsureCerts_LoadsExistingCA(t *testing.T) {
	dir := t.TempDir()
	m1, err := EnsureCerts(dir)
	if err != nil {
		t.Fatalf("first EnsureCerts failed: %v", err)
	}

	serial1 := m1.caCert.SerialNumber

	m2, err := EnsureCerts(dir)
	if err != nil {
		t.Fatalf("second EnsureCerts failed: %v", err)
	}

	if m2.caCert.SerialNumber.Cmp(serial1) != 0 {
		t.Error("CA was regenerated instead of loaded — serial numbers differ")
	}
}

func TestGetCertificate_GeneratesHostCert(t *testing.T) {
	dir := t.TempDir()
	m, err := EnsureCerts(dir)
	if err != nil {
		t.Fatalf("EnsureCerts failed: %v", err)
	}

	hello := &tls.ClientHelloInfo{ServerName: "api.myapp.localhost"}
	cert, err := m.GetCertificate(hello)
	if err != nil {
		t.Fatalf("GetCertificate failed: %v", err)
	}

	if cert == nil {
		t.Fatal("GetCertificate returned nil cert")
	}
	if cert.Leaf == nil {
		t.Fatal("cert.Leaf is nil")
	}
	if cert.Leaf.Subject.CommonName != "api.myapp.localhost" {
		t.Errorf("cert CN = %q, want %q", cert.Leaf.Subject.CommonName, "api.myapp.localhost")
	}

	found := false
	for _, name := range cert.Leaf.DNSNames {
		if name == "api.myapp.localhost" {
			found = true
			break
		}
	}
	if !found {
		t.Error("hostname not in cert SAN list")
	}

	err = cert.Leaf.CheckSignatureFrom(m.caCert)
	if err != nil {
		t.Errorf("cert not signed by CA: %v", err)
	}
}

func TestGetCertificate_EmptyServerName(t *testing.T) {
	dir := t.TempDir()
	m, err := EnsureCerts(dir)
	if err != nil {
		t.Fatalf("EnsureCerts failed: %v", err)
	}

	hello := &tls.ClientHelloInfo{ServerName: ""}
	cert, err := m.GetCertificate(hello)
	if err != nil {
		t.Fatalf("GetCertificate failed: %v", err)
	}

	if cert.Leaf.Subject.CommonName != "localhost" {
		t.Errorf("cert CN = %q, want %q", cert.Leaf.Subject.CommonName, "localhost")
	}
}

func TestGetCertificate_CachesInMemory(t *testing.T) {
	dir := t.TempDir()
	m, err := EnsureCerts(dir)
	if err != nil {
		t.Fatalf("EnsureCerts failed: %v", err)
	}

	hello := &tls.ClientHelloInfo{ServerName: "test.localhost"}
	cert1, err := m.GetCertificate(hello)
	if err != nil {
		t.Fatalf("first GetCertificate failed: %v", err)
	}

	cert2, err := m.GetCertificate(hello)
	if err != nil {
		t.Fatalf("second GetCertificate failed: %v", err)
	}

	if cert1 != cert2 {
		t.Error("expected same pointer from memory cache")
	}
}

func TestGetCertificate_CachesOnDisk(t *testing.T) {
	dir := t.TempDir()
	m1, err := EnsureCerts(dir)
	if err != nil {
		t.Fatalf("EnsureCerts failed: %v", err)
	}

	hello := &tls.ClientHelloInfo{ServerName: "disk.localhost"}
	cert1, err := m1.GetCertificate(hello)
	if err != nil {
		t.Fatalf("first GetCertificate failed: %v", err)
	}

	serial1 := cert1.Leaf.SerialNumber

	m2, err := EnsureCerts(dir)
	if err != nil {
		t.Fatalf("second EnsureCerts failed: %v", err)
	}

	cert2, err := m2.GetCertificate(hello)
	if err != nil {
		t.Fatalf("second GetCertificate failed: %v", err)
	}

	if cert2.Leaf.SerialNumber.Cmp(serial1) != 0 {
		t.Error("cert was regenerated instead of loaded from disk")
	}
}

func TestGetCertificate_RegeneratesExpired(t *testing.T) {
	dir := t.TempDir()
	m, err := EnsureCerts(dir)
	if err != nil {
		t.Fatalf("EnsureCerts failed: %v", err)
	}

	hostname := "expired.localhost"
	hello := &tls.ClientHelloInfo{ServerName: hostname}
	cert1, err := m.GetCertificate(hello)
	if err != nil {
		t.Fatalf("first GetCertificate failed: %v", err)
	}
	serial1 := cert1.Leaf.SerialNumber

	now := time.Now()
	writeTestHostCert(t, m, hostname, now.AddDate(0, 0, -2), now.AddDate(0, 0, -1))

	m.mu.Lock()
	delete(m.cache, hostname)
	m.mu.Unlock()

	cert2, err := m.GetCertificate(hello)
	if err != nil {
		t.Fatalf("second GetCertificate failed: %v", err)
	}

	if cert2.Leaf.SerialNumber.Cmp(serial1) == 0 {
		t.Error("expected cert to be regenerated for expired cert")
	}
}

func TestGetCertificate_RegeneratesExpiringSoon(t *testing.T) {
	dir := t.TempDir()
	m, err := EnsureCerts(dir)
	if err != nil {
		t.Fatalf("EnsureCerts failed: %v", err)
	}

	hostname := "expiring.localhost"
	hello := &tls.ClientHelloInfo{ServerName: hostname}
	cert1, err := m.GetCertificate(hello)
	if err != nil {
		t.Fatalf("first GetCertificate failed: %v", err)
	}
	serial1 := cert1.Leaf.SerialNumber

	now := time.Now()
	writeTestHostCert(t, m, hostname, now.AddDate(0, 0, -30), now.Add(3*24*time.Hour))

	m.mu.Lock()
	delete(m.cache, hostname)
	m.mu.Unlock()

	cert2, err := m.GetCertificate(hello)
	if err != nil {
		t.Fatalf("second GetCertificate failed: %v", err)
	}

	if cert2.Leaf.SerialNumber.Cmp(serial1) == 0 {
		t.Error("expected cert to be regenerated for soon-to-expire cert")
	}
}

func TestGetCertificate_DeduplicatesConcurrent(t *testing.T) {
	dir := t.TempDir()
	m, err := EnsureCerts(dir)
	if err != nil {
		t.Fatalf("EnsureCerts failed: %v", err)
	}

	hostname := "concurrent.localhost"
	const goroutines = 50

	certs := make([]*tls.Certificate, goroutines)
	errs := make([]error, goroutines)

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := range goroutines {
		go func(idx int) {
			defer wg.Done()
			hello := &tls.ClientHelloInfo{ServerName: hostname}
			c, e := m.GetCertificate(hello)
			certs[idx] = c
			errs[idx] = e
		}(i)
	}

	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d failed: %v", i, err)
		}
	}

	first := certs[0]
	for i := 1; i < goroutines; i++ {
		if certs[i] != first {
			if certs[i].Leaf.SerialNumber.Cmp(first.Leaf.SerialNumber) != 0 {
				t.Errorf("goroutine %d got different cert serial", i)
			}
		}
	}

	entries, err := os.ReadDir(filepath.Join(dir, hostCertsDir))
	if err != nil {
		t.Fatalf("reading host-certs dir: %v", err)
	}
	certCount := 0
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".pem" && e.Name() != hostname+"-key.pem" {
			certCount++
		}
	}
	if certCount != 1 {
		t.Errorf("expected 1 cert file for hostname, got %d", certCount)
	}
}

func TestEnsureCA_CascadesClearHostCerts(t *testing.T) {
	dir := t.TempDir()
	m, err := EnsureCerts(dir)
	if err != nil {
		t.Fatalf("first EnsureCerts failed: %v", err)
	}

	hello := &tls.ClientHelloInfo{ServerName: "cascade.localhost"}
	_, err = m.GetCertificate(hello)
	if err != nil {
		t.Fatalf("GetCertificate failed: %v", err)
	}

	hostCertPath := filepath.Join(dir, hostCertsDir, "cascade.localhost.pem")
	if _, err := os.Stat(hostCertPath); err != nil {
		t.Fatalf("host cert file should exist: %v", err)
	}

	os.Remove(filepath.Join(dir, CACertFile))
	os.Remove(filepath.Join(dir, caKeyFile))

	_, err = EnsureCerts(dir)
	if err != nil {
		t.Fatalf("second EnsureCerts failed: %v", err)
	}

	if _, err := os.Stat(hostCertPath); !os.IsNotExist(err) {
		t.Error("host cert file should have been cleared after CA regeneration")
	}
}

func TestFilePermissions(t *testing.T) {
	dir := t.TempDir()
	m, err := EnsureCerts(dir)
	if err != nil {
		t.Fatalf("EnsureCerts failed: %v", err)
	}

	caKeyInfo, err := os.Stat(filepath.Join(dir, caKeyFile))
	if err != nil {
		t.Fatalf("stat CA key: %v", err)
	}
	if caKeyInfo.Mode().Perm() != 0600 {
		t.Errorf("CA key permissions = %o, want 0600", caKeyInfo.Mode().Perm())
	}

	caCertInfo, err := os.Stat(filepath.Join(dir, CACertFile))
	if err != nil {
		t.Fatalf("stat CA cert: %v", err)
	}
	if caCertInfo.Mode().Perm() != 0644 {
		t.Errorf("CA cert permissions = %o, want 0644", caCertInfo.Mode().Perm())
	}

	hello := &tls.ClientHelloInfo{ServerName: "perms.localhost"}
	_, err = m.GetCertificate(hello)
	if err != nil {
		t.Fatalf("GetCertificate failed: %v", err)
	}

	hostKeyInfo, err := os.Stat(filepath.Join(dir, hostCertsDir, "perms.localhost-key.pem"))
	if err != nil {
		t.Fatalf("stat host key: %v", err)
	}
	if hostKeyInfo.Mode().Perm() != 0600 {
		t.Errorf("host key permissions = %o, want 0600", hostKeyInfo.Mode().Perm())
	}

	hostCertInfo, err := os.Stat(filepath.Join(dir, hostCertsDir, "perms.localhost.pem"))
	if err != nil {
		t.Fatalf("stat host cert: %v", err)
	}
	if hostCertInfo.Mode().Perm() != 0644 {
		t.Errorf("host cert permissions = %o, want 0644", hostCertInfo.Mode().Perm())
	}
}

func TestNeedsRenewal(t *testing.T) {
	if !needsRenewal(nil) {
		t.Error("nil cert should need renewal")
	}

	now := time.Now()
	expired := &x509.Certificate{NotAfter: now.AddDate(0, 0, -1)}
	if !needsRenewal(expired) {
		t.Error("expired cert should need renewal")
	}

	expiringSoon := &x509.Certificate{NotAfter: now.Add(3 * 24 * time.Hour)}
	if !needsRenewal(expiringSoon) {
		t.Error("cert expiring within 7 days should need renewal")
	}

	valid := &x509.Certificate{NotAfter: now.Add(30 * 24 * time.Hour)}
	if needsRenewal(valid) {
		t.Error("cert with 30 days remaining should not need renewal")
	}
}

func TestCACertPath(t *testing.T) {
	dir := t.TempDir()
	m, err := EnsureCerts(dir)
	if err != nil {
		t.Fatalf("EnsureCerts failed: %v", err)
	}

	want := filepath.Join(dir, CACertFile)
	if m.CACertPath() != want {
		t.Errorf("CACertPath() = %q, want %q", m.CACertPath(), want)
	}
}

func TestMultipleHostnames(t *testing.T) {
	dir := t.TempDir()
	m, err := EnsureCerts(dir)
	if err != nil {
		t.Fatalf("EnsureCerts failed: %v", err)
	}

	hostnames := []string{
		"api.myapp.localhost",
		"web.myapp.localhost",
		"api.feat-auth.myapp.localhost",
	}

	for _, hostname := range hostnames {
		hello := &tls.ClientHelloInfo{ServerName: hostname}
		cert, err := m.GetCertificate(hello)
		if err != nil {
			t.Fatalf("GetCertificate(%q) failed: %v", hostname, err)
		}
		if cert.Leaf.Subject.CommonName != hostname {
			t.Errorf("cert CN = %q, want %q", cert.Leaf.Subject.CommonName, hostname)
		}
	}
}

func writeTestHostCert(t *testing.T, m *Manager, hostname string, notBefore, notAfter time.Time) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating test key: %v", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		t.Fatalf("generating test serial: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: hostname},
		DNSNames:     []string{hostname},
		NotBefore:    notBefore,
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, m.caCert, &key.PublicKey, m.caKey)
	if err != nil {
		t.Fatalf("creating test cert: %v", err)
	}

	dir := filepath.Join(m.stateDir, hostCertsDir)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("creating host-certs dir: %v", err)
	}

	certPath := filepath.Join(dir, hostname+".pem")
	keyPath := filepath.Join(dir, hostname+"-key.pem")

	if err := saveCertAndKey(certPath, keyPath, certDER, key); err != nil {
		t.Fatalf("saving test cert: %v", err)
	}
}
