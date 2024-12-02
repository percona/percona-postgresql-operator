package extensions

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"github.com/percona/percona-postgresql-operator/internal/logging"
	"github.com/percona/percona-postgresql-operator/internal/postgres"
	"github.com/pkg/errors"
	"io"
	"log"
	"os"
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

func DisableCustomExtensionsInPostgreSQL(ctx context.Context, customExtensionsForDeletion []string, exec postgres.Executor) error {
	log := logging.FromContext(ctx)

	for _, extensionName := range customExtensionsForDeletion {

		sqlCommand := fmt.Sprintf(
			`SET client_min_messages = WARNING; DROP EXTENSION IF EXISTS %s;`,
			extensionName,
		)

		stdout, stderr, err := exec.ExecInAllDatabases(ctx,
			sqlCommand,
			map[string]string{
				"ON_ERROR_STOP": "on", // Abort when any one command fails.
				"QUIET":         "on", // Do not print successful commands to stdout.
			},
		)
		log.V(1).Info("disabled %s", extensionName, "stdout", stdout, "stderr", stderr)

		return errors.Wrap(err, "custom extension deletion")

	}
	return nil
}
