//go:build envtest
// +build envtest

package pgcluster

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"reflect"
	"testing"
	"time"

	pbVersion "github.com/Percona-Lab/percona-version-service/versionpb"
	gwRuntime "github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/pkg/errors"
	"go.nhat.io/grpcmock"
	"google.golang.org/grpc"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/percona/percona-postgresql-operator/percona/version"
	v2 "github.com/percona/percona-postgresql-operator/pkg/apis/pgv2.percona.com/v2"
)

var _ = Describe("Ensure Version", Ordered, func() {
	ctx := context.Background()

	const crName = "version"
	const ns = crName
	const operatorName = crName + "-operator"
	crNamespacedName := types.NamespacedName{Name: crName, Namespace: ns}

	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:      crName,
			Namespace: ns,
		},
	}
	addr := "127.0.0.1"
	gwPort := 11000
	unimplGwPort := 13000
	defaultEndpoint := fmt.Sprintf("http://%s:%d", addr, gwPort)
	unimplEndpoint := fmt.Sprintf("http://%s:%d", addr, unimplGwPort)

	BeforeAll(func() {
		By("Creating the Namespace to perform the tests")
		err := k8sClient.Create(ctx, namespace)
		Expect(err).To(Not(HaveOccurred()))
	})

	AfterAll(func() {
		// TODO(user): Attention if you improve this code by adding other context test you MUST
		// be aware of the current delete namespace limitations. More info: https://book.kubebuilder.io/reference/envtest.html#testing-considerations
		By("Deleting the Namespace to perform the tests")
		_ = k8sClient.Delete(ctx, namespace)

		Expect(os.Unsetenv("PERCONA_VS_FALLBACK_URI")).To(Succeed())
	})

	cr, err := readDefaultCR(crName, ns)
	It("should read default cr.yaml", func() {
		Expect(err).NotTo(HaveOccurred())
	})

	It("should create PerconaPGCluster", func() {
		cr.Spec.PostgresVersion = 14
		cr.Spec.PMM.Enabled = true
		cr.Labels = make(map[string]string)
		cr.Labels["helm.sh/chart"] = "percona-postgresql-operator"
		for i := range cr.Spec.InstanceSets {
			cr.Spec.InstanceSets[i].Sidecars = append(cr.Spec.InstanceSets[i].Sidecars, corev1.Container{
				Name:  "test-container",
				Image: "test-image",
			})
		}
		Expect(k8sClient.Create(ctx, cr)).Should(Succeed())
	})

	operatorDepl, err := readDefaultOperator(operatorName, ns)
	It("should read default operator.yaml", func() {
		Expect(err).NotTo(HaveOccurred())
	})
	It("should update operatorDepl", func() {
		operatorDepl.Labels = make(map[string]string)
		operatorDepl.Labels["helm.sh/chart"] = "percona-postgresql-operator"
	})

	Context("Reconcile controller", func() {
		It("Controller should reconcile", func() {
			_, err := reconciler().Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("Reconcile version", func() {
		Context("Normal version service", func() {
			It("should start normal version service", func() {
				vsServer := fakeVersionService(addr, gwPort, false, string(cr.GetUID()))
				Expect(vsServer.Start(new(testing.T))).To(Succeed())
			})
			It("should succeed while using default version service endpoint", func() {
				Expect(os.Setenv("PERCONA_VS_FALLBACK_URI", defaultEndpoint)).To(Succeed())

				vm := reconciler().getVersionMeta(cr, operatorDepl)
				Expect(version.EnsureVersion(ctx, vm)).To(Succeed())
			})
		})

		Context("Unimplemented version service", func() {
			It("should start unimplemented version service", func() {
				unimlVsServer := fakeVersionService(addr, unimplGwPort, true, string(cr.GetUID()))
				Expect(unimlVsServer.Start(new(testing.T))).To(Succeed())
			})
			It("should fail while using unimplemented version service endpoint", func() {
				Expect(os.Setenv("PERCONA_VS_FALLBACK_URI", unimplEndpoint)).To(Succeed())

				vm := reconciler().getVersionMeta(cr, operatorDepl)
				Expect(version.EnsureVersion(ctx, vm)).NotTo(BeNil())
			})
		})
	})
})

type fakeVS struct {
	addr          string
	gwPort        int
	unimplemented bool
	crUID         string
}

func (vs *fakeVS) Apply(_ context.Context, req any) (any, error) {
	if vs.unimplemented {
		return nil, errors.New("unimplemented")
	}

	r := req.(*pbVersion.ApplyRequest)

	have := &pbVersion.ApplyRequest{
		Apply:              r.GetApply(),
		BackupVersion:      r.GetBackupVersion(),
		CustomResourceUid:  r.GetCustomResourceUid(),
		DatabaseVersion:    r.GetDatabaseVersion(),
		KubeVersion:        r.GetKubeVersion(),
		NamespaceUid:       r.GetNamespaceUid(),
		OperatorVersion:    r.GetOperatorVersion(),
		Platform:           r.GetPlatform(),
		PmmVersion:         r.GetPmmVersion(),
		PmmEnabled:         r.GetPmmEnabled(),
		HelmDeployCr:       r.GetHelmDeployCr(),
		HelmDeployOperator: r.GetHelmDeployOperator(),
		SidecarsUsed:       r.GetSidecarsUsed(),
		Product:            r.GetProduct(),
	}
	want := &pbVersion.ApplyRequest{
		Apply:              "disabled",
		BackupVersion:      "",
		CustomResourceUid:  vs.crUID,
		DatabaseVersion:    "14",
		KubeVersion:        reconciler().KubeVersion,
		NamespaceUid:       "",
		OperatorVersion:    v2.Version,
		Platform:           reconciler().Platform,
		PmmVersion:         "",
		PmmEnabled:         true,
		HelmDeployCr:       true,
		HelmDeployOperator: true,
		SidecarsUsed:       true,
		Product:            v2.ProductName,
	}

	if !reflect.DeepEqual(have, want) {
		return nil, errors.Errorf("Have: %v; Want: %v", have, want)
	}

	return &pbVersion.VersionResponse{}, nil
}
func fakeVersionService(addr string, gwport int, unimplemented bool, crUID string) *fakeVS {
	return &fakeVS{
		addr:          addr,
		gwPort:        gwport,
		unimplemented: unimplemented,
		crUID:         crUID,
	}
}

type mockClientConn struct {
	dialer grpcmock.ContextDialer
}

func (m *mockClientConn) Invoke(ctx context.Context, method string, args, reply any, opts ...grpc.CallOption) error {
	return grpcmock.InvokeUnary(ctx, method, args, reply, grpcmock.WithInsecure(), grpcmock.WithCallOptions(opts...), grpcmock.WithContextDialer(m.dialer))
}
func (m *mockClientConn) NewStream(ctx context.Context, desc *grpc.StreamDesc, method string, opts ...grpc.CallOption) (grpc.ClientStream, error) {
	return nil, errors.New("unimplemented")
}

func (vs *fakeVS) Start(t *testing.T) error {
	_, d := grpcmock.MockServerWithBufConn(
		grpcmock.RegisterServiceFromInstance("version.VersionService", (*pbVersion.VersionServiceServer)(nil)),
		func(s *grpcmock.Server) {
			s.ExpectUnary("/version.VersionService/Apply").Run(vs.Apply)
		},
	)(t)

	gwmux := gwRuntime.NewServeMux()
	err := pbVersion.RegisterVersionServiceHandlerClient(context.Background(), gwmux, pbVersion.NewVersionServiceClient(&mockClientConn{d}))
	if err != nil {
		return errors.Wrap(err, "failed to register gateway")
	}
	gwServer := &http.Server{
		Addr:              fmt.Sprintf("%s:%d", vs.addr, vs.gwPort),
		Handler:           gwmux,
		ReadHeaderTimeout: time.Second * 10,
	}
	gwLis, err := net.Listen("tcp", gwServer.Addr)
	if err != nil {
		return errors.Wrap(err, "failed to listen gateway")
	}
	go func() {
		if err := gwServer.Serve(gwLis); err != nil {
			t.Error("failed to serve gRPC-Gateway", err)
		}
	}()

	return nil
}
