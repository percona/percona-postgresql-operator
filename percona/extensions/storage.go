package extensions

import (
	"io"
)

type ObjectGetter interface {
	// Get returns the object stored at the given path.
	Get(key string) (io.ReadCloser, error)
}

type StorageType string

const (
	StorageTypeS3    StorageType = "s3"
	StorageTypeGCS   StorageType = "gcs"
	StorageTypeAzure StorageType = "azure"
)
