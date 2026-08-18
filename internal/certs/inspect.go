/*
Copyright 2026.

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

// Package certs holds the certificate policy, inspection and renewal helpers
// shared by the k0smotron controllers. It has no controller dependencies so
// that every rule about certificate lifetime lives in exactly one place.
package certs

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/cloudflare/cfssl/helpers"
	corev1 "k8s.io/api/core/v1"
)

const (
	// TLSCrtDataName is the secret key holding the PEM-encoded certificate,
	// matching the layout produced by cluster-api's util/secret package.
	TLSCrtDataName = "tls.crt"

	// PurposeLabel carries the cluster-api certificate purpose. A secret name
	// alone is ambiguous, because cluster names may contain dashes, so callers
	// that already know the purpose set this label before inspecting.
	PurposeLabel = "k0smotron.io/certificate-purpose"
)

// Info is the subset of a certificate k0smotron makes decisions on.
type Info struct {
	// Purpose is the cluster-api certificate purpose, e.g. "etcd-server".
	Purpose string
	// NotAfter is the certificate expiry.
	NotAfter time.Time
	// Serial is the certificate serial number in lower-case hex.
	Serial string
	// Issuer is the common name of the signing CA.
	Issuer string
}

// Inspect parses the certificate stored in a cluster-api format secret.
// It returns an error rather than a zero value when the secret cannot be
// parsed, so that callers can report the certificate as unknown instead of
// silently treating it as healthy.
//
// When tls.crt holds a certificate chain (leaf followed by intermediate/CA
// certificates, a normal convention for user-supplied secretRefs), Inspect
// reports on the leaf, which is the first certificate in the PEM by that same
// convention.
func Inspect(s *corev1.Secret) ([]Info, error) {
	if s == nil {
		return nil, fmt.Errorf("secret is nil")
	}

	raw, ok := s.Data[TLSCrtDataName]
	if !ok || len(raw) == 0 {
		return nil, fmt.Errorf("secret %s/%s has no %q key", s.Namespace, s.Name, TLSCrtDataName)
	}

	certs, err := helpers.ParseCertificatesPEM(raw)
	if err != nil {
		return nil, fmt.Errorf("parsing certificate in secret %s/%s: %w", s.Namespace, s.Name, err)
	}
	if len(certs) == 0 {
		return nil, fmt.Errorf("secret %s/%s %q contains no certificates", s.Namespace, s.Name, TLSCrtDataName)
	}
	cert := certs[0]

	purpose := s.Labels[PurposeLabel]
	if purpose == "" {
		purpose = s.Name
	}

	return []Info{{
		Purpose:  purpose,
		NotAfter: cert.NotAfter,
		Serial:   strings.ToLower(cert.SerialNumber.Text(16)),
		Issuer:   cert.Issuer.CommonName,
	}}, nil
}

// NeedsRenewal reports whether a certificate is within renewBefore of expiry, or
// already expired.
func NeedsRenewal(i Info, renewBefore time.Duration, now time.Time) bool {
	return !now.Before(i.NotAfter.Add(-renewBefore))
}

// EarliestRenewal returns the earliest moment at which any of the given
// certificates becomes due for renewal. It returns the zero time when there is
// nothing to schedule.
func EarliestRenewal(is []Info, renewBefore time.Duration) time.Time {
	var earliest time.Time
	for _, i := range is {
		due := i.NotAfter.Add(-renewBefore)
		if earliest.IsZero() || due.Before(earliest) {
			earliest = due
		}
	}
	return earliest
}

// Fingerprint is a stable hash over the certificate serials. It is stamped onto
// a pod template so that re-signing a certificate rolls the pods that mount it.
// The result is independent of the order of the input.
func Fingerprint(is []Info) string {
	if len(is) == 0 {
		return ""
	}

	parts := make([]string, 0, len(is))
	for _, i := range is {
		parts = append(parts, i.Purpose+"="+i.Serial)
	}
	sort.Strings(parts)

	sum := sha256.Sum256([]byte(strings.Join(parts, ",")))
	return hex.EncodeToString(sum[:])[:16]
}
