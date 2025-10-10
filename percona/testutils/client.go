package testutils

import (
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	pgv2 "github.com/percona/percona-postgresql-operator/v2/pkg/apis/pgv2.percona.com/v2"
	crunchyv1beta1 "github.com/percona/percona-postgresql-operator/v2/pkg/apis/postgres-operator.crunchydata.com/v1beta1"
)

func BuildFakeClient(initObjs ...client.Object) client.Client {
	scheme := runtime.NewScheme()

	perconaTypes := []runtime.Object{
		new(pgv2.PerconaPGCluster),
		new(pgv2.PerconaPGClusterList),
		new(pgv2.PerconaPGBackup),
		new(pgv2.PerconaPGBackupList),
		new(pgv2.PerconaPGRestore),
		new(pgv2.PerconaPGRestoreList),
		new(pgv2.PerconaPGUpgrade),
		new(pgv2.PerconaPGUpgradeList),
	}

	crunchyTypes := []runtime.Object{
		new(crunchyv1beta1.PostgresCluster),
		new(crunchyv1beta1.PostgresClusterList),
		new(crunchyv1beta1.PGUpgrade),
		new(crunchyv1beta1.PGUpgradeList),
	}

	scheme.AddKnownTypes(pgv2.GroupVersion, perconaTypes...)
	scheme.AddKnownTypes(crunchyv1beta1.GroupVersion, crunchyTypes...)

	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(initObjs...).
		WithIndex(new(pgv2.PerconaPGBackup), pgv2.IndexFieldPGCluster, pgv2.PGClusterIndexerFunc).
		Build()
}
