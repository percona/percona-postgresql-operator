// Copyright 2021 - 2024 Crunchy Data Solutions, Inc.
//
// SPDX-License-Identifier: Apache-2.0

package naming

import (
	"context"
	"strings"
	"testing"
	"time"

	"gotest.tools/v3/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

func TestInstancePodDNSNames(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	instance := &appsv1.StatefulSet{}
	instance.Namespace = "some-place"
	instance.Name = "cluster-name-id"
	instance.Spec.ServiceName = "cluster-pods"

	names := InstancePodDNSNames(ctx, instance, "")
	assert.Assert(t, len(names) > 0)

	assert.DeepEqual(t, names[1:], []string{
		"cluster-name-id-0.cluster-pods.some-place.svc",
		"cluster-name-id-0.cluster-pods.some-place",
		"cluster-name-id-0.cluster-pods",
	})

	assert.Assert(t, len(names[0]) > len(names[1]), "expected FQDN first, got %q", names[0])
	assert.Assert(t, strings.HasPrefix(names[0], names[1]+"."), "wrong FQDN: %q", names[0])
	assert.Assert(t, !strings.HasSuffix(names[0], "."), "not expected root, got %q", names[0])

	names = InstancePodDNSNames(ctx, instance, "override.cluster.local")
	assert.Assert(t, len(names) > 0)

	assert.DeepEqual(t, names, []string{
		"cluster-name-id-0.cluster-pods.some-place.svc.override.cluster.local",
		"cluster-name-id-0.cluster-pods.some-place.svc",
		"cluster-name-id-0.cluster-pods.some-place",
		"cluster-name-id-0.cluster-pods",
	})

	assert.Assert(t, len(names[0]) > len(names[1]), "expected FQDN first, got %q", names[0])
	assert.Assert(t, strings.HasPrefix(names[0], names[1]+"."), "wrong FQDN: %q", names[0])
	assert.Assert(t, !strings.HasSuffix(names[0], "."), "not expected root, got %q", names[0])
}

func TestSafeDNSName(t *testing.T) {
	assert.Equal(t, "hello", SafeDNSName("hello"), "short name should be unchanged")
	assert.Equal(t, "hello", SafeDNSName("hello--"), "trailing hyphens should be stripped")

	long := strings.Repeat("a", 63)
	assert.Equal(t, long, SafeDNSName(long), "exactly 63 chars should pass through")

	long = strings.Repeat("a", 70)
	assert.Equal(t, strings.Repeat("a", 63), SafeDNSName(long), "names over 63 chars should be truncated")

	assert.Equal(t, strings.Repeat("a", 62),
		SafeDNSName(strings.Repeat("a", 62)+"--"),
		"truncation should remove any trailing hyphen")

	assert.Equal(t, "", SafeDNSName("---"), "all hyphens should become empty")
	assert.Equal(t, "", SafeDNSName(""), "empty string should stay empty")
}

func TestSafeDNSUniqueName(t *testing.T) {
	assert.Equal(t, "hello", SafeDNSUniqueName("hello"), "short name should be unchanged")
	assert.Equal(t, "hello", SafeDNSUniqueName("hello--"), "trailing hyphens should be stripped")

	long := strings.Repeat("a", 63)
	assert.Equal(t, long, SafeDNSUniqueName(long), "exactly 63 chars should pass through")

	long = strings.Repeat("a", 70)
	result := SafeDNSUniqueName(long)
	assert.Assert(t, len(result) <= 63, "expected <= 63 chars, got %d", len(result))
	assert.Assert(t, !strings.HasSuffix(result, "-"), "unexpected trailing hyphen: %q", result)

	assert.Equal(t, result, SafeDNSUniqueName(long), "same input should produce same output")

	a := SafeDNSUniqueName("something-long-enough-to-trigger-truncation-aaaaaaaa")
	b := SafeDNSUniqueName("something-long-enough-to-trigger-truncation-bbbbbbbb")
	assert.Assert(t, a != b, "different inputs should produce different results")

	assert.Equal(t, "", SafeDNSUniqueName(""), "empty string should stay empty")
}

func TestServiceDNSNames(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	service := &corev1.Service{}
	service.Namespace = "baltia"
	service.Name = "the-primary"

	names, err := ServiceDNSNames(ctx, service, "")
	assert.NilError(t, err)
	assert.Assert(t, len(names) > 0)

	assert.DeepEqual(t, names[1:], []string{
		"the-primary.baltia.svc",
		"the-primary.baltia",
		"the-primary",
	})

	assert.Assert(t, len(names[0]) > len(names[1]), "expected FQDN first, got %q", names[0])
	assert.Assert(t, strings.HasPrefix(names[0], names[1]+"."), "wrong FQDN: %q", names[0])
	assert.Assert(t, !strings.HasSuffix(names[0], "."), "not expected root, got %q", names[0])

	names, err = ServiceDNSNames(ctx, service, "override.cluster.local")
	assert.NilError(t, err)
	assert.Assert(t, len(names) > 0)

	assert.DeepEqual(t, names, []string{
		"the-primary.baltia.svc.override.cluster.local",
		"the-primary.baltia.svc",
		"the-primary.baltia",
		"the-primary",
	})

	assert.Assert(t, len(names[0]) > len(names[1]), "expected FQDN first, got %q", names[0])
	assert.Assert(t, strings.HasPrefix(names[0], names[1]+"."), "wrong FQDN: %q", names[0])
	assert.Assert(t, !strings.HasSuffix(names[0], "."), "not expected root, got %q", names[0])
}
