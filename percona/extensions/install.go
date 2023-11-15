package extensions

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"log"
	"os"
	"path"

	"github.com/pkg/errors"
)

func Install(key, archivePath, extensionPath string) error {
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

		switch hdr.Typeflag {
		case tar.TypeDir:
			log.Println("creating directory", hdr.Name)

			if err := os.Mkdir(hdr.Name, 0755); err != nil && !os.IsExist(err) {
				return errors.Wrap(err, "failed to create directory")
			}

		case tar.TypeReg:
			log.Println("creating file", hdr.Name)

			outFile, err := os.Create(hdr.Name)
			if err != nil {
				return errors.Wrap(err, "failed to create file")
			}

			for {
				_, err := io.CopyN(outFile, tReader, 1024)
				if err == io.EOF {
					break
				}
				if err != nil {
					return errors.Wrap(err, "failed to copy file")
				}
			}

			outFile.Close()

		default:
			return errors.Errorf("unknown type: %v in %s", hdr.Typeflag, hdr.Name)
		}
	}

	installedFile := path.Join(extensionPath, key+".installed")
	if _, err = os.Create(installedFile); err != nil {
		return errors.Wrapf(err, "failed to create %s", installedFile)
	}

	return nil
}
