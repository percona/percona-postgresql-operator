package pgcluster

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.opentelemetry.io/otel"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"k8s.io/component-base/featuregate"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/percona/percona-postgresql-operator/internal/controller/postgrescluster"
	"github.com/percona/percona-postgresql-operator/internal/util"
	v2 "github.com/percona/percona-postgresql-operator/pkg/apis/pgv2.percona.com/v2"
	"github.com/percona/percona-postgresql-operator/pkg/apis/postgres-operator.crunchydata.com/v1beta1"
	//+kubebuilder:scaffold:imports
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var (
	cfg       *rest.Config
	k8sClient client.Client
	testEnv   *envtest.Environment
)

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "PerconaPG Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}

	features := map[featuregate.Feature]featuregate.FeatureSpec{
		util.TablespaceVolumes: {Default: true},
		util.InstanceSidecars:  {Default: true},
		util.PGBouncerSidecars: {Default: true},
	}

	Expect(util.DefaultMutableFeatureGate.Add(features)).To(Succeed())

	var err error
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	err = v1beta1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	err = v2.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	Expect(os.Setenv("DISABLE_TELEMETRY", "true")).To(Succeed())
})

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
	Expect(os.Unsetenv("DISABLE_TELEMETRY")).To(Succeed())
})

func reconciler() *PGClusterReconciler {
	return (&PGClusterReconciler{
		Client:      k8sClient,
		Platform:    "unknown",
		KubeVersion: "1.25",
	})
}

func crunchyReconciler() *postgrescluster.Reconciler {
	return &postgrescluster.Reconciler{
		Client:   k8sClient,
		Owner:    postgrescluster.ControllerName,
		Recorder: new(record.FakeRecorder),
		Tracer:   otel.Tracer("test"),
	}
}

func readDefaultCR(name, namespace string) (*v2.PerconaPGCluster, error) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "deploy", "cr.yaml"))
	if err != nil {
		return nil, err
	}

	cr := &v2.PerconaPGCluster{}

	if err := yaml.Unmarshal(data, cr); err != nil {
		return nil, err
	}

	cr.Name = name
	cr.Namespace = namespace
	return cr, nil
}

func updateCrunchyPGClusterStatus(ctx context.Context, nn types.NamespacedName, update func(*v1beta1.PostgresCluster)) {
	pgc := &v1beta1.PostgresCluster{}
	Eventually(func() bool {
		err := k8sClient.Get(ctx, nn, pgc)
		return err == nil
	}, time.Second*15, time.Millisecond*250).Should(BeTrue())

	update(pgc)

	Expect(k8sClient.Status().Update(ctx, pgc)).Should(Succeed())
}

func updatePerconaPGClusterCR(ctx context.Context, nn types.NamespacedName, update func(*v2.PerconaPGCluster)) {
	cr := &v2.PerconaPGCluster{}
	Eventually(func() bool {
		err := k8sClient.Get(ctx, nn, cr)
		return err == nil
	}, time.Second*15, time.Millisecond*250).Should(BeTrue())

	update(cr)

	Expect(k8sClient.Update(ctx, cr)).Should(Succeed())
}

func readDefaultOperator(name, namespace string) (*appsv1.Deployment, error) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "deploy", "cr.yaml"))
	if err != nil {
		return nil, err
	}

	cr := &appsv1.Deployment{}

	if err := yaml.Unmarshal(data, cr); err != nil {
		return nil, err
	}

	cr.Name = name
	cr.Namespace = namespace
	return cr, nil
}
