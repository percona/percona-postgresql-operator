package pgcluster

import (
	"context"
	"io"
	"os"
	"path/filepath"

	"go.opentelemetry.io/otel"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	"k8s.io/component-base/featuregate"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/percona/percona-postgresql-operator/internal/controller/postgrescluster"
	"github.com/percona/percona-postgresql-operator/internal/naming"
	"github.com/percona/percona-postgresql-operator/internal/util"
	"github.com/percona/percona-postgresql-operator/percona/controller/pgbackup"
	v2 "github.com/percona/percona-postgresql-operator/pkg/apis/pgv2.percona.com/v2"
	"github.com/percona/percona-postgresql-operator/pkg/apis/postgres-operator.crunchydata.com/v1beta1"
)

var k8sClient client.Client

func reconciler() *PGClusterReconciler {
	return (&PGClusterReconciler{
		Client:      k8sClient,
		Platform:    "unknown",
		KubeVersion: "1.25",
		Cron:        NewCronRegistry(),
	})
}

func crunchyReconciler() *postgrescluster.Reconciler {
	return &postgrescluster.Reconciler{
		Client:   k8sClient,
		Owner:    postgrescluster.ControllerName,
		Recorder: new(record.FakeRecorder),
		Tracer:   otel.Tracer("test"),
		PodExec:  func(string, string, string, io.Reader, io.Writer, io.Writer, ...string) error { return nil },
	}
}

func backupReconciler() *pgbackup.PGBackupReconciler {
	return &pgbackup.PGBackupReconciler{
		Client: k8sClient,
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

func readDefaultOperator(name, namespace string) (*appsv1.Deployment, error) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "deploy", "operator.yaml"))
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

func readDefaultBackup(name, namespace string) (*v2.PerconaPGBackup, error) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "deploy", "backup.yaml"))
	if err != nil {
		return nil, err
	}

	bcp := &v2.PerconaPGBackup{}

	if err := yaml.Unmarshal(data, bcp); err != nil {
		return nil, err
	}

	bcp.Name = name
	bcp.Namespace = namespace
	return bcp, nil
}

type fakeClient struct {
	client.Client
}

var _ = client.Client(new(fakeClient))

func (f *fakeClient) Patch(ctx context.Context, obj client.Object, patch client.Patch, options ...client.PatchOption) error {
	err := f.Client.Patch(ctx, obj, patch, options...)
	if !k8serrors.IsNotFound(err) {
		return err
	}
	if err := f.Client.Create(ctx, obj); err != nil {
		return err
	}
	return f.Client.Patch(ctx, obj, patch, options...)
}

func buildFakeClient(ctx context.Context, cr *v2.PerconaPGCluster, objs ...client.Object) (client.Client, error) {
	features := map[featuregate.Feature]featuregate.FeatureSpec{
		util.TablespaceVolumes: {Default: true},
		util.InstanceSidecars:  {Default: true},
		util.PGBouncerSidecars: {Default: true},
	}
	if err := util.DefaultMutableFeatureGate.Add(features); err != nil {
		return nil, err
	}

	s := scheme.Scheme

	if err := v1beta1.AddToScheme(scheme.Scheme); err != nil {
		return nil, err
	}
	if err := v2.AddToScheme(scheme.Scheme); err != nil {
		return nil, err
	}

	objs = append(objs, cr)
	cr.Default()
	postgresCluster, err := cr.ToCrunchy(ctx, nil, s)
	if err != nil {
		return nil, err
	}
	objs = append(objs, postgresCluster)

	dcs := &corev1.Endpoints{ObjectMeta: naming.PatroniDistributedConfiguration(postgresCluster)}
	dcs.Annotations = map[string]string{
		"initialize": "system-identifier",
	}
	objs = append(objs, dcs)

	cl := new(fakeClient)
	cl.Client = fake.NewClientBuilder().WithScheme(s).WithObjects(objs...).WithStatusSubresource(objs...).Build()

	return cl, nil
}
