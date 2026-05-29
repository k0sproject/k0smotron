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

package v1beta1

import (
	"fmt"

	"github.com/k0sproject/k0smotron/api/k0smotron.io/v1beta2"
	v2 "github.com/k0sproject/k0smotron/api/k0smotron.io/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/conversion"
)

var _ conversion.Convertible = &JoinTokenRequest{}

// ConvertTo converts this JoinTokenRequest (v1beta1) to the hub version (v1beta2).
func (jtr *JoinTokenRequest) ConvertTo(dstRaw conversion.Hub) error {
	dst, ok := dstRaw.(*v2.JoinTokenRequest)
	if !ok {
		return fmt.Errorf("expected *v2.JoinTokenRequest, got %T", dstRaw)
	}

	dst.ObjectMeta = jtr.ObjectMeta

	dst.Spec = v1beta2.JoinTokenRequestSpec{
		ClusterName: jtr.Spec.ClusterRef.Name,
		Expiry:      jtr.Spec.Expiry,
		Role:        jtr.Spec.Role,
	}

	if jtr.Spec.ClusterRef.Namespace != "" {
		if dst.Annotations == nil {
			dst.Annotations = make(map[string]string)
		}
		dst.Annotations[v2.V1Beta1ClusterRefNamespaceAnnotation] = jtr.Spec.ClusterRef.Namespace
	}

	dst.Status.TokenID = jtr.Status.TokenID
	dst.SetDeprecatedStatus(jtr.Status.ReconciliationStatus, jtr.Status.ClusterUID)

	return nil
}

// ConvertFrom converts from the hub version (v1beta2) to this JoinTokenRequest (v1beta1).
// Conditions have no equivalent in v1beta1 and are silently dropped.
func (jtr *JoinTokenRequest) ConvertFrom(srcRaw conversion.Hub) error {
	src, ok := srcRaw.(*v2.JoinTokenRequest)
	if !ok {
		return fmt.Errorf("expected *v2.JoinTokenRequest, got %T", srcRaw)
	}

	jtr.ObjectMeta = src.ObjectMeta

	clusterNamespace := ""
	if ns, ok := src.Annotations[v2.V1Beta1ClusterRefNamespaceAnnotation]; ok {
		clusterNamespace = ns
	}

	jtr.Spec = JoinTokenRequestSpec{
		ClusterRef: ClusterRef{
			Name:      src.Spec.ClusterName,
			Namespace: clusterNamespace,
		},
		Expiry: src.Spec.Expiry,
		Role:   src.Spec.Role,
	}

	jtr.Status.TokenID = src.Status.TokenID
	jtr.Status.ClusterUID = src.GetDeprecatedClusterUUID()
	jtr.Status.ReconciliationStatus = src.GetDeprecatedReconciliationStatus()

	return nil
}
