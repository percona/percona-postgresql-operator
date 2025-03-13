package pgcluster

import (
	"context"
	"io"
	"os"
	"path/filepath"

	"github.com/pkg/errors"
	"go.opentelemetry.io/otel"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/event"

	"github.com/percona/percona-postgresql-operator/internal/controller/postgrescluster"
	"github.com/percona/percona-postgresql-operator/internal/naming"
	"github.com/percona/percona-postgresql-operator/percona/controller/pgbackup"
	pNaming "github.com/percona/percona-postgresql-operator/percona/naming"
	"github.com/percona/percona-postgresql-operator/percona/utils/registry"
	"github.com/percona/percona-postgresql-operator/percona/watcher"
	v2 "github.com/percona/percona-postgresql-operator/pkg/apis/pgv2.percona.com/v2"
	"github.com/percona/percona-postgresql-operator/pkg/apis/postgres-operator.crunchydata.com/v1beta1"
)

var k8sClient client.Client

func reconciler(cr *v2.PerconaPGCluster) *PGClusterReconciler {
	externalChan := make(chan event.GenericEvent)
	stopChan := make(chan event.DeleteEvent)

	watcherName, _ := watcher.GetWALWatcher(cr)
	reg := registry.New()

	dummyWatchwer := func() {
		for range stopChan {
			return
		}
	}
	err := reg.Add(watcherName, dummyWatchwer)
	if err != nil {
		panic(err)
	}

	go dummyWatchwer()

	return (&PGClusterReconciler{
		Client:               k8sClient,
		Platform:             "unknown",
		KubeVersion:          "1.26",
		Cron:                 NewCronRegistry(),
		Watchers:             reg,
		ExternalChan:         externalChan,
		StopExternalWatchers: stopChan,
	})
}

func crunchyReconciler() *postgrescluster.Reconciler {
	return &postgrescluster.Reconciler{
		Client:   k8sClient,
		Owner:    postgrescluster.ControllerName,
		Recorder: new(record.FakeRecorder),
		Tracer:   otel.Tracer("test"),
		PodExec: func(context.Context, string, string, string, io.Reader, io.Writer, io.Writer, ...string) error {
			return nil
		},
	}
}

func backupReconciler() *pgbackup.PGBackupReconciler {
	return &pgbackup.PGBackupReconciler{
		Client: k8sClient,
	}
}

func readTestCR(name, namespace, testFile string) (*v2.PerconaPGCluster, error) {
	data, err := os.ReadFile(filepath.Join("..", "testdata", testFile))
	if err != nil {
		return nil, err
	}

	cr := &v2.PerconaPGCluster{}

	if err := yaml.Unmarshal(data, cr); err != nil {
		return nil, err
	}

	cr.Name = name
	cr.Namespace = namespace
	if cr.Spec.PostgresVersion == 0 {
		return nil, errors.New("postgresVersion should be specified")
	}
	if cr.Annotations == nil {
		cr.Annotations = make(map[string]string)
	}

	cr.Annotations[pNaming.AnnotationCustomPatroniVersion] = "4.0.0"
	cr.Status.Postgres.Version = cr.Spec.PostgresVersion
	return cr, nil
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
	if cr.Annotations == nil {
		cr.Annotations = make(map[string]string)
	}
<<<<<<< HEAD
	cr.Annotations[pNaming.InternalAnnotationDisablePatroniVersionCheck] = "true"
=======
	cr.Annotations[pNaming.AnnotationCustomPatroniVersion] = "4.0.0"
>>>>>>> upstream/release-2.6.0
	cr.Namespace = namespace
	cr.Status.Postgres.Version = cr.Spec.PostgresVersion
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
	s := scheme.Scheme

	if err := v1beta1.AddToScheme(s); err != nil {
		return nil, err
	}
	if err := v2.AddToScheme(s); err != nil {
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
