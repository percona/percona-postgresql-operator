// Copyright 2021 - 2024 Crunchy Data Solutions, Inc.
//
// SPDX-License-Identifier: Apache-2.0

package pki

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/percona/percona-postgresql-operator/v2/internal/testing/require"
)

func TestCertificateTextMarshaling(t *testing.T) {
	t.Run("Zero", func(t *testing.T) {
		// Zero cannot marshal.
		_, err := Certificate{}.MarshalText()
		assert.ErrorContains(t, err, "malformed")

		// Empty cannot unmarshal.
		var sink Certificate
		assert.ErrorContains(t, sink.UnmarshalText(nil), "PEM-encoded")
		assert.ErrorContains(t, sink.UnmarshalText([]byte{}), "PEM-encoded")
	})

	root, err := NewRootCertificateAuthority()
	assert.NilError(t, err)

	cert := root.Certificate
	txt, err := cert.MarshalText()
	assert.NilError(t, err)
	assert.Assert(t, bytes.HasPrefix(txt, []byte("-----BEGIN CERTIFICATE-----\n")), "got %q", txt)
	assert.Assert(t, bytes.HasSuffix(txt, []byte("\n-----END CERTIFICATE-----\n")), "got %q", txt)

	t.Run("RoundTrip", func(t *testing.T) {
		var sink Certificate
		assert.NilError(t, sink.UnmarshalText(txt))
		assert.DeepEqual(t, cert, sink)
	})

	t.Run("Bundle", func(t *testing.T) {
		other, _ := NewRootCertificateAuthority()
		otherText, err := other.Certificate.MarshalText()
		assert.NilError(t, err)

		bundle := bytes.Join([][]byte{txt, otherText}, nil)

		// Only the first certificate of a bundle is parsed for
		// cryptographic use (e.g. signing, CommonName, DNSNames), but every
		// certificate in the bundle is kept so the full chain — such as an
		// intermediate CA followed by its root — round-trips through
		// MarshalText. Otherwise a custom root CA bundle silently loses
		// every certificate after the first when it is copied into
		// per-instance trust stores.
		var sink Certificate
		assert.NilError(t, sink.UnmarshalText(bundle))
		assert.DeepEqual(t, cert, sink)

		sinkText, err := sink.MarshalText()
		assert.NilError(t, err)
		assert.DeepEqual(t, sinkText, bundle)
	})

	t.Run("BundleIgnoresNonCertificateBlocks", func(t *testing.T) {
		// A non-certificate PEM block (e.g. a private key) appended after
		// the first certificate must never be forwarded into a trust store,
		// even though certificates that follow are kept.
		keyText, err := root.PrivateKey.MarshalText()
		assert.NilError(t, err)

		other, _ := NewRootCertificateAuthority()
		otherText, err := other.Certificate.MarshalText()
		assert.NilError(t, err)

		bundle := bytes.Join([][]byte{txt, keyText, otherText}, nil)

		var sink Certificate
		assert.NilError(t, sink.UnmarshalText(bundle))

		sinkText, err := sink.MarshalText()
		assert.NilError(t, err)
		assert.DeepEqual(t, sinkText, bytes.Join([][]byte{txt, otherText}, nil))
	})

	t.Run("EncodedEmpty", func(t *testing.T) {
		txt := []byte("-----BEGIN CERTIFICATE-----\n\n-----END CERTIFICATE-----\n")

		var sink Certificate
		assert.ErrorContains(t, sink.UnmarshalText(txt), "malformed")
	})

	t.Run("EncodedGarbage", func(t *testing.T) {
		txt := []byte("-----BEGIN CERTIFICATE-----\nasdfasdf\n-----END CERTIFICATE-----\n")

		var sink Certificate
		assert.ErrorContains(t, sink.UnmarshalText(txt), "malformed")
	})

	t.Run("ReadByOpenSSL", func(t *testing.T) {
		openssl := require.OpenSSL(t)
		dir := t.TempDir()

		certFile := filepath.Join(dir, "cert.pem")
		certBytes, err := cert.MarshalText()
		assert.NilError(t, err)
		assert.NilError(t, os.WriteFile(certFile, certBytes, 0o600))

		// The "openssl x509" command parses X.509 certificates.
		cmd := exec.Command(openssl, "x509",
			"-in", certFile, "-inform", "PEM", "-noout", "-text")

		output, err := cmd.CombinedOutput()
		assert.NilError(t, err, "%q\n%s", cmd.Args, output)
	})
}

func TestPrivateKeyTextMarshaling(t *testing.T) {
	t.Run("Zero", func(t *testing.T) {
		// Zero cannot marshal.
		_, err := PrivateKey{}.MarshalText()
		assert.ErrorContains(t, err, "unknown")

		// Empty cannot unmarshal.
		var sink PrivateKey
		assert.ErrorContains(t, sink.UnmarshalText(nil), "PEM-encoded")
		assert.ErrorContains(t, sink.UnmarshalText([]byte{}), "PEM-encoded")
	})

	root, err := NewRootCertificateAuthority()
	assert.NilError(t, err)

	key := root.PrivateKey
	txt, err := key.MarshalText()
	assert.NilError(t, err)
	assert.Assert(t, bytes.HasPrefix(txt, []byte("-----BEGIN EC PRIVATE KEY-----\n")), "got %q", txt)
	assert.Assert(t, bytes.HasSuffix(txt, []byte("\n-----END EC PRIVATE KEY-----\n")), "got %q", txt)

	t.Run("RoundTrip", func(t *testing.T) {
		var sink PrivateKey
		assert.NilError(t, sink.UnmarshalText(txt))
		assert.DeepEqual(t, key, sink)
	})

	t.Run("Bundle", func(t *testing.T) {
		other, _ := NewRootCertificateAuthority()
		otherText, err := other.PrivateKey.MarshalText()
		assert.NilError(t, err)

		bundle := bytes.Join([][]byte{txt, otherText}, nil)

		// Only the first key of a bundle is parsed.
		var sink PrivateKey
		assert.NilError(t, sink.UnmarshalText(bundle))
		assert.DeepEqual(t, key, sink)
	})

	t.Run("EncodedEmpty", func(t *testing.T) {
		txt := []byte("-----BEGIN EC PRIVATE KEY-----\n\n-----END EC PRIVATE KEY-----\n")

		var sink PrivateKey
		assert.ErrorContains(t, sink.UnmarshalText(txt), "asn1")
	})

	t.Run("EncodedGarbage", func(t *testing.T) {
		txt := []byte("-----BEGIN EC PRIVATE KEY-----\nasdfasdf\n-----END EC PRIVATE KEY-----\n")

		var sink PrivateKey
		assert.ErrorContains(t, sink.UnmarshalText(txt), "asn1")
	})

	t.Run("ReadByOpenSSL", func(t *testing.T) {
		openssl := require.OpenSSL(t)
		dir := t.TempDir()

		keyFile := filepath.Join(dir, "key.pem")
		keyBytes, err := key.MarshalText()
		assert.NilError(t, err)
		assert.NilError(t, os.WriteFile(keyFile, keyBytes, 0o600))

		// The "openssl pkey" command processes public and private keys.
		cmd := exec.Command(openssl, "pkey",
			"-in", keyFile, "-inform", "PEM", "-noout", "-text")

		output, err := cmd.CombinedOutput()
		assert.NilError(t, err, "%q\n%s", cmd.Args, output)

		assert.Assert(t,
			bytes.Contains(output, []byte("Private-Key:")),
			"expected valid private key, got:\n%s", output)

		t.Run("Check", func(t *testing.T) {
			output, _ := exec.Command(openssl, "pkey", "-help").CombinedOutput()
			if !strings.Contains(string(output), "-check") {
				t.Skip(`requires "-check" flag`)
			}

			cmd := exec.Command(openssl, "pkey",
				"-check", "-in", keyFile, "-inform", "PEM", "-noout", "-text")

			output, err := cmd.CombinedOutput()
			assert.NilError(t, err, "%q\n%s", cmd.Args, output)

			assert.Assert(t,
				bytes.Contains(output, []byte("is valid")),
				"expected valid private key, got:\n%s", output)
		})
	})
}
