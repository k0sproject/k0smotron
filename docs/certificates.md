# Certificates

k0smotron signs four leaf certificates for a hosted control plane and renews
them automatically before they expire.

## What k0smotron manages

| Certificate | Signed by | Renewed automatically | Expiry reported on |
|---|---|---|---|
| `ca`, `etcd`, `front-proxy` | Cluster API, 10 year validity | No | `K0sControlPlane` only |
| `etcd-server`, `etcd-peer`, `apiserver-etcd-client` | k0smotron | Yes | `Cluster`, mirrored onto `K0smotronControlPlane` |
| `ingress-haproxy` | k0smotron | Yes, with the limitation below | `Cluster`, mirrored onto `K0smotronControlPlane` |
| `<cluster>-kubeconfig` | Cluster API | Yes, independently | not reported |

For a hosted control plane, the CA certificates are **not** reported: the
`k0smotron.io/Cluster` resource reports only the leaves it signs itself, and the
`K0smotronControlPlane` mirrors those leaf-only conditions up from its child
`Cluster` once that child exists. Only a machine-based `K0sControlPlane` reports
CA expiry, because CAs are the only certificates visible to it from the
management cluster.

With `spec.storage.type: kine` and no ingress, k0smotron signs no leaf
certificates at all, so the `Cluster` reports `CertificatesAvailable: True` with
nothing to track.

## Configuring lifetime and renewal

```yaml
apiVersion: k0smotron.io/v1beta2
kind: Cluster
metadata:
  name: my-cluster
spec:
  certificates:
    duration: 8760h      # requested validity of certificates k0smotron signs
    renewBefore: 720h    # renew this long before expiry
```

`renewBefore` must be shorter than `duration`. A leaf certificate is
additionally clamped so that it never outlives the CA that issued it.

`spec.certificates` exists only in `k0smotron.io/v1beta2`. Reading and
re-applying a `Cluster` through the deprecated `v1beta1` API drops this field,
reverting to the defaults (`8760h`/`720h`). Use `v1beta2` if you have
configured non-default certificate lifetimes.

Renewal rewrites the certificate secret and stamps a new fingerprint on the
affected StatefulSet's pod template, which makes Kubernetes perform an ordered,
readiness-gated rolling update. Because the CA does not change, the old and new
certificates trust each other, so there is no window in which a renewed pod
cannot talk to a not-yet-renewed one.

To renew immediately without waiting for the threshold:

```bash
kubectl annotate cluster.k0smotron.io my-cluster k0smotron.io/renew-certificates=""
```

The controller removes the annotation once renewal completes.

Apply the annotation to the `k0smotron.io/Cluster` resource, **not** to the
`K0smotronControlPlane`: only `spec` is copied from the control plane down to the
child `Cluster`, so annotating the `K0smotronControlPlane` has no effect at all.

## Observing expiry

Two metrics are exported on the manager's metrics endpoint:

- `k0smotron_certificate_expiration_timestamp_seconds{namespace,cluster,kind,purpose}`
- `k0smotron_certificate_renewal_total{namespace,cluster,purpose,result}`

An alert on the first one:

```yaml
- alert: K0smotronCertificateExpiringSoon
  expr: k0smotron_certificate_expiration_timestamp_seconds - time() < 7 * 24 * 3600
  for: 1h
```

Two conditions are set on the cluster resource:

- `CertificatesAvailable` — `False` when a managed certificate has expired,
  `Unknown` when a certificate secret cannot be read.
- `CertificatesExpiring` — `True` when a certificate is inside its renewal
  window or already expired. Negative polarity: `True` means attention needed.
  `Unknown` when a certificate secret cannot be read, so an unreadable
  certificate is never reported as `Valid`.

## Limitations

### Workers provisioned before an ingress certificate renewal

The `ingress-haproxy` certificate is written into worker **bootstrap data**,
which is rendered once when a machine is provisioned. Renewing the certificate
updates newly provisioned and replaced workers; workers already running keep the
old certificate until they are rolled. A certificate renewal deliberately does
not trigger node replacement.

### k0s in-pod certificates on persistent storage

k0s generates its own certificates inside the control plane pod (apiserver
serving, konnectivity, `admin.conf`). With the default
`spec.persistence.type: emptyDir` these are discarded and regenerated on every
pod restart, so they never approach expiry.

With `spec.persistence.type: pvc` or `hostPath` they survive restarts and are
**not** renewed by k0smotron or by k0s: k0s only regenerates certificates whose
issuer common name is one it recognises, and Cluster API issues CAs under the
common name `kubernetes`. PVC persistence for hosted control planes is
deprecated; use the default `emptyDir`.
