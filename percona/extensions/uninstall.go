package extensions

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"log"
	"os"

	"github.com/pkg/errors"
)

func Uninstall(archivePath string) error {
	extensionArchive, err := os.Open(archivePath)
	if err != nil {
		return errors.Wrap(err, "failed to open file")
	}

	gReader, err := gzip.NewReader(extensionArchive)
	if err != nil {
		return errors.Wrap(err, "failed to create gzip reader")
	}
	defer gReader.Close()

	tReader := tar.NewReader(gReader)
	for {
		hdr, err := tReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return errors.Wrap(err, "failed to read tar header")
		}

		if hdr.Typeflag == tar.TypeReg {
			log.Printf("removing %s", hdr.Name)
			if err := os.Remove(hdr.Name); err != nil {
				return errors.Wrapf(err, "failed to remove %s", hdr.Name)
			}
		}
	}

	return nil
}
