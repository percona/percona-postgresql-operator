// Copyright 2021 - 2024 Crunchy Data Solutions, Inc.
//
// SPDX-License-Identifier: Apache-2.0

package pgcluster

import (
	"context"

	"github.com/hashicorp/go-multierror"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v2 "github.com/percona/percona-postgresql-operator/v2/pkg/apis/pgv2.percona.com/v2"
	v1beta1 "github.com/percona/percona-postgresql-operator/v2/pkg/apis/upstream.pgv2.percona.com/v1beta1"
)

// reconcilePatroniEtcd validates that secrets referenced in the etcd DCS
// configuration exist and contain the required keys. Emits Warning events for
// every problem found and returns a combined error so the operator surfaces all
// issues in a single reconcile cycle.
func (r *PGClusterReconciler) reconcilePatroniEtcd(ctx context.Context, cr *v2.PerconaPGCluster) error {
	if cr.Spec.Patroni == nil {
		return nil
	}
	if cr.Spec.Patroni.DCSType() != v1beta1.PatroniDCSTypeEtcd {
		return nil
	}
	dcs := cr.Spec.Patroni.GetDCS()
	if dcs.Etcd == nil {
		return nil
	}
	etcd := dcs.Etcd

	var errs []error
	if etcd.TLSSecret != "" {
		if err := r.requireSecret(ctx, cr, etcd.TLSSecret,
			"EtcdTLSSecretNotFound", "EtcdTLSSecretInvalid",
			[]string{"ca.crt", "tls.crt", "tls.key"}); err != nil {
			errs = append(errs, err)
		}
	}
	if etcd.AuthSecret != "" {
		if err := r.requireSecret(ctx, cr, etcd.AuthSecret,
			"EtcdAuthSecretNotFound", "EtcdAuthSecretInvalid",
			[]string{"username", "password"}); err != nil {
			errs = append(errs, err)
		}
	}
	return multierror.Append(nil, errs...).ErrorOrNil()
}

// requireSecret fetches the named Secret and validates that each key in
// requiredKeys is present in Secret.Data. Emits a Warning event and returns
// an error if the Secret is missing or any required key is absent.
// notFoundReason is used when the Secret does not exist; invalidReason is used
// when the Secret exists but is missing a required key.
func (r *PGClusterReconciler) requireSecret(ctx context.Context, cr *v2.PerconaPGCluster, name, notFoundReason, invalidReason string, requiredKeys []string) error {
	secret := &corev1.Secret{}
	err := r.Client.Get(ctx, types.NamespacedName{Name: name, Namespace: cr.Namespace}, secret)
	if client.IgnoreNotFound(err) != nil {
		return errors.Wrapf(err, "get secret %q", name)
	}
	if err != nil {
		r.Recorder.Eventf(cr, corev1.EventTypeWarning, notFoundReason,
			"Secret %q not found in namespace %q", name, cr.Namespace)
		return errors.Errorf("secret %q not found", name)
	}
	for _, key := range requiredKeys {
		if _, ok := secret.Data[key]; !ok {
			r.Recorder.Eventf(cr, corev1.EventTypeWarning, invalidReason,
				"Secret %q is missing required key %q", name, key)
			return errors.Errorf("secret %q missing required key %q", name, key)
		}
	}
	return nil
}
