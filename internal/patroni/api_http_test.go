package patroni

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-logr/logr"
	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func createMockKubeClient() client.Client {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	// Use a valid self-signed certificate.
	// This will be parsed, so it really needs to be valid.
	certData := map[string][]byte{
		certAuthorityFileKey: []byte(`-----BEGIN CERTIFICATE-----
MIIDczCCAlugAwIBAgIUDnQ23t9B4fcouJacQrL93ejxKdgwDQYJKoZIhvcNAQEL
BQAwSTELMAkGA1UEBhMCVVMxDTALBgNVBAgMBFRlc3QxDTALBgNVBAcMBFRlc3Qx
DTALBgNVBAoMBFRlc3QxDTALBgNVBAMMBHRlc3QwHhcNMjUwOTI2MTM0MTE2WhcN
MjYwOTI2MTM0MTE2WjBJMQswCQYDVQQGEwJVUzENMAsGA1UECAwEVGVzdDENMAsG
A1UEBwwEVGVzdDENMAsGA1UECgwEVGVzdDENMAsGA1UEAwwEdGVzdDCCASIwDQYJ
KoZIhvcNAQEBBQADggEPADCCAQoCggEBAOqni0gg5nXsnFiFWdz0yCcn5cdAz/W3
6WlewEEF+sjUX9+cbUXqeFRisWX64FcMZ5802I8SJHEhavtfzWdBdKqvlQ0XeKRR
jmVXByS790IlgVZQ0aWOKuSJVsPRhwNQ34U4EcPA9xjU9PWR2ULeNEmXAOIVJEhP
2vzWKbF5xPKe3FJtx4gEi3YyxiPbxP45Hf5b6B4duGml11zO+ZHSrTzte04eKoya
BN/bYHdA4kHh5PksxdZIUQFUX6KUorqEMv4FJwNs4e73YY7zwdW3c0IlztjGcUGs
kVnDrLNaKep+t1iNyoXeB6reZ9OwqVZBwQuTCorsaWjUFIbhCsRWjtUCAwEAAaNT
MFEwHQYDVR0OBBYEFAx9cxoaa9mymVRi69uF9boSNyGHMB8GA1UdIwQYMBaAFAx9
cxoaa9mymVRi69uF9boSNyGHMA8GA1UdEwEB/wQFMAMBAf8wDQYJKoZIhvcNAQEL
BQADggEBAJ00idIco8+KG6LZlXp5xXxM29xPU5QvujUfDbW/E2VUKHcePGmuBwHF
m/qYM/itZGSOuw2nsIOIUHDVNBX7vVHDeEzXlSC++GsEo7ptlsD2xSkIjCeY8o1d
PWaqiTI8jeXSISjFIiLF6D4lG7i7JxhNTKUqp5R7HKxiQr5vDxD5YlZuevSZJQIu
QplCHd646HHd1F07MVSfuGWeN1bf/rSQfvUkrnTDIUgz2oMye7uF6aDHRsteBGuw
6eM3ewTAxbEZzxSA0mMUgNbXO9do3OGr6UVFmWQ47NEkJBNR511DER5K82B6ZM/w
c6O9VaABYfNuet0+w/J9nKEdi2r16+Y=
-----END CERTIFICATE-----`),
		certServerFileKey: []byte(`-----BEGIN CERTIFICATE-----
MIIDczCCAlugAwIBAgIUDnQ23t9B4fcouJacQrL93ejxKdgwDQYJKoZIhvcNAQEL
BQAwSTELMAkGA1UEBhMCVVMxDTALBgNVBAgMBFRlc3QxDTALBgNVBAcMBFRlc3Qx
DTALBgNVBAoMBFRlc3QxDTALBgNVBAMMBHRlc3QwHhcNMjUwOTI2MTM0MTE2WhcN
MjYwOTI2MTM0MTE2WjBJMQswCQYDVQQGEwJVUzENMAsGA1UECAwEVGVzdDENMAsG
A1UEBwwEVGVzdDENMAsGA1UECgwEVGVzdDENMAsGA1UEAwwEdGVzdDCCASIwDQYJ
KoZIhvcNAQEBBQADggEPADCCAQoCggEBAOqni0gg5nXsnFiFWdz0yCcn5cdAz/W3
6WlewEEF+sjUX9+cbUXqeFRisWX64FcMZ5802I8SJHEhavtfzWdBdKqvlQ0XeKRR
jmVXByS790IlgVZQ0aWOKuSJVsPRhwNQ34U4EcPA9xjU9PWR2ULeNEmXAOIVJEhP
2vzWKbF5xPKe3FJtx4gEi3YyxiPbxP45Hf5b6B4duGml11zO+ZHSrTzte04eKoya
BN/bYHdA4kHh5PksxdZIUQFUX6KUorqEMv4FJwNs4e73YY7zwdW3c0IlztjGcUGs
kVnDrLNaKep+t1iNyoXeB6reZ9OwqVZBwQuTCorsaWjUFIbhCsRWjtUCAwEAAaNT
MFEwHQYDVR0OBBYEFAx9cxoaa9mymVRi69uF9boSNyGHMB8GA1UdIwQYMBaAFAx9
cxoaa9mymVRi69uF9boSNyGHMA8GA1UdEwEB/wQFMAMBAf8wDQYJKoZIhvcNAQEL
BQADggEBAJ00idIco8+KG6LZlXp5xXxM29xPU5QvujUfDbW/E2VUKHcePGmuBwHF
m/qYM/itZGSOuw2nsIOIUHDVNBX7vVHDeEzXlSC++GsEo7ptlsD2xSkIjCeY8o1d
PWaqiTI8jeXSISjFIiLF6D4lG7i7JxhNTKUqp5R7HKxiQr5vDxD5YlZuevSZJQIu
QplCHd646HHd1F07MVSfuGWeN1bf/rSQfvUkrnTDIUgz2oMye7uF6aDHRsteBGuw
6eM3ewTAxbEZzxSA0mMUgNbXO9do3OGr6UVFmWQ47NEkJBNR511DER5K82B6ZM/w
c6O9VaABYfNuet0+w/J9nKEdi2r16+Y=
-----END CERTIFICATE-----
-----BEGIN PRIVATE KEY-----
MIIEvQIBADANBgkqhkiG9w0BAQEFAASCBKcwggSjAgEAAoIBAQDqp4tIIOZ17JxY
hVnc9MgnJ+XHQM/1t+lpXsBBBfrI1F/fnG1F6nhUYrFl+uBXDGefNNiPEiRxIWr7
X81nQXSqr5UNF3ikUY5lVwcku/dCJYFWUNGljirkiVbD0YcDUN+FOBHDwPcY1PT1
kdlC3jRJlwDiFSRIT9r81imxecTyntxSbceIBIt2MsYj28T+OR3+W+geHbhppddc
zvmR0q087XtOHiqMmgTf22B3QOJB4eT5LMXWSFEBVF+ilKK6hDL+BScDbOHu92GO
88HVt3NCJc7YxnFBrJFZw6yzWinqfrdYjcqF3geq3mfTsKlWQcELkwqK7Glo1BSG
4QrEVo7VAgMBAAECggEAHi9wUNZ+nvPRhueckDpi1vqgadnSBqNiYL4iEBtDT/tV
2++E9QX89an+dQZpPnlniQjkxL7KNk1ctDp2M06twdk1XMpEqCqfnTStRBHz9Cvb
7+0Uku3vYZezNBxreEc6gaodSuezQZv/aOman6ny4vaMVAjxMmYnXvfzxBNMfQMo
dqmcOHXG3hrcXcIrsq297k4GmgnBOjlv4pLIIWfSSECcFqhFD8uZamnSlh96b4C1
BU2RVnjsYx/asATvJzDAP+zMt+2s9MV9j/iYAyzmkTywk6RFheLXR/VW+9o2CSi4
0+8ypiLnI9K/KJWw5OI33MzGRBDUNipR4D7brh8nfwKBgQD4+AcB7QGkzhobv1Hg
IvP1kk5BjPZhEGxWlnQHM8u4LwrJCQEtwd2eF9KAR53kenfdR9wJ7dipGkRcVWqq
fUSU4VOZdCg037AMIVdv/bGr2fbtnBWpIxyVYKg1mtx/6UhYoGBkx7kYDVOUsK9a
SN4HZOiu7mA/a8JJYy1/oK3gXwKBgQDxSAcfbQHMQc1Ar29ixgApxZ50FOpzDt5f
HSa2Dgt1/Fo8mAgeknKi4AwA4GzY2sSkKwEQkSQvg/DZFNi8Tuyk//Nhx7ynwy7o
y5LBQ3HYCxarLPcyACez3C4YsoQiqu9YL572p23SLxby63hrePj/JJa9Tbb01UoH
rAWnvp8NSwKBgQDyCS3G0YInlbYMA5K1M0W4FuO9Fizvb+fixaFG3zPNeu4hQn/C
3BV2+/HIg9cbp3Ofy5w+itt2ifKrUN7Bn8ZsdiGvrRzpSgz7ve4jEZ8IUn2bwYHN
TDUdgzoD4uk58LBEeKU9VGy81TfL9XiDbRNsXM1YQqWPAlN+xMwWpz5iQQKBgF+r
kbdyP55AESSu61mc7P+jLjsU+Al7Qc0w/+J8GytDTnxsQ/vrUa0nbVsDoeUyiXoW
2ys4gcKdbGiHDZFNMiQSoOyKiFF04SrJXX1oQsHJU8m34KRgz11P1q9QSXh9kr3C
1CM1LCSFK3JSz8K9iu2QEn0pTXwy/lGgcfWbbfGVAoGAWd6Xra6e4qidxcRlEtdp
B7ghQKYwX4aOCPCtcTSW+aaQNiRZQU5V8XB5I79V1CyjPDrOuvva/2KslvQvOxvB
A7e9QkYI0UcYQIHYTi0HEaCm9K6FcjVcc35DhIGhq9jBkb9bwa4amj+KAdiTXIwI
m6N/96rp2DBjm4avLKb8jo0=
-----END PRIVATE KEY-----`),
	}

	// Mock Patroni certificates.
	secrets := []*corev1.Secret{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-namespace-instance1-abc1-certs",
				Namespace: "test-namespace",
			},
			Data: certData,
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-namespace-instance1-abc2-certs",
				Namespace: "test-namespace",
			},
			Data: certData,
		},
	}

	objects := make([]client.Object, len(secrets))
	for i, secret := range secrets {
		objects[i] = secret
	}

	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objects...).
		Build()
}

func TestHTTPClientChangePrimaryAndWait(t *testing.T) {
	t.Run("Arguments", func(t *testing.T) {
		var capturedRequest *http.Request
		var capturedBody map[string]any

		server := httptest.NewTLSServer(
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				capturedRequest = r
				if err := json.NewDecoder(r.Body).Decode(&capturedBody); err == nil {
					w.WriteHeader(200)
					w.Write([]byte(`Successfully switched over to "new"`))
				} else {
					w.WriteHeader(400)
				}
			}),
		)
		defer server.Close()

		mockClient := createMockKubeClient()
		httpClient := HTTPClient{
			kubeClient: mockClient,
			client: instanceClient{
				baseUrl:    server.URL,
				httpClient: server.Client(),
				logger:     logr.Discard(),
			},
			logger: logr.Discard(),
		}

		success, err := httpClient.ChangePrimaryAndWait(context.Background(), "old", "new", true)

		assert.NilError(t, err)
		assert.Assert(t, success)
		assert.Equal(t, capturedRequest.Method, "POST")
		assert.Assert(t, strings.HasSuffix(capturedRequest.URL.Path, "/switchover"))
		assert.Equal(t, capturedRequest.Header.Get("Content-Type"), "application/json")
		assert.DeepEqual(t, capturedBody, map[string]any{
			"leader":    "old",
			"candidate": "new",
		})
	})

	t.Run("Error", func(t *testing.T) {
		server := httptest.NewTLSServer(
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(500)
				w.Write([]byte(`Switchover failed: cluster is not healthy`))
			}),
		)
		defer server.Close()

		mockClient := createMockKubeClient()
		httpClient := HTTPClient{
			kubeClient: mockClient,
			client: instanceClient{
				baseUrl:    server.URL,
				httpClient: server.Client(),
				logger:     logr.Discard(),
			},
			logger: logr.Discard(),
		}

		success, err := httpClient.ChangePrimaryAndWait(context.Background(), "old", "new", true)

		assert.Assert(t, err != nil)
		assert.Assert(t, !success)
		assert.Assert(t, strings.Contains(err.Error(), "switchover failed with status 500"))
	})

	t.Run("EmptyLeader", func(t *testing.T) {
		mockClient := createMockKubeClient()
		httpClient := HTTPClient{
			kubeClient: mockClient,
			client: instanceClient{
				logger: logr.Discard(),
			},
			logger: logr.Discard(),
		}

		success, err := httpClient.ChangePrimaryAndWait(context.Background(), "", "new", true)

		assert.Assert(t, err != nil)
		assert.Assert(t, !success)
		assert.Assert(t, strings.Contains(err.Error(), "leader is required"))
	})

	// The current behavior is that if patroni switches over to a candidate
	// different than the one we specified, we should return success = false.
	t.Run("DifferentCandidate", func(t *testing.T) {
		server := httptest.NewTLSServer(
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(200)
				// Patroni chose different candidate - no "Successfully"
				w.Write([]byte(`Switched over to "different-node" instead of "new"`))
			}),
		)
		defer server.Close()

		mockClient := createMockKubeClient()
		httpClient := HTTPClient{
			kubeClient: mockClient,
			client: instanceClient{
				baseUrl:    server.URL,
				httpClient: server.Client(),
				logger:     logr.Discard(),
			},
			logger: logr.Discard(),
		}

		success, err := httpClient.ChangePrimaryAndWait(context.Background(), "old", "new", true)

		assert.NilError(t, err)    // No error, but success is false
		assert.Assert(t, !success) // Should return false like CLI does
	})
}

func TestHTTPClientSwitchoverAndWait(t *testing.T) {
	t.Run("Arguments", func(t *testing.T) {
		var capturedSwitchoverRequest *http.Request
		var capturedSwitchoverBody map[string]any
		switchoverCalled := false

		server := httptest.NewTLSServer(
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/cluster") {
					response := map[string]any{
						"members": []map[string]any{
							{
								"name":     "current-leader",
								"role":     "leader",
								"state":    "running",
								"timeline": int64(4),
							},
						},
					}
					w.Header().Set("Content-Type", "application/json")
					json.NewEncoder(w).Encode(response)
				} else if strings.HasSuffix(r.URL.Path, "/switchover") {
					capturedSwitchoverRequest = r
					json.NewDecoder(r.Body).Decode(&capturedSwitchoverBody)
					switchoverCalled = true
					w.WriteHeader(200)
					w.Write([]byte(`Successfully switched over to "new"`))
				}
			}),
		)
		defer server.Close()

		mockClient := createMockKubeClient()
		httpClient := HTTPClient{
			kubeClient: mockClient,
			client: instanceClient{
				baseUrl:    server.URL,
				httpClient: server.Client(),
				logger:     logr.Discard(),
			},
			logger: logr.Discard(),
		}

		success, err := httpClient.SwitchoverAndWait(context.Background(), "new")

		assert.NilError(t, err)
		assert.Assert(t, success)
		assert.Assert(t, switchoverCalled)
		assert.Equal(t, capturedSwitchoverRequest.Method, "POST")
		assert.DeepEqual(t, capturedSwitchoverBody, map[string]any{
			"leader":    "current-leader",
			"candidate": "new",
		})
	})

	// When this happens we need to perform a failover
	t.Run("NoLeader", func(t *testing.T) {
		server := httptest.NewTLSServer(
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/cluster") {
					// Mock cluster with no leader
					response := map[string]any{
						"members": []map[string]any{
							{
								"name":  "replica-1",
								"role":  "replica",
								"state": "running",
							},
						},
					}
					w.Header().Set("Content-Type", "application/json")
					json.NewEncoder(w).Encode(response)
				}
			}),
		)
		defer server.Close()

		mockClient := createMockKubeClient()
		httpClient := HTTPClient{
			kubeClient: mockClient,
			client: instanceClient{
				baseUrl:    server.URL,
				httpClient: server.Client(),
				logger:     logr.Discard(),
			},
			logger: logr.Discard(),
		}

		success, err := httpClient.SwitchoverAndWait(context.Background(), "new")

		assert.Assert(t, err != nil)
		assert.Assert(t, !success)
		assert.Assert(t, strings.Contains(err.Error(), "failed to detect current leader"))
	})
}

func TestHTTPClientFailoverAndWait(t *testing.T) {
	t.Run("Arguments", func(t *testing.T) {
		var capturedRequest *http.Request
		var capturedBody map[string]any

		server := httptest.NewTLSServer(
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				capturedRequest = r
				json.NewDecoder(r.Body).Decode(&capturedBody)
				w.WriteHeader(200)
				w.Write([]byte(`Successfully failed over to "new"`))
			}),
		)
		defer server.Close()

		mockClient := createMockKubeClient()
		httpClient := HTTPClient{
			kubeClient: mockClient,
			client: instanceClient{
				baseUrl:    server.URL,
				httpClient: server.Client(),
				logger:     logr.Discard(),
			},
			logger: logr.Discard(),
		}

		success, err := httpClient.FailoverAndWait(context.Background(), "new")

		assert.NilError(t, err)
		assert.Assert(t, success)
		assert.Equal(t, capturedRequest.Method, "POST")
		assert.Assert(t, strings.HasSuffix(capturedRequest.URL.Path, "/failover"))
		assert.DeepEqual(t, capturedBody, map[string]any{
			"candidate": "new",
		})
	})

	t.Run("Error", func(t *testing.T) {
		server := httptest.NewTLSServer(
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(500)
				w.Write([]byte(`Failover failed: no healthy replicas available`))
			}),
		)
		defer server.Close()

		mockClient := createMockKubeClient()
		httpClient := HTTPClient{
			kubeClient: mockClient,
			client: instanceClient{
				baseUrl:    server.URL,
				httpClient: server.Client(),
				logger:     logr.Discard(),
			},
			logger: logr.Discard(),
		}

		success, err := httpClient.FailoverAndWait(context.Background(), "new")

		assert.Assert(t, err != nil)
		assert.Assert(t, !success)
		assert.Assert(t, strings.Contains(err.Error(), "failover failed with status 500"))
	})

	t.Run("EmptyCandidate", func(t *testing.T) {
		mockClient := createMockKubeClient()
		httpClient := HTTPClient{
			kubeClient: mockClient,
			client: instanceClient{
				logger: logr.Discard(),
			},
			logger: logr.Discard(),
		}

		success, err := httpClient.FailoverAndWait(context.Background(), "")

		assert.Assert(t, err != nil)
		assert.Assert(t, !success)
		assert.Assert(t, strings.Contains(err.Error(), "failover requires a specific candidate"))
	})

	// Same as switchover. If we failover but to a different candidate, the
	// operator expects success = false.
	t.Run("DifferentCandidate", func(t *testing.T) {
		server := httptest.NewTLSServer(
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(200)
				// Patroni chose different candidate - no "Successfully"
				w.Write([]byte(`Failed over to "different-node" instead of "new"`))
			}),
		)
		defer server.Close()

		mockClient := createMockKubeClient()
		httpClient := HTTPClient{
			kubeClient: mockClient,
			client: instanceClient{
				baseUrl:    server.URL,
				httpClient: server.Client(),
				logger:     logr.Discard(),
			},
			logger: logr.Discard(),
		}

		success, err := httpClient.FailoverAndWait(context.Background(), "new")

		assert.NilError(t, err)    // No error, but success is false
		assert.Assert(t, !success) // Should return false like CLI does
	})
}

func TestHTTPClientReplaceConfiguration(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		var capturedRequest *http.Request
		var capturedBody map[string]any

		server := httptest.NewTLSServer(
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				capturedRequest = r
				json.NewDecoder(r.Body).Decode(&capturedBody)
				w.WriteHeader(200)
				// Patroni returns the updated configuration as JSON
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(capturedBody)
			}),
		)
		defer server.Close()

		mockClient := createMockKubeClient()
		httpClient := HTTPClient{
			kubeClient: mockClient,
			client: instanceClient{
				baseUrl:    server.URL,
				httpClient: server.Client(),
				logger:     logr.Discard(),
			},
			logger: logr.Discard(),
		}

		config := map[string]any{"some": "values"}
		err := httpClient.ReplaceConfiguration(context.Background(), config)

		assert.NilError(t, err)
		assert.Equal(t, capturedRequest.Method, "PUT")
		assert.Assert(t, strings.HasSuffix(capturedRequest.URL.Path, "/config"))
		assert.Equal(t, capturedRequest.Header.Get("Content-Type"), "application/json")
		assert.DeepEqual(t, capturedBody, config)
	})

	t.Run("Error", func(t *testing.T) {
		server := httptest.NewTLSServer(
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(400)
				w.Write([]byte(`Invalid configuration: postgresql.parameters.max_connections must be integer`))
			}),
		)
		defer server.Close()

		mockClient := createMockKubeClient()
		httpClient := HTTPClient{
			kubeClient: mockClient,
			client: instanceClient{
				baseUrl:    server.URL,
				httpClient: server.Client(),
				logger:     logr.Discard(),
			},
			logger: logr.Discard(),
		}

		err := httpClient.ReplaceConfiguration(
			context.Background(),
			map[string]any{"some": "values"},
		)

		assert.Assert(t, err != nil)
		assert.Assert(t, strings.Contains(err.Error(), "HTTP request failed with status 400"))
	})
}

func TestHTTPClientGetTimeline(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		server := httptest.NewTLSServer(
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/cluster") {
					response := map[string]any{
						"members": []map[string]any{
							{
								"name":     "leader-pod",
								"role":     "leader",
								"state":    "running",
								"timeline": int64(4),
							},
							{
								"name":     "replica-pod",
								"role":     "replica",
								"state":    "running",
								"timeline": int64(4),
							},
						},
					}
					w.Header().Set("Content-Type", "application/json")
					json.NewEncoder(w).Encode(response)
				}
			}),
		)
		defer server.Close()

		mockClient := createMockKubeClient()
		httpClient := HTTPClient{
			kubeClient: mockClient,
			client: instanceClient{
				baseUrl:    server.URL,
				httpClient: server.Client(),
				logger:     logr.Discard(),
			},
			logger: logr.Discard(),
		}

		timeline, err := httpClient.GetTimeline(context.Background())

		assert.NilError(t, err)
		assert.Equal(t, timeline, int64(4))
	})

	t.Run("NoLeader", func(t *testing.T) {
		server := httptest.NewTLSServer(
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/cluster") {
					response := map[string]any{
						"members": []map[string]any{
							{
								"name":     "replica-pod",
								"role":     "replica",
								"state":    "running",
								"timeline": int64(4),
							},
						},
					}
					w.Header().Set("Content-Type", "application/json")
					json.NewEncoder(w).Encode(response)
				}
			}),
		)
		defer server.Close()

		mockClient := createMockKubeClient()
		httpClient := HTTPClient{
			kubeClient: mockClient,
			client: instanceClient{
				baseUrl:    server.URL,
				httpClient: server.Client(),
				logger:     logr.Discard(),
			},
			logger: logr.Discard(),
		}

		timeline, err := httpClient.GetTimeline(context.Background())

		assert.NilError(t, err)
		assert.Equal(t, timeline, int64(0)) // Should return 0 when no leader
	})

	t.Run("LeaderNotRunning", func(t *testing.T) {
		server := httptest.NewTLSServer(
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/cluster") {
					response := map[string]any{
						"members": []map[string]any{
							{
								"name":     "leader-pod",
								"role":     "leader",
								"state":    "stopped", // Not running
								"timeline": int64(4),
							},
						},
					}
					w.Header().Set("Content-Type", "application/json")
					json.NewEncoder(w).Encode(response)
				}
			}),
		)
		defer server.Close()

		mockClient := createMockKubeClient()
		httpClient := HTTPClient{
			kubeClient: mockClient,
			client: instanceClient{
				baseUrl:    server.URL,
				httpClient: server.Client(),
				logger:     logr.Discard(),
			},
			logger: logr.Discard(),
		}

		timeline, err := httpClient.GetTimeline(context.Background())

		assert.NilError(t, err)
		assert.Equal(t, timeline, int64(0)) // Should return 0 when leader not running
	})
}

func TestExtractMetadataFromPodName(t *testing.T) {
	t.Run("ValidPodName", func(t *testing.T) {
		metadata, err := extractMetadataFromPodName("pgdb-609qv5o187x841r2-instance1-h8q2-0")

		assert.NilError(t, err)
		assert.Equal(t, metadata.Name, "pgdb-609qv5o187x841r2-instance1-h8q2-0")
		assert.Equal(t, metadata.Namespace, "pgdb-609qv5o187x841r2")
		assert.Equal(t, metadata.InstanceName, "pgdb-609qv5o187x841r2-instance1-h8q2")
	})

	t.Run("InvalidPodName", func(t *testing.T) {
		_, err := extractMetadataFromPodName("invalid-pod-name")

		assert.Assert(t, err != nil)
		assert.Assert(t, strings.Contains(err.Error(), "unexpected format"))
	})
}
