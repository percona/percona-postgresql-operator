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
	var storageType, endpoint, region, bucket, key, extensionPath, uriStyle string
	var install, uninstall, verifyTLS bool

	flag.StringVar(&storageType, "type", "", "Storage type")
	flag.StringVar(&endpoint, "endpoint", "", "Storage endpoint")
	flag.StringVar(&region, "region", "", "Storage region")
	flag.StringVar(&bucket, "bucket", "", "Storage bucket")
	flag.StringVar(&extensionPath, "extension-path", "", "Extension installation path")
	flag.StringVar(&uriStyle, "uri-style", "url", "URI style, either path or url")

	flag.StringVar(&key, "key", "", "Extension archive key")

	flag.BoolVar(&install, "install", false, "Install extension")
	flag.BoolVar(&uninstall, "uninstall", false, "Uninstall extension")
	flag.BoolVar(&verifyTLS, "verify-tls", true, "Verify TLS")
	flag.Parse()

	if (install && uninstall) || (!install && !uninstall) {
		log.Fatalf("ERROR: set either -install or -uninstall")
	}

	log.Printf("starting extension installer for %s/%s (%s) in %s", bucket, key, storageType, region, uriStyle, verifyTLS)

	storage := initStorage(extensions.StorageType(storageType), endpoint, bucket, region, uriStyle, verifyTLS)

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

func initStorage(storageType extensions.StorageType, endpoint, bucket, region string, uriStyle string, verifyTLS bool) extensions.ObjectGetter {
	switch storageType {
	case extensions.StorageTypeS3:
		return extensions.NewS3(endpoint, region, bucket, uriStyle, verifyTLS)
	default:
		log.Fatalf("unknown storage type: %s", os.Getenv("STORAGE_TYPE"))
	}

	return nil
}
