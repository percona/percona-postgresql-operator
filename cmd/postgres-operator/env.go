package main

import (
	stderrors "errors"
	"os"
	"reflect"
	"strconv"
	"time"

	"github.com/pkg/errors"
)

type EnvConfig struct {
	LeaderElection          bool   `default:"true" env:"PGO_CONTROLLER_LEADER_ELECTION_ENABLED"`
	LeaderElectionID        string `env:"PGO_CONTROLLER_LEASE_NAME"`
	LeaderElectionNamespace string `env:"PGO_NAMESPACE"`

	LeaseDuration time.Duration `default:"60" env:"PGO_CONTROLLER_LEASE_DURATION"`
	RenewDeadline time.Duration `default:"40" env:"PGO_CONTROLLER_RENEW_DEADLINE"`
	RetryPeriod   time.Duration `default:"10" env:"PGO_CONTROLLER_RETRY_PERIOD"`

	SingleNamespace string `env:"PGO_TARGET_NAMESPACE"`
	MultiNamespaces string `env:"PGO_TARGET_NAMESPACES"`

	PprofBindAddress string `env:"PPROF_BIND_ADDRESS"`

	Workers int `env:"PGO_WORKERS"`
}

func parseEnvVars() (EnvConfig, error) {
	var cfg EnvConfig
	var errs []error

	v := reflect.ValueOf(&cfg).Elem()
	t := v.Type()

	for i := range v.NumField() {
		sf, fv := t.Field(i), v.Field(i)

		key := sf.Tag.Get("env")
		raw := os.Getenv(key)
		if raw == "" {
			raw = sf.Tag.Get("default")
		}
		if raw == "" {
			continue
		}

		switch fv.Kind() {
		case reflect.Bool:
			b, err := strconv.ParseBool(raw)
			if err != nil {
				errs = append(errs, errors.Wrapf(err, "invalid %s env var value", key))
				continue
			}
			fv.SetBool(b)
		case reflect.String:
			fv.SetString(raw)
		case reflect.Int, reflect.Int64:
			n, err := strconv.Atoi(raw)
			if err != nil {
				errs = append(errs, errors.Wrapf(err, "invalid %s env var value", key))
				continue
			}
			if fv.Type() == reflect.TypeFor[time.Duration]() {
				if n < 0 {
					errs = append(errs, errors.Errorf("invalid %s env var value: must be >= 0", key))
					continue
				}
				fv.SetInt(int64(time.Second * time.Duration(n)))
				continue
			}
			fv.SetInt(int64(n))
		default:
			return EnvConfig{}, errors.Errorf("unsupported field type %s for %s", fv.Type(), sf.Name)
		}
	}

	return cfg, stderrors.Join(errs...)
}
