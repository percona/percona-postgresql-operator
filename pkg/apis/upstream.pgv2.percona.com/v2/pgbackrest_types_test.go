package v1beta1

import (
	"testing"

	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/utils/ptr"
)

func TestPGBackRestRepo_StorageEquals(t *testing.T) {
	testCases := []struct {
		name     string
		repoA    *PGBackRestRepo
		repoB    *PGBackRestRepo
		expected bool
	}{
		// S3 repo tests
		{
			name: "S3 repos with same values",
			repoA: &PGBackRestRepo{
				Name: "repo1",
				S3: &RepoS3{
					Bucket:   "my-bucket",
					Endpoint: "s3.amazonaws.com",
					Region:   "us-east-1",
				},
			},
			repoB: &PGBackRestRepo{
				Name: "repo1",
				S3: &RepoS3{
					Bucket:   "my-bucket",
					Endpoint: "s3.amazonaws.com",
					Region:   "us-east-1",
				},
			},
			expected: true,
		},
		{
			name: "S3 repos with different bucket",
			repoA: &PGBackRestRepo{
				Name: "repo1",
				S3: &RepoS3{
					Bucket:   "my-bucket",
					Endpoint: "s3.amazonaws.com",
					Region:   "us-east-1",
				},
			},
			repoB: &PGBackRestRepo{
				Name: "repo1",
				S3: &RepoS3{
					Bucket:   "other-bucket",
					Endpoint: "s3.amazonaws.com",
					Region:   "us-east-1",
				},
			},
			expected: false,
		},
		{
			name: "S3 repos with different endpoint",
			repoA: &PGBackRestRepo{
				Name: "repo1",
				S3: &RepoS3{
					Bucket:   "my-bucket",
					Endpoint: "s3.amazonaws.com",
					Region:   "us-east-1",
				},
			},
			repoB: &PGBackRestRepo{
				Name: "repo1",
				S3: &RepoS3{
					Bucket:   "my-bucket",
					Endpoint: "s3.us-west-2.amazonaws.com",
					Region:   "us-east-1",
				},
			},
			expected: false,
		},
		{
			name: "S3 repos with different region",
			repoA: &PGBackRestRepo{
				Name: "repo1",
				S3: &RepoS3{
					Bucket:   "my-bucket",
					Endpoint: "s3.amazonaws.com",
					Region:   "us-east-1",
				},
			},
			repoB: &PGBackRestRepo{
				Name: "repo1",
				S3: &RepoS3{
					Bucket:   "my-bucket",
					Endpoint: "s3.amazonaws.com",
					Region:   "us-west-2",
				},
			},
			expected: false,
		},
		// GCS repo tests
		{
			name: "GCS repos with same values",
			repoA: &PGBackRestRepo{
				Name: "repo1",
				GCS: &RepoGCS{
					Bucket: "my-gcs-bucket",
				},
			},
			repoB: &PGBackRestRepo{
				Name: "repo1",
				GCS: &RepoGCS{
					Bucket: "my-gcs-bucket",
				},
			},
			expected: true,
		},
		{
			name: "GCS repos with different bucket",
			repoA: &PGBackRestRepo{
				Name: "repo1",
				GCS: &RepoGCS{
					Bucket: "my-gcs-bucket",
				},
			},
			repoB: &PGBackRestRepo{
				Name: "repo1",
				GCS: &RepoGCS{
					Bucket: "other-gcs-bucket",
				},
			},
			expected: false,
		},
		// Azure repo tests
		{
			name: "Azure repos with same values",
			repoA: &PGBackRestRepo{
				Name: "repo1",
				Azure: &RepoAzure{
					Container: "my-container",
				},
			},
			repoB: &PGBackRestRepo{
				Name: "repo1",
				Azure: &RepoAzure{
					Container: "my-container",
				},
			},
			expected: true,
		},
		{
			name: "Azure repos with different container",
			repoA: &PGBackRestRepo{
				Name: "repo1",
				Azure: &RepoAzure{
					Container: "my-container",
				},
			},
			repoB: &PGBackRestRepo{
				Name: "repo1",
				Azure: &RepoAzure{
					Container: "other-container",
				},
			},
			expected: false,
		},
		// Volume repo tests
		{
			name: "Volume repos with same values",
			repoA: &PGBackRestRepo{
				Name: "repo1",
				Volume: &RepoPVC{
					VolumeClaimSpec: corev1.PersistentVolumeClaimSpec{
						AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
						Resources: corev1.VolumeResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceStorage: resource.MustParse("1Gi"),
							},
						},
					},
				},
			},
			repoB: &PGBackRestRepo{
				Name: "repo1",
				Volume: &RepoPVC{
					VolumeClaimSpec: corev1.PersistentVolumeClaimSpec{
						AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
						Resources: corev1.VolumeResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceStorage: resource.MustParse("1Gi"),
							},
						},
					},
				},
			},
			expected: true,
		},
		{
			name: "Volume repos with different storage size",
			repoA: &PGBackRestRepo{
				Name: "repo1",
				Volume: &RepoPVC{
					VolumeClaimSpec: corev1.PersistentVolumeClaimSpec{
						AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
						Resources: corev1.VolumeResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceStorage: resource.MustParse("1Gi"),
							},
						},
					},
				},
			},
			repoB: &PGBackRestRepo{
				Name: "repo1",
				Volume: &RepoPVC{
					VolumeClaimSpec: corev1.PersistentVolumeClaimSpec{
						AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
						Resources: corev1.VolumeResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceStorage: resource.MustParse("2Gi"),
							},
						},
					},
				},
			},
			expected: false,
		},
		{
			name: "Volume repos with different access modes",
			repoA: &PGBackRestRepo{
				Name: "repo1",
				Volume: &RepoPVC{
					VolumeClaimSpec: corev1.PersistentVolumeClaimSpec{
						AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
						Resources: corev1.VolumeResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceStorage: resource.MustParse("1Gi"),
							},
						},
					},
				},
			},
			repoB: &PGBackRestRepo{
				Name: "repo1",
				Volume: &RepoPVC{
					VolumeClaimSpec: corev1.PersistentVolumeClaimSpec{
						AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteMany},
						Resources: corev1.VolumeResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceStorage: resource.MustParse("1Gi"),
							},
						},
					},
				},
			},
			expected: false,
		},
		{
			name: "Volume repos with different storage class",
			repoA: &PGBackRestRepo{
				Name: "repo1",
				Volume: &RepoPVC{
					VolumeClaimSpec: corev1.PersistentVolumeClaimSpec{
						AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
						StorageClassName: ptr.To("standard"),
						Resources: corev1.VolumeResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceStorage: resource.MustParse("1Gi"),
							},
						},
					},
				},
			},
			repoB: &PGBackRestRepo{
				Name: "repo1",
				Volume: &RepoPVC{
					VolumeClaimSpec: corev1.PersistentVolumeClaimSpec{
						AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
						StorageClassName: ptr.To("premium"),
						Resources: corev1.VolumeResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceStorage: resource.MustParse("1Gi"),
							},
						},
					},
				},
			},
			expected: false,
		},
		// Different repo types
		{
			name: "S3 vs GCS",
			repoA: &PGBackRestRepo{
				Name: "repo1",
				S3: &RepoS3{
					Bucket:   "my-bucket",
					Endpoint: "s3.amazonaws.com",
					Region:   "us-east-1",
				},
			},
			repoB: &PGBackRestRepo{
				Name: "repo1",
				GCS: &RepoGCS{
					Bucket: "my-bucket",
				},
			},
			expected: false,
		},
		{
			name: "S3 vs Azure",
			repoA: &PGBackRestRepo{
				Name: "repo1",
				S3: &RepoS3{
					Bucket:   "my-bucket",
					Endpoint: "s3.amazonaws.com",
					Region:   "us-east-1",
				},
			},
			repoB: &PGBackRestRepo{
				Name: "repo1",
				Azure: &RepoAzure{
					Container: "my-container",
				},
			},
			expected: false,
		},
		{
			name: "S3 vs Volume",
			repoA: &PGBackRestRepo{
				Name: "repo1",
				S3: &RepoS3{
					Bucket:   "my-bucket",
					Endpoint: "s3.amazonaws.com",
					Region:   "us-east-1",
				},
			},
			repoB: &PGBackRestRepo{
				Name: "repo1",
				Volume: &RepoPVC{
					VolumeClaimSpec: corev1.PersistentVolumeClaimSpec{
						AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
						Resources: corev1.VolumeResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceStorage: resource.MustParse("1Gi"),
							},
						},
					},
				},
			},
			expected: false,
		},
		{
			name: "GCS vs Azure",
			repoA: &PGBackRestRepo{
				Name: "repo1",
				GCS: &RepoGCS{
					Bucket: "my-bucket",
				},
			},
			repoB: &PGBackRestRepo{
				Name: "repo1",
				Azure: &RepoAzure{
					Container: "my-container",
				},
			},
			expected: false,
		},
		{
			name: "GCS vs Volume",
			repoA: &PGBackRestRepo{
				Name: "repo1",
				GCS: &RepoGCS{
					Bucket: "my-bucket",
				},
			},
			repoB: &PGBackRestRepo{
				Name: "repo1",
				Volume: &RepoPVC{
					VolumeClaimSpec: corev1.PersistentVolumeClaimSpec{
						AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
						Resources: corev1.VolumeResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceStorage: resource.MustParse("1Gi"),
							},
						},
					},
				},
			},
			expected: false,
		},
		{
			name: "Azure vs Volume",
			repoA: &PGBackRestRepo{
				Name: "repo1",
				Azure: &RepoAzure{
					Container: "my-container",
				},
			},
			repoB: &PGBackRestRepo{
				Name: "repo1",
				Volume: &RepoPVC{
					VolumeClaimSpec: corev1.PersistentVolumeClaimSpec{
						AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
						Resources: corev1.VolumeResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceStorage: resource.MustParse("1Gi"),
							},
						},
					},
				},
			},
			expected: false,
		},
		// Nil cases
		{
			name: "Both repos have nil storage",
			repoA: &PGBackRestRepo{
				Name: "repo1",
			},
			repoB: &PGBackRestRepo{
				Name: "repo1",
			},
			expected: false,
		},
		{
			name: "RepoA has S3, RepoB has nil",
			repoA: &PGBackRestRepo{
				Name: "repo1",
				S3: &RepoS3{
					Bucket:   "my-bucket",
					Endpoint: "s3.amazonaws.com",
					Region:   "us-east-1",
				},
			},
			repoB: &PGBackRestRepo{
				Name: "repo1",
			},
			expected: false,
		},
		{
			name: "RepoA has nil, RepoB has S3",
			repoA: &PGBackRestRepo{
				Name: "repo1",
			},
			repoB: &PGBackRestRepo{
				Name: "repo1",
				S3: &RepoS3{
					Bucket:   "my-bucket",
					Endpoint: "s3.amazonaws.com",
					Region:   "us-east-1",
				},
			},
			expected: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actual := tc.repoA.StorageEquals(tc.repoB)
			assert.Equal(t, tc.expected, actual)
		})
	}
}
