/*
Copyright 2023.

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

package k0smotronio

import (
	"context"
	"crypto/x509"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	bootstrapv1 "sigs.k8s.io/cluster-api/api/bootstrap/kubeadm/v1beta2"
	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/certs"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/cloudflare/cfssl/cli/genkey"
	"github.com/cloudflare/cfssl/csr"
	"github.com/cloudflare/cfssl/helpers"
	"github.com/cloudflare/cfssl/signer"
	"github.com/cloudflare/cfssl/signer/local"
	"sigs.k8s.io/cluster-api/util/secret"

	km "github.com/k0sproject/k0smotron/v2/api/k0smotron.io/v1beta2"
	kcerts "github.com/k0sproject/k0smotron/v2/internal/certs"
	kutil "github.com/k0sproject/k0smotron/v2/internal/controller/util"
	"github.com/k0sproject/k0smotron/v2/internal/metrics"
)

// certificateSettings resolves the effective certificate duration and renewal
// threshold for a cluster, falling back to the package defaults.
func certificateSettings(kmc *km.Cluster) (time.Duration, time.Duration) {
	duration := kcerts.DefaultDuration
	renewBefore := kcerts.DefaultRenewBefore

	if c := kmc.Spec.Certificates; c != nil {
		if c.Duration != nil && c.Duration.Duration > 0 {
			duration = c.Duration.Duration
		}
		if c.RenewBefore != nil && c.RenewBefore.Duration > 0 {
			renewBefore = c.RenewBefore.Duration
		}
	}

	return duration, renewBefore
}

// shouldRenew reports whether a certificate must be re-signed now, either
// because it entered its renewal window or because the operator asked for it
// explicitly via the renew annotation.
func shouldRenew(kmc *km.Cluster, i kcerts.Info, renewBefore time.Duration, now time.Time) bool {
	if _, forced := kmc.Annotations[km.RenewCertificatesAnnotation]; forced {
		return true
	}
	return kcerts.NeedsRenewal(i, renewBefore, now)
}

// isManagedSecret reports whether a certificate secret was created by
// k0smotron for this cluster. Certificates the user supplied through
// spec.certificateRefs are reported but never rewritten: overwriting a
// certificate the operator brought themselves would destroy their key material.
//
// The owner is not always the Cluster itself. When the hosted control plane
// runs on a remote host cluster, ensureEtcdCertificates stamps the external
// owner (a ConfigMap named "<cluster>-root-owner", see
// kutil.GetExternalControllerRef) instead, whose UID never equals kmc.UID.
// Checking only kmc.UID would classify our own certificates as user-supplied
// and skip renewal forever — silently reproducing the very expiry bug this
// work exists to fix.
func isManagedSecret(s *corev1.Secret, kmc *km.Cluster, externalOwner metav1.Object) bool {
	if s == nil {
		// No secret recorded means this is a first issuance we are about to
		// create ourselves.
		return true
	}

	for _, ref := range s.OwnerReferences {
		if ref.UID == kmc.UID {
			return true
		}
		if externalOwner != nil && ref.UID == externalOwner.GetUID() {
			return true
		}
	}

	return false
}

// signOne produces a fresh keypair for one purpose.
func signOne(g *csr.Generator, signr signer.Signer, cn string, hosts []string, org string) ([]byte, []byte, error) {
	req := csr.CertificateRequest{
		KeyRequest: csr.NewKeyRequest(),
		CN:         cn,
		Names:      []csr.Name{{O: org}},
		Hosts:      hosts,
	}
	req.KeyRequest.A = "rsa"
	req.KeyRequest.S = 2048

	csrBytes, key, err := g.ProcessRequest(&req)
	if err != nil {
		return nil, nil, fmt.Errorf("error processing csr: %w", err)
	}

	crt, err := signr.Sign(signer.SignRequest{Request: string(csrBytes)})
	if err != nil {
		return nil, nil, fmt.Errorf("error signing csr: %w", err)
	}

	return crt, key, nil
}

// inspectKeyPair reads expiry metadata straight from a PEM certificate.
func inspectKeyPair(purpose string, crt []byte) (kcerts.Info, error) {
	s := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{kcerts.PurposeLabel: purpose}},
		Data:       map[string][]byte{kcerts.TLSCrtDataName: crt},
	}

	infos, err := kcerts.Inspect(s)
	if err != nil {
		return kcerts.Info{}, err
	}

	return infos[0], nil
}

// signLeaves generates or renews a set of leaf certificates signed by caCert.
// It returns the inspection results for every leaf, whether it was re-signed or
// left alone, so the caller can report expiry and compute the rollout
// fingerprint.
func (scope *kmcScope) signLeaves(
	ctx context.Context,
	kmc *km.Cluster,
	caCertPEM, caKeyPEM []byte,
	leaves secret.Certificates,
	hosts func(purpose string) []string,
	org func(purpose string) string,
	owner metav1.OwnerReference,
) ([]kcerts.Info, error) {
	caCert, err := helpers.ParseCertificatePEM(caCertPEM)
	if err != nil {
		return nil, fmt.Errorf("error parsing CA certificate: %w", err)
	}

	caPrivKey, err := helpers.ParsePrivateKeyPEM(caKeyPEM)
	if err != nil {
		return nil, fmt.Errorf("error parsing CA private key: %w", err)
	}

	duration, renewBefore := certificateSettings(kmc)
	now := time.Now()

	// An explicit policy: a nil policy makes cfssl fall back to its own
	// DefaultConfig, whose expiry is a library detail rather than our decision.
	policy := kcerts.SigningPolicy(duration, caCert.NotAfter, now)

	signr, err := local.NewSigner(caPrivKey, caCert, x509.SHA256WithRSA, policy)
	if err != nil {
		return nil, fmt.Errorf("error creating signer: %w", err)
	}

	if err := leaves.LookupCached(ctx, scope.secretCachingClient, scope.client, util.ObjectKey(kmc)); err != nil {
		return nil, fmt.Errorf("error looking up certs: %w", err)
	}

	g := &csr.Generator{Validator: genkey.Validator}
	infos := make([]kcerts.Info, 0, len(leaves))
	var renewalErrs []error
	var issuedPurposes []string

	// Renewal errors are reported, never returned: they must not abort the
	// rest of the reconcile loop. A defer drains them on every exit path —
	// including an early, fatal return from a later first-issuance failure —
	// so an earlier renewal error is never silently discarded.
	defer func() {
		for _, err := range renewalErrs {
			log.FromContext(ctx).Error(err, "Certificate renewal failed, will retry on next reconcile")
			scope.currentReconcileState.certificateRenewalErrors =
				append(scope.currentReconcileState.certificateRenewalErrors, err.Error())
		}
	}()

	for _, c := range leaves {
		purpose := string(c.Purpose)

		// Existing certificate: inspect it, and renew only if it is due.
		if c.KeyPair != nil {
			info, err := inspectKeyPair(purpose, c.KeyPair.Cert)
			if err != nil {
				// An unreadable certificate is reported, never overwritten: it
				// may be a user-supplied certificate we must not destroy.
				// Renewal is skipped, reconciliation continues.
				scope.currentReconcileState.certificatesUnparseable =
					append(scope.currentReconcileState.certificatesUnparseable, purpose)
				continue
			}

			if !isManagedSecret(c.Secret, kmc, scope.externalOwner) {
				// Supplied by the user via spec.certificateRefs. Reported, never
				// rewritten — rotating it is the user's decision, not ours.
				infos = append(infos, info)
				continue
			}

			if !shouldRenew(kmc, info, renewBefore, now) {
				infos = append(infos, info)
				continue
			}

			crt, keyPEM, err := signOne(g, signr, purpose, hosts(purpose), org(purpose))
			if err != nil {
				// A renewal failure must not break the rest of reconciliation:
				// the existing certificate is still valid until its expiry, and
				// the condition plus the counter surface the problem.
				metrics.RecordRenewal(kmc.Namespace, kmc.Name, purpose, metrics.RenewalResultError)
				renewalErrs = append(renewalErrs, fmt.Errorf("renewing %q: %w", purpose, err))
				infos = append(infos, info)
				continue
			}

			// cluster-api's SaveGenerated only issues a Create, so renewal must
			// update the existing secret in place.
			secretKey := client.ObjectKey{Namespace: kmc.Namespace, Name: secret.Name(kmc.Name, c.Purpose)}
			if err := kcerts.SaveRenewed(ctx, scope.client, secretKey, crt, keyPEM); err != nil {
				metrics.RecordRenewal(kmc.Namespace, kmc.Name, purpose, metrics.RenewalResultError)
				renewalErrs = append(renewalErrs, fmt.Errorf("saving renewed %q: %w", purpose, err))
				infos = append(infos, info)
				continue
			}

			c.KeyPair = &certs.KeyPair{Cert: crt, Key: keyPEM}
			metrics.RecordRenewal(kmc.Namespace, kmc.Name, purpose, metrics.RenewalResultSuccess)
			scope.currentReconcileState.certificatesRenewed = true

			renewed, err := inspectKeyPair(purpose, crt)
			if err != nil {
				// The secret write already succeeded: the certificate WAS
				// renewed, we merely failed to read back its own metadata.
				// That must not abort reconciliation any more than a renewal
				// failure does — it is recorded the same way.
				renewalErrs = append(renewalErrs, fmt.Errorf("inspecting freshly renewed certificate %q: %w", purpose, err))
				continue
			}
			infos = append(infos, renewed)
			continue
		}

		// First issuance. Unlike renewal this IS fatal: without the certificate
		// the etcd and control plane StatefulSets cannot start at all.
		crt, keyPEM, err := signOne(g, signr, purpose, hosts(purpose), org(purpose))
		if err != nil {
			metrics.RecordRenewal(kmc.Namespace, kmc.Name, purpose, metrics.RenewalResultError)
			return infos, fmt.Errorf("signing certificate %q: %w", purpose, err)
		}

		c.Generated = true
		c.KeyPair = &certs.KeyPair{Cert: crt, Key: keyPEM}
		// Success is not counted yet: SaveGenerated below is what actually
		// persists this certificate, and it can still fail. Counting here
		// would claim success before the write is known to have happened.
		issuedPurposes = append(issuedPurposes, purpose)

		issued, err := inspectKeyPair(purpose, crt)
		if err != nil {
			return infos, fmt.Errorf("inspecting freshly signed certificate %q: %w", purpose, err)
		}
		infos = append(infos, issued)
	}

	if err := leaves.SaveGenerated(ctx, scope.client, util.ObjectKey(kmc), owner); err != nil {
		for _, purpose := range issuedPurposes {
			metrics.RecordRenewal(kmc.Namespace, kmc.Name, purpose, metrics.RenewalResultError)
		}
		return infos, err
	}

	for _, purpose := range issuedPurposes {
		metrics.RecordRenewal(kmc.Namespace, kmc.Name, purpose, metrics.RenewalResultSuccess)
	}

	if len(issuedPurposes) > 0 {
		// First issuance counts too: a certificate appearing for the first time on
		// an existing cluster (e.g. apiserver-etcd-client when storage changes)
		// needs the consuming pods to pick it up.
		scope.currentReconcileState.certificatesRenewed = true
	}

	return infos, nil
}

func (scope *kmcScope) ensureEtcdCertificates(ctx context.Context, kmc *km.Cluster) ([]kcerts.Info, error) {
	certificates := secret.NewCertificatesForInitialControlPlane(&bootstrapv1.ClusterConfiguration{})
	if err := certificates.LookupCached(ctx, scope.secretCachingClient, scope.client, util.ObjectKey(kmc)); err != nil {
		return nil, fmt.Errorf("error looking up etcd certs: %w", err)
	}

	etcdCACert := certificates.GetByPurpose(secret.EtcdCA)
	if etcdCACert.KeyPair == nil || len(etcdCACert.KeyPair.Cert) == 0 {
		return nil, fmt.Errorf("etcd CA certificate not found")
	}

	svc := kmc.GetEtcdServiceName()
	ns := kmc.GetNamespace()
	etcdHosts := []string{
		"127.0.0.1",
		"localhost",
		svc,
		fmt.Sprintf("%s.%s.svc", svc, ns),
		fmt.Sprintf("%s.%s.svc.cluster.local", svc, ns),
		fmt.Sprintf("*.%s", svc),
		fmt.Sprintf("*.%s.%s.svc", svc, ns),
		fmt.Sprintf("*.%s.%s.svc.cluster.local", svc, ns),
	}

	owner := *metav1.NewControllerRef(kmc, km.GroupVersion.WithKind("Cluster"))
	if scope.externalOwner != nil {
		owner = *kutil.GetExternalControllerRef(scope.externalOwner)
	}

	return scope.signLeaves(ctx, kmc,
		etcdCACert.KeyPair.Cert, etcdCACert.KeyPair.Key,
		secret.Certificates{
			&secret.Certificate{Purpose: "apiserver-etcd-client"},
			&secret.Certificate{Purpose: "etcd-server"},
			&secret.Certificate{Purpose: "etcd-peer"},
		},
		func(string) []string { return etcdHosts },
		func(purpose string) string { return purpose },
		owner,
	)
}

func (scope *kmcScope) ensureHAProxyCerts(ctx context.Context, kmc *km.Cluster) ([]kcerts.Info, error) {
	certificates := secret.NewCertificatesForInitialControlPlane(&bootstrapv1.ClusterConfiguration{})
	if err := certificates.LookupCached(ctx, scope.secretCachingClient, scope.client, util.ObjectKey(kmc)); err != nil {
		return nil, fmt.Errorf("error looking up cluster certs: %w", err)
	}

	clusterCACert := certificates.GetByPurpose(secret.ClusterCA)
	if clusterCACert.KeyPair == nil || len(clusterCACert.KeyPair.Cert) == 0 {
		return nil, fmt.Errorf("cluster CA certificate not found")
	}

	hosts := []string{
		"kubernetes",
		"kubernetes.default",
		"kubernetes.default.svc",
		"kubernetes.default.svc.cluster",
		"kubernetes.default.svc." + scope.clusterSettings.clusterDomain,
		"kubernetes.svc." + scope.clusterSettings.clusterDomain,
		"localhost",
		"127.0.0.1",
		scope.clusterSettings.kubernetesServiceIP,
	}

	return scope.signLeaves(ctx, kmc,
		clusterCACert.KeyPair.Cert, clusterCACert.KeyPair.Key,
		secret.Certificates{&secret.Certificate{Purpose: "ingress-haproxy"}},
		func(string) []string { return hosts },
		func(string) string { return "kubernetes" },
		*metav1.NewControllerRef(kmc, km.GroupVersion.WithKind("Cluster")),
	)
}
