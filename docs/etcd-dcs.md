# External etcd DCS for Patroni

By default, the operator uses Kubernetes Endpoints as the Patroni distributed configuration store (DCS). Clusters on managed Kubernetes platforms where the control plane API is not accessible from within the cluster, or clusters requiring an HA DCS independent of the Kubernetes API, can use an external etcd cluster instead.

## Configuration

Set `spec.patroni.dcs.type: etcd` and provide at least one endpoint:

```yaml
spec:
  patroni:
    dcs:
      type: etcd
      etcd:
        endpoints:
          - https://etcd.etcd-cluster.svc:2379
```

### Field reference

| Field | Type | Required | Description |
|---|---|---|---|
| `dcs.type` | `kubernetes` \| `etcd` | No (default: `kubernetes`) | DCS backend. **Immutable after cluster creation.** |
| `dcs.etcd.endpoints` | `[]string` | Yes (when type is etcd) | etcd endpoint URLs including scheme and port. All endpoints must use the same scheme (`http://` or `https://`). |
| `dcs.etcd.tlsSecret` | `string` | No | Name of a Secret in the same namespace containing TLS client credentials. |
| `dcs.etcd.authSecret` | `string` | No | Name of a Secret in the same namespace containing etcd username/password. |

## Secrets

### TLS secret (`tlsSecret`)

Required when the etcd cluster uses TLS. The Secret must contain exactly these keys:

```
ca.crt   — PEM-encoded CA certificate that signed the etcd server certificate
tls.crt  — PEM-encoded client certificate (for mutual TLS)
tls.key  — PEM-encoded private key for the client certificate
```

Example:

```bash
kubectl create secret generic etcd-tls \
  --from-file=ca.crt=ca.pem \
  --from-file=tls.crt=client.pem \
  --from-file=tls.key=client-key.pem
```

Reference it in the CR:

```yaml
spec:
  patroni:
    dcs:
      type: etcd
      etcd:
        endpoints:
          - https://etcd.etcd-cluster.svc:2379
        tlsSecret: etcd-tls
```

The secret is mounted read-only at `/etc/patroni/etcd-tls/` inside each PostgreSQL pod.

### Auth secret (`authSecret`)

Required when the etcd cluster uses username/password authentication. The Secret must contain exactly these keys:

```
username — etcd username
password — etcd password
```

Example:

```bash
kubectl create secret generic etcd-auth \
  --from-literal=username=patroni \
  --from-literal=password=supersecret
```

Reference it in the CR:

```yaml
spec:
  patroni:
    dcs:
      type: etcd
      etcd:
        endpoints:
          - https://etcd.etcd-cluster.svc:2379
        authSecret: etcd-auth
```

Credentials are injected as `PATRONI_ETCD3_USERNAME` and `PATRONI_ETCD3_PASSWORD` environment variables in each PostgreSQL pod. They are **not** written to any ConfigMap.

## Secret rotation

**Rotating TLS certificates or auth credentials requires a rolling restart of all PostgreSQL pods.** Patroni loads DCS credentials and TLS mounts only at startup. It does not watch or dynamically reload environment variables or volume mounts after the process starts.

To rotate credentials:

1. Update the Secret with new values:
   ```bash
   kubectl create secret generic etcd-auth \
     --from-literal=username=patroni \
     --from-literal=password=newsecret \
     --dry-run=client -o yaml | kubectl apply -f -
   ```

2. Trigger a rolling restart of each instance StatefulSet:
   ```bash
   kubectl rollout restart statefulset/<cluster-name>-instance1
   ```

   Repeat for each instance set in the cluster.

## Immutability

`spec.patroni.dcs.type` is immutable after cluster creation. The admission webhook rejects updates that attempt to change this field. Switching DCS backends requires deleting and recreating the cluster.

## Validation

The operator validates the etcd configuration at both admission time and during each reconcile:

- **Admission**: CEL rules reject empty endpoints when `type: etcd`, mixed `http://`/`https://` schemes in the same endpoint list, endpoints that do not match `^https?://`, and changes to `dcs.type` after creation.
- **Reconcile**: If `tlsSecret` or `authSecret` is set but the referenced Secret does not exist or is missing a required key, the operator emits a `Warning` event on the `PerconaPGCluster` object and requeues the reconcile. No PostgreSQL pods are affected until the secret is corrected.

Inspect events with:

```bash
kubectl describe perconapgcluster <cluster-name>
```
