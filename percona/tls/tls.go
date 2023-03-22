package tls

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"time"

	"github.com/pkg/errors"
)

var validityNotAfter = time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC)
var serialNumberLimit = new(big.Int).Lsh(big.NewInt(1), 128)

func GenerateCA(privateKey *rsa.PrivateKey) (*x509.Certificate, error) {
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		return nil, errors.Wrap(err, "generate serial number for root")
	}

	caTemplate := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"Root CA"},
		},
		NotBefore:             time.Now(),
		NotAfter:              validityNotAfter,
		KeyUsage:              x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	return &caTemplate, nil
}

func GenerateCertificate(ca *x509.Certificate, privateKey *rsa.PrivateKey, hosts []string, commonName string) (*x509.Certificate, error) {
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		return nil, errors.Wrap(err, "generate serial number")
	}

	tlsTemplate := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"PGO"},
			CommonName:   commonName,
		},
		Issuer: pkix.Name{
			Organization: []string{"Root CA"},
		},
		NotBefore:             time.Now(),
		NotAfter:              validityNotAfter,
		DNSNames:              hosts,
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		IsCA:                  false,
	}

	return &tlsTemplate, nil

}

// Issue returns CA certificate, TLS certificate and TLS private key
func Issue(hosts []string) (*x509.Certificate, *x509.Certificate, *rsa.PrivateKey, error) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("generate rsa key: %w", err)
	}

	caCert, err := GenerateCA(priv)
	if err != nil {
		return nil, nil, nil, errors.Wrap(err, "generate CA certificate")
	}

	tlsCert, err := GenerateCertificate(caCert, priv, hosts, "PGO")
	if err != nil {
		return nil, nil, nil, errors.Wrap(err, "generate TLS keypair")
	}

	return caCert, tlsCert, priv, nil
}

func EncodePEM(ca *x509.Certificate, cert *x509.Certificate, priv *rsa.PrivateKey) ([]byte, []byte, []byte, error) {
	derBytes, err := x509.CreateCertificate(rand.Reader, ca, ca, &priv.PublicKey, priv)
	if err != nil {
		return nil, nil, nil, errors.Wrap(err, "create CA certificate")
	}

	caCertOut := &bytes.Buffer{}
	err = pem.Encode(caCertOut, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
	if err != nil {
		return nil, nil, nil, errors.Wrap(err, "encode CA certificate")
	}

	tlsDerBytes, err := x509.CreateCertificate(rand.Reader, cert, ca, &priv.PublicKey, priv)
	if err != nil {
		return nil, nil, nil, errors.Wrap(err, "create certificate")
	}

	tlsCertOut := &bytes.Buffer{}
	err = pem.Encode(tlsCertOut, &pem.Block{Type: "CERTIFICATE", Bytes: tlsDerBytes})
	if err != nil {
		return nil, nil, nil, errors.Wrap(err, "encode TLS  certificate")
	}

	keyOut := &bytes.Buffer{}
	block := &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)}
	err = pem.Encode(keyOut, block)
	if err != nil {
		return nil, nil, nil, errors.Wrap(err, "encode RSA private key")
	}

	return caCertOut.Bytes(), tlsCertOut.Bytes(), keyOut.Bytes(), nil
}
