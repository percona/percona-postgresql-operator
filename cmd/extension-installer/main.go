package main

import (
	"flag"
	"io"
	"log"
	"os"
	"path"

	"github.com/percona/percona-postgresql-operator/percona/extensions"
)

func main() {
	var storageType, region, bucket, key, extensionPath string
	var install, uninstall bool

	flag.StringVar(&storageType, "type", "", "Storage type")
	flag.StringVar(&region, "region", "", "Storage region")
	flag.StringVar(&bucket, "bucket", "", "Storage bucket")
	flag.StringVar(&extensionPath, "extension-path", "", "Extension installation path")
	flag.StringVar(&key, "key", "", "Extension archive key")

	flag.BoolVar(&install, "install", false, "Install extension")
	flag.BoolVar(&uninstall, "uninstall", false, "Uninstall extension")
	flag.Parse()

	if (install && uninstall) || (!install && !uninstall) {
		log.Fatalf("ERROR: set either -install or -uninstall")
	}

	log.Printf("starting extension installer for %s/%s (%s) in %s", bucket, key, storageType, region)

	storage := initStorage(extensions.StorageType(storageType), bucket, region)

	packageName := key + ".tar.gz"

	object, err := storage.Get(packageName)
	if err != nil {
		log.Fatalf("ERROR: failed to get object: %v", err)
	}

	archivePath := path.Join("/tmp", packageName)

	// TODO: /tmp is not a good place due to its size limitation.
	dst, err := os.Create(archivePath)
	if err != nil {
		log.Fatalf("ERROR: failed to create file: %v", err)
	}
	defer dst.Close()

	written, err := io.Copy(dst, object)
	if err != nil {
		log.Fatalf("ERROR: failed to copy object: %v", err)
	}

	log.Printf("copied %d bytes to %s", written, dst.Name())

	switch {
	case install:
		log.Printf("installing extension %s", archivePath)
		if err := extensions.Install(key, archivePath, extensionPath); err != nil {
			log.Fatalf("ERROR: failed to install extension: %v", err)
		}
	case uninstall:
		log.Printf("uninstalling extension %s", archivePath)
		if err := extensions.Uninstall(archivePath); err != nil {
			log.Fatalf("ERROR: failed to uninstall extension: %v", err)
		}
	}
}

func initStorage(storageType extensions.StorageType, bucket, region string) extensions.ObjectGetter {
	switch storageType {
	case extensions.StorageTypeS3:
		return extensions.NewS3(region, bucket)
	default:
		log.Fatalf("unknown storage type: %s", os.Getenv("STORAGE_TYPE"))
	}

	return nil
}
