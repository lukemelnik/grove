package certs

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	CACertFile  = "ca.pem"
	CACommonName = "Grove Local CA"

	caKeyFile        = "ca-key.pem"
	hostCertsDir     = "host-certs"
	caOrg            = "Grove"
	caValidityYears  = 10
	certValidityDays = 365
	renewalWindow    = 7 * 24 * time.Hour
)

type Manager struct {
	stateDir string
	caCert   *x509.Certificate
	caKey    *ecdsa.PrivateKey

	mu       sync.Mutex
	cache    map[string]*tls.Certificate
	inflight map[string]*inflight
}

type inflight struct {
	wg   sync.WaitGroup
	cert *tls.Certificate
	err  error
}

func EnsureCerts(stateDir string) (*Manager, error) {
	m := &Manager{
		stateDir: stateDir,
		cache:    make(map[string]*tls.Certificate),
		inflight: make(map[string]*inflight),
	}

	if err := os.MkdirAll(stateDir, 0755); err != nil {
		return nil, fmt.Errorf("creating state directory: %w", err)
	}

	if err := m.ensureCA(); err != nil {
		return nil, fmt.Errorf("ensuring CA: %w", err)
	}

	return m, nil
}

func (m *Manager) CACertPath() string {
	return filepath.Join(m.stateDir, CACertFile)
}

func (m *Manager) GetCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	hostname := hello.ServerName
	if hostname == "" {
		hostname = "localhost"
	}

	m.mu.Lock()
	if cert, ok := m.cache[hostname]; ok {
		if cert.Leaf != nil && !needsRenewal(cert.Leaf) {
			m.mu.Unlock()
			return cert, nil
		}
		delete(m.cache, hostname)
	}

	if inf, ok := m.inflight[hostname]; ok {
		m.mu.Unlock()
		inf.wg.Wait()
		return inf.cert, inf.err
	}

	inf := &inflight{}
	inf.wg.Add(1)
	m.inflight[hostname] = inf
	m.mu.Unlock()

	cert, err := m.loadOrGenerateHostCert(hostname)

	inf.cert = cert
	inf.err = err
	inf.wg.Done()

	m.mu.Lock()
	delete(m.inflight, hostname)
	if err == nil {
		m.cache[hostname] = cert
	}
	m.mu.Unlock()

	return cert, err
}

var DefaultStateDir = func() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("determining home directory: %w", err)
	}
	return filepath.Join(home, ".grove", "proxy"), nil
}

func (m *Manager) ensureCA() error {
	certPath := filepath.Join(m.stateDir, CACertFile)
	keyPath := filepath.Join(m.stateDir, caKeyFile)

	caCert, caKey, err := loadKeyPair(certPath, keyPath)
	if err == nil && !needsRenewal(caCert) {
		m.caCert = caCert
		m.caKey = caKey
		return nil
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("generating CA key: %w", err)
	}

	serial, err := generateSerialNumber()
	if err != nil {
		return fmt.Errorf("generating serial number: %w", err)
	}

	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   CACommonName,
			Organization: []string{caOrg},
		},
		NotBefore:             now,
		NotAfter:              now.AddDate(caValidityYears, 0, 0),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            0,
		MaxPathLenZero:        true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return fmt.Errorf("creating CA certificate: %w", err)
	}

	caCert, err = x509.ParseCertificate(certDER)
	if err != nil {
		return fmt.Errorf("parsing CA certificate: %w", err)
	}

	if err := saveCertAndKey(certPath, keyPath, certDER, key); err != nil {
		return fmt.Errorf("saving CA: %w", err)
	}

	m.caCert = caCert
	m.caKey = key

	return m.clearHostCerts()
}

func (m *Manager) loadOrGenerateHostCert(hostname string) (*tls.Certificate, error) {
	dir := filepath.Join(m.stateDir, hostCertsDir)
	certPath := filepath.Join(dir, hostname+".pem")
	keyPath := filepath.Join(dir, hostname+"-key.pem")

	tlsCert, err := loadTLSCert(certPath, keyPath)
	if err == nil && tlsCert.Leaf != nil && !needsRenewal(tlsCert.Leaf) {
		return tlsCert, nil
	}

	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("creating host-certs directory: %w", err)
	}

	return m.generateHostCert(hostname, certPath, keyPath)
}

func (m *Manager) generateHostCert(hostname, certPath, keyPath string) (*tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generating host key: %w", err)
	}

	serial, err := generateSerialNumber()
	if err != nil {
		return nil, fmt.Errorf("generating serial number: %w", err)
	}

	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName: hostname,
		},
		DNSNames:    []string{hostname},
		NotBefore:   now,
		NotAfter:    now.AddDate(0, 0, certValidityDays),
		KeyUsage:    x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, m.caCert, &key.PublicKey, m.caKey)
	if err != nil {
		return nil, fmt.Errorf("creating host certificate: %w", err)
	}

	if err := saveCertAndKey(certPath, keyPath, certDER, key); err != nil {
		return nil, fmt.Errorf("saving host cert: %w", err)
	}

	leaf, err := x509.ParseCertificate(certDER)
	if err != nil {
		return nil, fmt.Errorf("parsing host certificate: %w", err)
	}

	return &tls.Certificate{
		Certificate: [][]byte{certDER},
		PrivateKey:  key,
		Leaf:        leaf,
	}, nil
}

func (m *Manager) clearHostCerts() error {
	dir := filepath.Join(m.stateDir, hostCertsDir)
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("clearing host certs: %w", err)
	}
	m.cache = make(map[string]*tls.Certificate)
	return nil
}

func needsRenewal(cert *x509.Certificate) bool {
	if cert == nil {
		return true
	}
	return time.Now().Add(renewalWindow).After(cert.NotAfter)
}

func generateSerialNumber() (*big.Int, error) {
	limit := new(big.Int).Lsh(big.NewInt(1), 128)
	return rand.Int(rand.Reader, limit)
}

func loadKeyPair(certPath, keyPath string) (*x509.Certificate, *ecdsa.PrivateKey, error) {
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return nil, nil, err
	}

	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, nil, err
	}

	certBlock, _ := pem.Decode(certPEM)
	if certBlock == nil {
		return nil, nil, fmt.Errorf("failed to decode certificate PEM from %s", certPath)
	}

	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return nil, nil, fmt.Errorf("parsing certificate: %w", err)
	}

	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil {
		return nil, nil, fmt.Errorf("failed to decode key PEM from %s", keyPath)
	}

	key, err := x509.ParseECPrivateKey(keyBlock.Bytes)
	if err != nil {
		return nil, nil, fmt.Errorf("parsing private key: %w", err)
	}

	return cert, key, nil
}

func loadTLSCert(certPath, keyPath string) (*tls.Certificate, error) {
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return nil, err
	}

	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, err
	}

	tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, err
	}

	leaf, err := x509.ParseCertificate(tlsCert.Certificate[0])
	if err != nil {
		return nil, err
	}
	tlsCert.Leaf = leaf

	return &tlsCert, nil
}

func saveCertAndKey(certPath, keyPath string, certDER []byte, key *ecdsa.PrivateKey) error {
	certPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certDER,
	})

	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return fmt.Errorf("marshaling private key: %w", err)
	}

	keyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "EC PRIVATE KEY",
		Bytes: keyDER,
	})

	if err := os.WriteFile(certPath, certPEM, 0644); err != nil {
		return fmt.Errorf("writing certificate: %w", err)
	}

	if err := os.WriteFile(keyPath, keyPEM, 0600); err != nil {
		return fmt.Errorf("writing private key: %w", err)
	}

	return nil
}
