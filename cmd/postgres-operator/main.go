// Copyright 2017 - 2024 Crunchy Data Solutions, Inc.
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"
	"os"
	goruntime "runtime"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/pkg/errors"
	"go.opentelemetry.io/otel"
	uzap "go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	cruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/percona/percona-postgresql-operator/v2/internal/controller/pgupgrade"
	"github.com/percona/percona-postgresql-operator/v2/internal/controller/postgrescluster"
	"github.com/percona/percona-postgresql-operator/v2/internal/controller/runtime"
	"github.com/percona/percona-postgresql-operator/v2/internal/controller/standalone_pgadmin"
	"github.com/percona/percona-postgresql-operator/v2/internal/feature"
	"github.com/percona/percona-postgresql-operator/v2/internal/initialize"
	"github.com/percona/percona-postgresql-operator/v2/internal/logging"
	"github.com/percona/percona-postgresql-operator/v2/internal/naming"
	"github.com/percona/percona-postgresql-operator/v2/internal/upgradecheck"
	perconaController "github.com/percona/percona-postgresql-operator/v2/percona/controller"
	"github.com/percona/percona-postgresql-operator/v2/percona/controller/pgbackup"
	"github.com/percona/percona-postgresql-operator/v2/percona/controller/pgcluster"
	"github.com/percona/percona-postgresql-operator/v2/percona/controller/pgrestore"
	perconaPGUpgrade "github.com/percona/percona-postgresql-operator/v2/percona/controller/pgupgrade"
	perconaRuntime "github.com/percona/percona-postgresql-operator/v2/percona/runtime"
	"github.com/percona/percona-postgresql-operator/v2/percona/utils/registry"
	v2 "github.com/percona/percona-postgresql-operator/v2/pkg/apis/pgv2.percona.com/v2"
	"github.com/percona/percona-postgresql-operator/v2/pkg/apis/postgres-operator.crunchydata.com/v1beta1"
)

var (
	GitCommit     string
	GitBranch     string
	BuildTime     string
	versionString string
)

// assertNoError panics when err is not nil.
func assertNoError(err error) {
	if err != nil {
		panic(err)
	}
}

func main() {
	otelFlush, err := initOpenTelemetry()
	assertNoError(err)
	defer otelFlush()

	opts := zap.Options{
		Encoder: getLogEncoder(),
		Level:   getLogLevel(),
	}
	l := zap.New(zap.UseFlagOptions(&opts))
	logging.SetLogSink(l.GetSink())
	cruntime.SetLogger(l)

	// create a context that will be used to stop all controllers on a SIGTERM or SIGINT
	ctx := cruntime.SetupSignalHandler()
	log := logging.FromContext(ctx)
	log.V(1).Info("debug flag set to true")

	log.Info("Manager starting up", "gitCommit", GitCommit, "gitBranch", GitBranch,
		"buildTime", BuildTime, "goVersion", goruntime.Version(), "os", goruntime.GOOS, "arch", goruntime.GOARCH)

	features := feature.NewGate()
	err = features.SetFromMap(map[string]bool{
		feature.InstanceSidecars:           true, // needed for PMM
		feature.PGBouncerSidecars:          true, // K8SPG-645
		feature.PGBackrestRepoHostSidecars: true, // K8SPG-832
		feature.TablespaceVolumes:          true,
	})
	assertNoError(err)

	assertNoError(features.Set(os.Getenv("PGO_FEATURE_GATES")))
	ctx = feature.NewContext(ctx, features)
	log.Info("feature gates",
		// These are set by the user
		"PGO_FEATURE_GATES", feature.ShowAssigned(ctx),
		// These are enabled, including features that are on by default
		"enabled", feature.ShowEnabled(ctx))

	cruntime.SetLogger(log)

	cfg, err := runtime.GetConfig()
	assertNoError(err)

	cfg.Wrap(otelTransportWrapper())

	// Configure client-go to suppress warnings when warning headers are encountered. This prevents
	// warnings from being logged over and over again during reconciliation (e.g. this will suppress
	// deprecation warnings when using an older version of a resource for backwards compatibility).
	rest.SetDefaultWarningHandler(rest.NoWarnings{})

	options, err := initManager(ctx)
	assertNoError(err)

	mgr, err := perconaRuntime.CreateRuntimeManager(
		cfg,
		features,
		options,
	)
	assertNoError(err)

	// Add Percona custom resource types to scheme
	assertNoError(v2.AddToScheme(mgr.GetScheme()))

	// add all PostgreSQL Operator controllers to the runtime manager
	err = addControllersToManager(ctx, mgr)
	assertNoError(err)

	log.Info("starting controller runtime manager and will wait for signal to exit")

	// Disable Crunchy upgrade checking
	upgradeCheckingDisabled := true
	if !upgradeCheckingDisabled {
		log.Info("upgrade checking enabled")
		// get the URL for the check for upgrades endpoint if set in the env
		assertNoError(upgradecheck.ManagedScheduler(mgr,
			isOpenshift(ctx, mgr.GetConfig()), os.Getenv("CHECK_FOR_UPGRADES_URL"), versionString, nil))
	}

	assertNoError(mgr.Start(ctx))
	log.Info("signal received, exiting")
}

// addControllersToManager adds all PostgreSQL Operator controllers to the provided controller
// runtime manager.
func addControllersToManager(ctx context.Context, mgr manager.Manager) error {
	os.Setenv("REGISTRATION_REQUIRED", "false")

	r := &postgrescluster.Reconciler{
		Client:      mgr.GetClient(),
		Owner:       postgrescluster.ControllerName,
		Recorder:    mgr.GetEventRecorderFor(postgrescluster.ControllerName),
		Tracer:      otel.Tracer(postgrescluster.ControllerName),
		IsOpenShift: isOpenshift(ctx, mgr.GetConfig()),
	}
	cm := &perconaController.CustomManager{Manager: mgr}
	if err := r.SetupWithManager(cm); err != nil {
		return err
	}
	if cm.Controller() == nil {
		return errors.New("missing controller in manager")
	}

	if err := mgr.GetFieldIndexer().IndexField(
		context.Background(),
		&v2.PerconaPGCluster{},
		v2.IndexFieldEnvFromSecrets,
		v2.EnvFromSecretsIndexerFunc,
	); err != nil {
		return err
	}

	externalEvents := make(chan event.GenericEvent)
	stopChan := make(chan event.DeleteEvent)

	pc := &pgcluster.PGClusterReconciler{
		Client:               mgr.GetClient(),
		Owner:                pgcluster.PGClusterControllerName,
		Recorder:             mgr.GetEventRecorderFor(pgcluster.PGClusterControllerName),
		Tracer:               otel.Tracer(pgcluster.PGClusterControllerName),
		Platform:             detectPlatform(ctx, mgr.GetConfig()),
		KubeVersion:          getServerVersion(ctx, mgr.GetConfig()),
		CrunchyController:    cm.Controller(),
		IsOpenShift:          isOpenshift(ctx, mgr.GetConfig()),
		Cron:                 pgcluster.NewCronRegistry(),
		ExternalChan:         externalEvents,
		StopExternalWatchers: stopChan,
		Watchers:             registry.New(),
	}
	if err := pc.SetupWithManager(mgr); err != nil {
		return err
	}

	pb := &pgbackup.PGBackupReconciler{
		Client:       mgr.GetClient(),
		ExternalChan: externalEvents,
	}
	if err := pb.SetupWithManager(mgr); err != nil {
		return err
	}

	if err := mgr.GetFieldIndexer().IndexField(
		context.Background(),
		&v2.PerconaPGBackup{},
		v2.IndexFieldPGCluster,
		v2.PGClusterIndexerFunc,
	); err != nil {
		return err
	}

	if err := mgr.GetFieldIndexer().IndexField(
		context.Background(),
		&v2.PerconaPGBackup{},
		"status.state",
		func(rawObj client.Object) []string {
			backup := rawObj.(*v2.PerconaPGBackup)
			return []string{string(backup.Status.State)}
		},
	); err != nil {
		return err
	}

	pr := &pgrestore.PGRestoreReconciler{
		Client:   mgr.GetClient(),
		Owner:    pgrestore.PGRestoreControllerName,
		Recorder: mgr.GetEventRecorderFor(pgrestore.PGRestoreControllerName),
		Tracer:   otel.Tracer(pgrestore.PGRestoreControllerName),
	}
	if err := pr.SetupWithManager(mgr); err != nil {
		return err
	}

	upgradeReconciler := &pgupgrade.PGUpgradeReconciler{
		Client: mgr.GetClient(),
		Owner:  "pgupgrade-controller",
	}

	if err := upgradeReconciler.SetupWithManager(mgr); err != nil {
		return errors.Wrap(err, "unable to create PGUpgrade controller")
	}

	pu := &perconaPGUpgrade.PGUpgradeReconciler{
		Client: mgr.GetClient(),
	}
	if err := pu.SetupWithManager(mgr); err != nil {
		return err
	}

	pgAdminReconciler := &standalone_pgadmin.PGAdminReconciler{
		Client:      mgr.GetClient(),
		Owner:       "pgadmin-controller",
		Recorder:    mgr.GetEventRecorderFor(naming.ControllerPGAdmin),
		IsOpenShift: isOpenshift(ctx, mgr.GetConfig()),
	}

	if err := pgAdminReconciler.SetupWithManager(mgr); err != nil {
		return errors.Wrap(err, "unable to create PGAdmin controller")
	}

	return nil
}

//+kubebuilder:rbac:groups="coordination.k8s.io",resources="leases",verbs={get,create,update}

func initManager(ctx context.Context) (runtime.Options, error) {
	log := logging.FromContext(ctx)

	options := runtime.Options{}
	options.Cache.SyncPeriod = initialize.Pointer(time.Hour)

	options.HealthProbeBindAddress = ":8081"

	// Enable leader elections when configured with a valid Lease.coordination.k8s.io name.
	// - https://docs.k8s.io/concepts/architecture/leases
	// - https://releases.k8s.io/v1.30.0/pkg/apis/coordination/validation/validation.go#L26
	if lease := os.Getenv("PGO_CONTROLLER_LEASE_NAME"); len(lease) > 0 {
		if errs := validation.IsDNS1123Subdomain(lease); len(errs) > 0 {
			return options, fmt.Errorf("value for PGO_CONTROLLER_LEASE_NAME is invalid: %v", errs)
		}

		options.LeaderElection = true
		options.LeaderElectionID = lease
		options.LeaderElectionNamespace = os.Getenv("PGO_NAMESPACE")
	} else {
		// K8SPG-761
		options.LeaderElection = true
		options.LeaderElectionID = perconaRuntime.ElectionID
	}

	// Check PGO_TARGET_NAMESPACE for backwards compatibility with
	// "singlenamespace" installations
	singlenamespace := strings.TrimSpace(os.Getenv("PGO_TARGET_NAMESPACE"))

	// Check PGO_TARGET_NAMESPACES for non-cluster-wide, multi-namespace
	// installations
	multinamespace := strings.TrimSpace(os.Getenv("PGO_TARGET_NAMESPACES"))

	// Initialize DefaultNamespaces if any target namespaces are set
	if len(singlenamespace) > 0 || len(multinamespace) > 0 {
		options.Cache.DefaultNamespaces = map[string]runtime.CacheConfig{}
	}

	if len(singlenamespace) > 0 {
		options.Cache.DefaultNamespaces[singlenamespace] = runtime.CacheConfig{}
	}

	if len(multinamespace) > 0 {
		for _, namespace := range strings.FieldsFunc(multinamespace, func(c rune) bool {
			return c != '-' && !unicode.IsLetter(c) && !unicode.IsNumber(c)
		}) {
			options.Cache.DefaultNamespaces[namespace] = runtime.CacheConfig{}
		}
	}

	options.Controller.GroupKindConcurrency = map[string]int{
		"PostgresCluster." + v1beta1.GroupVersion.Group: 1,
		"PGUpgrade." + v1beta1.GroupVersion.Group:       1,
		"PGAdmin." + v1beta1.GroupVersion.Group:         1,
		"PerconaPGCluster." + v2.GroupVersion.Group:     1,
		"PerconaPGUpgrade." + v2.GroupVersion.Group:     1,
		"PerconaPGBackup." + v2.GroupVersion.Group:      1,
		"PerconaPGRestore." + v2.GroupVersion.Group:     1,
	}

	if s := os.Getenv("PGO_WORKERS"); s != "" {
		if i, err := strconv.Atoi(s); err == nil && i > 0 {
			for kind := range options.Controller.GroupKindConcurrency {
				options.Controller.GroupKindConcurrency[kind] = i
			}
		} else {
			log.Error(err, "PGO_WORKERS must be a positive number")
		}
	}

	return options, nil
}

func isGKE(ctx context.Context, cfg *rest.Config) bool {
	log := logging.FromContext(ctx)

	const groupName, kind = "cloud.google.com", "BackendConfig"

	client, err := discovery.NewDiscoveryClientForConfig(cfg)
	assertNoError(err)

	groups, err := client.ServerGroups()
	if err != nil {
		assertNoError(err)
	}
	for _, g := range groups.Groups {
		if g.Name != groupName {
			continue
		}
		for _, v := range g.Versions {
			resourceList, err := client.ServerResourcesForGroupVersion(v.GroupVersion)
			if err != nil {
				assertNoError(err)
			}
			for _, r := range resourceList.APIResources {
				if r.Kind == kind {
					log.Info("detected GKE environment")
					return true
				}
			}
		}
	}

	return false
}

func isEKS(ctx context.Context, cfg *rest.Config) bool {
	log := logging.FromContext(ctx)

	const groupName, kind = "vpcresources.k8s.aws", "SecurityGroupPolicy"

	client, err := discovery.NewDiscoveryClientForConfig(cfg)
	assertNoError(err)

	groups, err := client.ServerGroups()
	if err != nil {
		assertNoError(err)
	}
	for _, g := range groups.Groups {
		if g.Name != groupName {
			continue
		}
		for _, v := range g.Versions {
			resourceList, err := client.ServerResourcesForGroupVersion(v.GroupVersion)
			if err != nil {
				assertNoError(err)
			}
			for _, r := range resourceList.APIResources {
				if r.Kind == kind {
					log.Info("detected EKS environment")
					return true
				}
			}
		}
	}

	return false
}

func detectPlatform(ctx context.Context, cfg *rest.Config) string {
	switch {
	case isOpenshift(ctx, cfg):
		return "openshift"
	case isGKE(ctx, cfg):
		return "gke"
	case isEKS(ctx, cfg):
		return "eks"
	default:
		return "unknown"
	}
}

// getServerVersion returns the stringified server version (i.e., the same info `kubectl version`
// returns for the server)
// Any errors encountered will be logged and will return an empty string
func getServerVersion(ctx context.Context, cfg *rest.Config) string {
	log := logging.FromContext(ctx)
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		log.V(1).Info("upgrade check issue: could not retrieve discovery client",
			"response", err.Error())
		return ""
	}
	versionInfo, err := discoveryClient.ServerVersion()
	if err != nil {
		log.V(1).Info("upgrade check issue: could not retrieve server version",
			"response", err.Error())
		return ""
	}

	return versionInfo.String()
}

func getLogEncoder() zapcore.Encoder {
	consoleEnc := zapcore.NewConsoleEncoder(uzap.NewDevelopmentEncoderConfig())

	s, found := os.LookupEnv("LOG_STRUCTURED")
	if !found {
		return consoleEnc
	}

	useJson, err := strconv.ParseBool(s)
	if err != nil {
		return consoleEnc
	}
	if !useJson {
		return consoleEnc
	}

	return zapcore.NewJSONEncoder(uzap.NewProductionEncoderConfig())
}

func getLogLevel() zapcore.LevelEnabler {
	l, found := os.LookupEnv("LOG_LEVEL")
	if !found {
		return zapcore.InfoLevel
	}

	switch strings.ToUpper(l) {
	case "DEBUG":
		return zapcore.DebugLevel
	case "INFO":
		return zapcore.InfoLevel
	case "ERROR":
		return zapcore.ErrorLevel
	default:
		return zapcore.InfoLevel
	}
}

func isOpenshift(ctx context.Context, cfg *rest.Config) bool {
	log := logging.FromContext(ctx)

	const sccGroupName, sccKind = "security.openshift.io", "SecurityContextConstraints"

	client, err := discovery.NewDiscoveryClientForConfig(cfg)
	assertNoError(err)

	groups, err := client.ServerGroups()
	if err != nil {
		assertNoError(err)
	}
	for _, g := range groups.Groups {
		if g.Name != sccGroupName {
			continue
		}
		for _, v := range g.Versions {
			resourceList, err := client.ServerResourcesForGroupVersion(v.GroupVersion)
			if err != nil {
				assertNoError(err)
			}
			for _, r := range resourceList.APIResources {
				if r.Kind == sccKind {
					log.Info("detected Openshift environment")
					return true
				}
			}
		}
	}

	return false
}
