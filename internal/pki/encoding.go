// Copyright 2021 - 2024 Crunchy Data Solutions, Inc.
//
// SPDX-License-Identifier: Apache-2.0

package pki

import (
	"crypto/ecdsa"
	"crypto/x509"
	"encoding"
	"encoding/pem"

	"github.com/pkg/errors"
)

const (
	// pemLabelCertificate is the textual encoding label for an X.509 certificate
	// according to RFC 7468. See https://tools.ietf.org/html/rfc7468.
	pemLabelCertificate = "CERTIFICATE"

	// pemLabelECDSAKey is the textual encoding label for an elliptic curve private key
	// according to RFC 5915. See https://tools.ietf.org/html/rfc5915.
	pemLabelECDSAKey = "EC PRIVATE KEY"
)

var (
	_ encoding.TextMarshaler   = Certificate{}
	_ encoding.TextMarshaler   = (*Certificate)(nil)
	_ encoding.TextUnmarshaler = (*Certificate)(nil)
)

// MarshalText returns a PEM encoding of c that OpenSSL understands. When c
// was unmarshaled from a bundle containing more than one certificate, e.g. an
// intermediate certificate authority followed by its root, the additional
// certificates are included after the first so the full bundle round-trips.
func (c Certificate) MarshalText() ([]byte, error) {
	if c.x509 == nil || len(c.x509.Raw) == 0 {
		_, err := x509.ParseCertificate(nil)
		return nil, err
	}

	out := pem.EncodeToMemory(&pem.Block{
		Type:  pemLabelCertificate,
		Bytes: c.x509.Raw,
	})

	return append(out, c.chain...), nil
}

// UnmarshalText populates c from its PEM encoding. When data contains more
// than one PEM-encoded certificate, e.g. a CA bundle made up of an
// intermediate certificate authority followed by its root, the first is
// parsed for cryptographic use and every certificate is kept so the full
// bundle round-trips through MarshalText. Any other kind of PEM block
// (a private key, for example) is ignored rather than carried along.
func (c *Certificate) UnmarshalText(data []byte) error {
	block, rest := pem.Decode(data)

	if block == nil || block.Type != pemLabelCertificate {
		return errors.New("not a PEM-encoded certificate")
	}

	parsed, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return err
	}

	c.x509 = parsed
	c.chain = nil

	for {
		var next *pem.Block
		next, rest = pem.Decode(rest)
		if next == nil {
			break
		}
		if next.Type == pemLabelCertificate {
			c.chain = append(c.chain, pem.EncodeToMemory(next)...)
		}
	}

	return nil
}

var (
	_ encoding.TextMarshaler   = PrivateKey{}
	_ encoding.TextMarshaler   = (*PrivateKey)(nil)
	_ encoding.TextUnmarshaler = (*PrivateKey)(nil)
)

// MarshalText returns a PEM encoding of k that OpenSSL understands.
func (k PrivateKey) MarshalText() ([]byte, error) {
	if k.ecdsa == nil {
		k.ecdsa = new(ecdsa.PrivateKey)
	}

	der, err := x509.MarshalECPrivateKey(k.ecdsa)
	if err != nil {
		return nil, err
	}

	return pem.EncodeToMemory(&pem.Block{
		Type:  pemLabelECDSAKey,
		Bytes: der,
	}), nil
}

// UnmarshalText populates k from its PEM encoding.
func (k *PrivateKey) UnmarshalText(data []byte) error {
	block, _ := pem.Decode(data)

	if block == nil || block.Type != pemLabelECDSAKey {
		return errors.New("not a PEM-encoded private key")
	}

	key, err := x509.ParseECPrivateKey(block.Bytes)
	if err == nil {
		k.ecdsa = key
	}
	return err
}
