package snapshots

import (
	"testing"
	"time"

	volumesnapshotv1 "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

func TestShouldFailSnapshot(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name           string
		volumeSnapshot *volumesnapshotv1.VolumeSnapshot
		wantFail       bool
	}{
		{
			name: "Status.Error.Time is zero",
			volumeSnapshot: &volumesnapshotv1.VolumeSnapshot{
				Status: &volumesnapshotv1.VolumeSnapshotStatus{
					Error: &volumesnapshotv1.VolumeSnapshotError{
						Time: ptr.To(metav1.Time{}),
					},
				},
			},
			wantFail: false,
		},
		{
			name: "error within deadline",
			volumeSnapshot: &volumesnapshotv1.VolumeSnapshot{
				Status: &volumesnapshotv1.VolumeSnapshotStatus{
					Error: &volumesnapshotv1.VolumeSnapshotError{
						Time: ptr.To(metav1.NewTime(now.Add(-1 * time.Minute))), // 1mins ago, within deadline
					},
				},
			},
			wantFail: false,
		},
		{
			name: "error past deadline",
			volumeSnapshot: &volumesnapshotv1.VolumeSnapshot{
				Status: &volumesnapshotv1.VolumeSnapshotStatus{
					Error: &volumesnapshotv1.VolumeSnapshotError{
						Time: ptr.To(metav1.NewTime(now.Add(-10 * time.Minute))), // 10 minutes ago (past 5min deadline)
					},
				},
			},
			wantFail: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldFailSnapshot(tt.volumeSnapshot); got != tt.wantFail {
				t.Errorf("shouldFailSnapshot() = %v, want %v", got, tt.wantFail)
			}
		})
	}
}
