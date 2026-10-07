// Copyright 2021 - 2024 Crunchy Data Solutions, Inc.
//
// SPDX-License-Identifier: Apache-2.0

package pki

import (
	"bytes"
	"encoding/pem"
	"strings"
	"testing"

	"gotest.tools/v3/assert"
)

func certPEM(t *testing.T, root *RootCertificateAuthority) []byte {
	t.Helper()
	text, err := root.Certificate.MarshalText()
	assert.NilError(t, err)
	return text
}

func certCount(b []byte) int {
	return strings.Count(string(b), "-----BEGIN CERTIFICATE-----")
}

func TestTrustBundle(t *testing.T) {
	t.Parallel()

	one, err := NewRootCertificateAuthority()
	assert.NilError(t, err, "bug in test")
	two, err := NewRootCertificateAuthority()
	assert.NilError(t, err, "bug in test")

	onePEM := certPEM(t, one)
	twoPEM := certPEM(t, two)

	t.Run("Empty", func(t *testing.T) {
		assert.Assert(t, TrustBundle() == nil)
		assert.Assert(t, TrustBundle(nil, []byte{}) == nil)
	})

	t.Run("SingleInputIsUnchanged", func(t *testing.T) {
		// The no-additional-CAs path must be byte-identical to what the
		// operator writes today.
		assert.DeepEqual(t, TrustBundle(onePEM), onePEM)
	})

	t.Run("OrderPreserved", func(t *testing.T) {
		assert.DeepEqual(t, TrustBundle(onePEM, twoPEM), append(append([]byte{}, onePEM...), twoPEM...))
	})

	t.Run("DuplicatesDropped", func(t *testing.T) {
		assert.Equal(t, certCount(TrustBundle(onePEM, onePEM)), 1)
		assert.DeepEqual(t, TrustBundle(onePEM, onePEM), onePEM)

		// A duplicate arriving inside a multi-certificate input is dropped too.
		both := append(append([]byte{}, onePEM...), twoPEM...)
		assert.Equal(t, certCount(TrustBundle(both, onePEM, twoPEM)), 2)
	})

	t.Run("Idempotent", func(t *testing.T) {
		once := TrustBundle(onePEM, twoPEM)
		assert.DeepEqual(t, TrustBundle(once), once)
	})

	t.Run("WhitespaceNormalized", func(t *testing.T) {
		padded := append([]byte("\n\n"), onePEM...)
		padded = append(padded, '\n')
		assert.DeepEqual(t, TrustBundle(padded), onePEM)

		// A Secret whose ca.crt has no trailing newline is common, and must
		// still contribute its certificate.
		assert.DeepEqual(t, TrustBundle(bytes.TrimRight(onePEM, "\n")), onePEM)
	})

	t.Run("OutputIsAlwaysSeparated", func(t *testing.T) {
		// Every block is re-encoded, so entries are newline-terminated no
		// matter how the inputs were formatted. This is what the pgBouncer
		// append had to check for by hand.
		bundle := TrustBundle(bytes.TrimRight(onePEM, "\n"), bytes.TrimRight(twoPEM, "\n"))
		assert.Equal(t, certCount(bundle), 2)
		assert.Assert(t, bytes.HasSuffix(bundle, []byte("-----END CERTIFICATE-----\n")))

		// ...and the result round-trips through the parser unchanged.
		assert.DeepEqual(t, TrustBundle(bundle), bundle)
	})

	t.Run("NonCertificateBlocksIgnored", func(t *testing.T) {
		key, err := one.PrivateKey.MarshalText()
		assert.NilError(t, err)

		bundle := TrustBundle(append(append([]byte{}, key...), onePEM...))
		assert.DeepEqual(t, bundle, onePEM)
		assert.Assert(t, !strings.Contains(string(bundle), "PRIVATE KEY"))
	})

	t.Run("GarbageIgnored", func(t *testing.T) {
		assert.Assert(t, TrustBundle([]byte("not pem at all")) == nil)
		assert.DeepEqual(t, TrustBundle([]byte("junk\n"), onePEM), onePEM)
	})

	t.Run("MalformedCertificateBlockIgnored", func(t *testing.T) {
		// A block labeled CERTIFICATE whose DER does not actually decode as
		// an X.509 certificate must not be treated as one: accepting it
		// would let malformed input reach a trust file unexamined.
		fake := []byte("-----BEGIN CERTIFICATE-----\nAAAA\n-----END CERTIFICATE-----\n")
		assert.Assert(t, TrustBundle(fake) == nil)
		assert.DeepEqual(t, TrustBundle(fake, onePEM), onePEM)
	})

	t.Run("CrossSignedVariantsBothKept", func(t *testing.T) {
		// Two certificates that share a subject and public key but differ in
		// their DER are distinct trust anchors; dedup must not collapse them.
		block, _ := pem.Decode(onePEM)
		assert.Assert(t, block != nil)

		altered := append([]byte{}, block.Bytes...)
		altered[len(altered)-1] ^= 0xff
		alteredPEM := pem.EncodeToMemory(&pem.Block{Type: pemLabelCertificate, Bytes: altered})

		assert.Equal(t, certCount(TrustBundle(onePEM, alteredPEM)), 2)
	})
}
