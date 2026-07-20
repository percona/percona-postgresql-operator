// Copyright 2021 - 2024 Crunchy Data Solutions, Inc.
//
// SPDX-License-Identifier: Apache-2.0

package pgbouncer

import (
	corev1 "k8s.io/api/core/v1"
)

const (
	tlsAuthoritySecretKey   = "ca.crt"
	tlsCertificateSecretKey = corev1.TLSCertKey
	tlsPrivateKeySecretKey  = corev1.TLSPrivateKeyKey

	certBackendAuthorityAbsolutePath   = configDirectory + "/" + certBackendAuthorityProjectionPath
	certBackendAuthorityProjectionPath = "~postgres-operator/backend-ca.crt"

	certFrontendAuthorityAbsolutePath  = configDirectory + "/" + certFrontendAuthorityProjectionPath
	certFrontendPrivateKeyAbsolutePath = configDirectory + "/" + certFrontendPrivateKeyProjectionPath
	certFrontendAbsolutePath           = configDirectory + "/" + certFrontendProjectionPath

	certFrontendAuthorityProjectionPath  = "~postgres-operator/frontend-ca.crt"
	certFrontendPrivateKeyProjectionPath = "~postgres-operator/frontend-tls.key"
	certFrontendProjectionPath           = "~postgres-operator/frontend-tls.crt"

	certFrontendAuthoritySecretKey  = "pgbouncer-frontend.ca-roots"
	certFrontendPrivateKeySecretKey = "pgbouncer-frontend.key"
	certFrontendSecretKey           = "pgbouncer-frontend.crt"
)

// backendAuthority creates a volume projection of the PostgreSQL server
// certificate authority.
func backendAuthority(postgres *corev1.SecretProjection) corev1.VolumeProjection {
	var items []corev1.KeyToPath
	result := postgres.DeepCopy()

	for i := range result.Items {
		// The PostgreSQL server projection expects Path to match typical Keys.
		if result.Items[i].Path == tlsAuthoritySecretKey {
			result.Items[i].Path = certBackendAuthorityProjectionPath
			items = append(items, result.Items[i])
		}
	}

	if len(items) == 0 {
		items = []corev1.KeyToPath{{
			Key:  tlsAuthoritySecretKey,
			Path: certBackendAuthorityProjectionPath,
		}}
	}

	result.Items = items
	return corev1.VolumeProjection{Secret: result}
}

// CustomTLSAuthorityKey returns the key within the custom PgBouncer TLS
// Secret that holds its certificate authority, honoring any remapping in
// the projection items.
func CustomTLSAuthorityKey(projection *corev1.SecretProjection) string {
	for _, item := range projection.Items {
		// The custom projection expects Path to match typical Keys.
		if item.Path == tlsAuthoritySecretKey {
			return item.Key
		}
	}
	return tlsAuthoritySecretKey
}

// frontendCertificate creates volume projections of the PgBouncer certificate.
func frontendCertificate(
	custom *corev1.SecretProjection, secret *corev1.Secret, additionalCAs bool,
) []corev1.VolumeProjection {
	if custom == nil {
		return []corev1.VolumeProjection{{Secret: &corev1.SecretProjection{
			LocalObjectReference: corev1.LocalObjectReference{
				Name: secret.Name,
			},
			Items: []corev1.KeyToPath{
				{
					Key:  certFrontendAuthoritySecretKey,
					Path: certFrontendAuthorityProjectionPath,
				},
				{
					Key:  certFrontendPrivateKeySecretKey,
					Path: certFrontendPrivateKeyProjectionPath,
				},
				{
					Key:  certFrontendSecretKey,
					Path: certFrontendProjectionPath,
				},
			},
		}}}
	}

	// The custom projection may have more or less than the three items we need
	// to mount. Search for items that have the Path we expect and mount them at
	// the path we need. When no items are specified, the Key serves as the Path.

	// TODO(cbandy): A more structured field or validating webhook would ensure
	// that the necessary values are specified.

	var items []corev1.KeyToPath
	result := custom.DeepCopy()

	for i := range result.Items {
		// The custom projection expects Path to match typical Keys.
		switch result.Items[i].Path {
		case tlsAuthoritySecretKey:
			// K8SPG-952: with additional CAs, the authority is mounted from
			// the operator Secret below where the merged bundle is stored.
			if additionalCAs {
				continue
			}
			result.Items[i].Path = certFrontendAuthorityProjectionPath
			items = append(items, result.Items[i])

		case tlsCertificateSecretKey:
			result.Items[i].Path = certFrontendProjectionPath
			items = append(items, result.Items[i])

		case tlsPrivateKeySecretKey:
			result.Items[i].Path = certFrontendPrivateKeyProjectionPath
			items = append(items, result.Items[i])
		}
	}

	if len(items) == 0 {
		if !additionalCAs {
			items = append(items, corev1.KeyToPath{
				Key:  tlsAuthoritySecretKey,
				Path: certFrontendAuthorityProjectionPath,
			})
		}
		items = append(items,
			corev1.KeyToPath{
				Key:  tlsPrivateKeySecretKey,
				Path: certFrontendPrivateKeyProjectionPath,
			},
			corev1.KeyToPath{
				Key:  tlsCertificateSecretKey,
				Path: certFrontendProjectionPath,
			},
		)
	}

	result.Items = items
	projections := []corev1.VolumeProjection{{Secret: result}}

	// K8SPG-952: mount the merged CA bundle from the operator Secret so
	// PgBouncer also trusts the additional authorities.
	if additionalCAs {
		projections = append(projections, corev1.VolumeProjection{
			Secret: &corev1.SecretProjection{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: secret.Name,
				},
				Items: []corev1.KeyToPath{{
					Key:  certFrontendAuthoritySecretKey,
					Path: certFrontendAuthorityProjectionPath,
				}},
			},
		})
	}
	return projections
}
