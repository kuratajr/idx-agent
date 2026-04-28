package clientcore

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"fmt"
)

func verifyCertFingerprint(rawCerts [][]byte, expectedFingerprint string) error {
	if expectedFingerprint == "" {
		return nil
	}
	if len(rawCerts) == 0 {
		return fmt.Errorf("no certificates presented by server")
	}
	cert, err := x509.ParseCertificate(rawCerts[0])
	if err != nil {
		return fmt.Errorf("failed to parse certificate: %w", err)
	}
	fingerprint := sha256.Sum256(cert.Raw)
	actualFingerprint := hex.EncodeToString(fingerprint[:])
	if actualFingerprint != expectedFingerprint {
		return fmt.Errorf("certificate fingerprint mismatch: expected %s, got %s", expectedFingerprint, actualFingerprint)
	}
	return nil
}

func (c *runtimeClient) buildTLSConfig() *tls.Config {
	cfg := &tls.Config{
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: c.insecureSkipVerify || c.certFingerprint != "",
	}
	if c.certFingerprint != "" {
		cfg.VerifyPeerCertificate = func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
			return verifyCertFingerprint(rawCerts, c.certFingerprint)
		}
	}
	return cfg
}
