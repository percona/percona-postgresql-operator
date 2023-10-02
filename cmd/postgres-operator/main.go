package main

/*
Copyright 2017 - 2023 Crunchy Data Solutions, Inc.
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

import (
	"context"
	"os"
	"strconv"
	"strings"

	"github.com/pkg/errors"
	"go.opentelemetry.io/otel"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	cruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/percona/percona-postgresql-operator/internal/controller/postgrescluster"
	"github.com/percona/percona-postgresql-operator/internal/controller/runtime"
	"github.com/percona/percona-postgresql-operator/internal/logging"
	"github.com/percona/percona-postgresql-operator/internal/upgradecheck"
	"github.com/percona/percona-postgresql-operator/internal/util"
	perconaController "github.com/percona/percona-postgresql-operator/percona/controller"
	"github.com/percona/percona-postgresql-operator/percona/controller/pgbackup"
	"github.com/percona/percona-postgresql-operator/percona/controller/pgcluster"
	"github.com/percona/percona-postgresql-operator/percona/controller/pgrestore"
	v2 "github.com/percona/percona-postgresql-operator/pkg/apis/pgv2.percona.com/v2"
)

var versionString string

// assertNoError panics when err is not nil.
func assertNoError(err error) {
	if err != nil {
		panic(err)
	}
}

func initLogging() {
	// Configure a singleton that treats logr.Logger.V(1) as logrus.DebugLevel.
	var verbosity int
	if strings.EqualFold(os.Getenv("CRUNCHY_DEBUG"), "true") {
		verbosity = 1
	}
	logging.SetLogSink(logging.Logrus(os.Stdout, versionString, 1, verbosity))
}

func main() {
	// Set any supplied feature gates; panic on any unrecognized feature gate
	err := util.AddAndSetFeatureGates(os.Getenv("PGO_FEATURE_GATES"))
	assertNoError(err)
	// Needed for PMM
	err = util.DefaultMutableFeatureGate.SetFromMap(map[string]bool{
		string(util.InstanceSidecars): true,
	})
	assertNoError(err)

	otelFlush, err := initOpenTelemetry()
	assertNoError(err)
	defer otelFlush()

	initLogging()

	// create a context that will be used to stop all controllers on a SIGTERM or SIGINT
	ctx := cruntime.SetupSignalHandler()
	log := logging.FromContext(ctx)
	log.V(1).Info("debug flag set to true")

	// We are forcing `InstanceSidecars` feature to be enabled.
	// It's necessary to get actual feature gate values instead of using
	// `PGO_FEATURE_GATES` env var to print logs
	var featureGates []string
	for k := range util.DefaultMutableFeatureGate.GetAll() {
		f := string(k) + "=" + strconv.FormatBool(util.DefaultMutableFeatureGate.Enabled(k))
		featureGates = append(featureGates, f)
	}

	log.Info("feature gates enabled",
		"PGO_FEATURE_GATES", strings.Join(featureGates, ","))

	cruntime.SetLogger(log)

	cfg, err := runtime.GetConfig()
	assertNoError(err)

	cfg.Wrap(otelTransportWrapper())

	// Configure client-go to suppress warnings when warning headers are encountered. This prevents
	// warnings from being logged over and over again during reconciliation (e.g. this will suppress
	// deprecation warnings when using an older version of a resource for backwards compatibility).
	rest.SetDefaultWarningHandler(rest.NoWarnings{})

	mgr, err := runtime.CreateRuntimeManager(os.Getenv("PGO_TARGET_NAMESPACE"), cfg, false)
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
			isOpenshift(ctx, mgr.GetConfig()), os.Getenv("CHECK_FOR_UPGRADES_URL"), versionString))
	}

	assertNoError(mgr.Start(ctx))
	log.Info("signal received, exiting")
}

// addControllersToManager adds all PostgreSQL Operator controllers to the provided controller
// runtime manager.
func addControllersToManager(ctx context.Context, mgr manager.Manager) error {
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

	pc := &pgcluster.PGClusterReconciler{
		Client:            mgr.GetClient(),
		Owner:             pgcluster.PGClusterControllerName,
		Recorder:          mgr.GetEventRecorderFor(pgcluster.PGClusterControllerName),
		Tracer:            otel.Tracer(pgcluster.PGClusterControllerName),
		Platform:          detectPlatform(ctx, mgr.GetConfig()),
		KubeVersion:       getServerVersion(ctx, mgr.GetConfig()),
		CrunchyController: cm.Controller(),
	}
	if err := pc.SetupWithManager(mgr); err != nil {
		return err
	}

	pb := &pgbackup.PGBackupReconciler{
		Client:   mgr.GetClient(),
		Owner:    pgbackup.PGBackupControllerName,
		Recorder: mgr.GetEventRecorderFor(pgbackup.PGBackupControllerName),
		Tracer:   otel.Tracer(pgbackup.PGBackupControllerName),
	}
	if err := pb.SetupWithManager(mgr); err != nil {
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

	return nil
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
					log.Info("detected OpenShift environment")
					return true
				}
			}
		}
	}

	return false
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
