// Copyright 2021 - 2024 Crunchy Data Solutions, Inc.
//
// SPDX-License-Identifier: Apache-2.0

package pki

import "encoding/pem"

// TrustBundle concatenates the PEM-encoded certificates found in inputs into a
// single CA bundle. Certificates keep the order they are given in, exact
// duplicates are dropped, and PEM blocks that are not certificates (a private
// key, for example) are ignored. Identical inputs always produce identical
// bytes, so a bundle written to a Secret does not churn between reconciles.
//
// Duplicates are compared by their DER encoding rather than by subject or
// public key: a self-signed root and a cross-signed variant of the same
// authority share both of those but are different certificates, and dropping
// either one breaks chain building for clients that need that path.
func TrustBundle(inputs ...[]byte) []byte {
	var out []byte
	seen := make(map[string]bool)

	for _, input := range inputs {
		for rest := input; len(rest) > 0; {
			var block *pem.Block
			if block, rest = pem.Decode(rest); block == nil {
				break
			}
			if block.Type != pemLabelCertificate || seen[string(block.Bytes)] {
				continue
			}
			seen[string(block.Bytes)] = true

			// Re-encode rather than copying the input so that bundles which
			// differ only in whitespace or PEM headers produce the same bytes.
			out = append(out, pem.EncodeToMemory(&pem.Block{
				Type:  pemLabelCertificate,
				Bytes: block.Bytes,
			})...)
		}
	}

	return out
}
