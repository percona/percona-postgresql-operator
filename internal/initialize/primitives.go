// Copyright 2021 - 2024 Crunchy Data Solutions, Inc.
//
// SPDX-License-Identifier: Apache-2.0

package initialize

// FromPointer returns the value that p points to.
// When p is nil, it returns the zero value of T.
func FromPointer[T any](p *T) T {
	var v T
	if p != nil {
		v = *p
	}
	return v
}

// Map initializes m when it points to nil.
func Map[M ~map[K]V, K comparable, V any](m *M) {
	// See https://pkg.go.dev/maps for similar type constraints.

	if m != nil && *m == nil {
		*m = make(M)
	}
}
