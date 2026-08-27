package testutils

import (
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	pgv3 "github.com/percona/percona-postgresql-operator/v3/pkg/apis/pgv2.percona.com/v2"
	crunchyv1beta1 "github.com/percona/percona-postgresql-operator/v3/pkg/apis/upstream.pgv2.percona.com/v1beta1"
)

func BuildFakeClient(initObjs ...client.Object) client.Client {
	scheme := runtime.NewScheme()

	perconaTypes := []runtime.Object{
		new(pgv3.PerconaPGCluster),
		new(pgv3.PerconaPGClusterList),
		new(pgv3.PerconaPGBackup),
		new(pgv3.PerconaPGBackupList),
		new(pgv3.PerconaPGRestore),
		new(pgv3.PerconaPGRestoreList),
		new(pgv3.PerconaPGUpgrade),
		new(pgv3.PerconaPGUpgradeList),
	}

	crunchyTypes := []runtime.Object{
		new(crunchyv1beta1.PostgresCluster),
		new(crunchyv1beta1.PostgresClusterList),
		new(crunchyv1beta1.PGUpgrade),
		new(crunchyv1beta1.PGUpgradeList),
	}

	scheme.AddKnownTypes(pgv3.GroupVersion, perconaTypes...)
	scheme.AddKnownTypes(crunchyv1beta1.GroupVersion, crunchyTypes...)

	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(initObjs...).
		WithIndex(new(pgv3.PerconaPGBackup), pgv3.IndexFieldPGCluster, pgv3.PGClusterIndexerFunc).
		Build()
}
