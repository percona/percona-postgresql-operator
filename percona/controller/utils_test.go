package controller

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/percona/percona-postgresql-operator/v2/internal/naming"
)

func TestGetReadyInstancePod(t *testing.T) {
	const (
		clusterName = "test-cluster"
		ns          = "test-ns"
	)

	instanceLabels := map[string]string{
		naming.LabelCluster:  clusterName,
		naming.LabelInstance: "instance1",
	}

	tests := []struct {
		name    string
		pods    []corev1.Pod
		wantPod string
		wantErr string
	}{
		{
			name:    "no pods",
			pods:    nil,
			wantErr: "no running instance found",
		},
		{
			name: "pod not running",
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod-pending",
						Namespace: ns,
						Labels:    instanceLabels,
					},
					Status: corev1.PodStatus{
						Phase: corev1.PodPending,
					},
				},
			},
			wantErr: "no running instance found",
		},
		{
			name: "pod running but not ready",
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod-not-ready",
						Namespace: ns,
						Labels:    instanceLabels,
					},
					Status: corev1.PodStatus{
						Phase: corev1.PodRunning,
						Conditions: []corev1.PodCondition{
							{
								Type:   corev1.PodReady,
								Status: corev1.ConditionFalse,
							},
						},
					},
				},
			},
			wantErr: "no running instance found",
		},
		{
			name: "pod running and ready",
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod-ready",
						Namespace: ns,
						Labels:    instanceLabels,
					},
					Status: corev1.PodStatus{
						Phase: corev1.PodRunning,
						Conditions: []corev1.PodCondition{
							{
								Type:   corev1.PodReady,
								Status: corev1.ConditionTrue,
							},
						},
					},
				},
			},
			wantPod: "pod-ready",
		},
		{
			name: "skips not-ready pod and returns ready pod",
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod-not-ready",
						Namespace: ns,
						Labels:    instanceLabels,
					},
					Status: corev1.PodStatus{
						Phase: corev1.PodRunning,
						Conditions: []corev1.PodCondition{
							{
								Type:   corev1.PodReady,
								Status: corev1.ConditionFalse,
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod-ready",
						Namespace: ns,
						Labels:    instanceLabels,
					},
					Status: corev1.PodStatus{
						Phase: corev1.PodRunning,
						Conditions: []corev1.PodCondition{
							{
								Type:   corev1.PodReady,
								Status: corev1.ConditionTrue,
							},
						},
					},
				},
			},
			wantPod: "pod-ready",
		},
		{
			name: "pod running without ready condition",
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod-no-condition",
						Namespace: ns,
						Labels:    instanceLabels,
					},
					Status: corev1.PodStatus{
						Phase:      corev1.PodRunning,
						Conditions: []corev1.PodCondition{},
					},
				},
			},
			wantErr: "no running instance found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()

			builder := fake.NewClientBuilder()
			for i := range tt.pods {
				builder = builder.WithObjects(&tt.pods[i])
			}
			cl := builder.Build()

			pod, err := GetReadyInstancePod(ctx, cl, clusterName, ns)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				assert.Nil(t, pod)
			} else {
				require.NoError(t, err)
				require.NotNil(t, pod)
				assert.Equal(t, tt.wantPod, pod.Name)
			}
		})
	}
}
