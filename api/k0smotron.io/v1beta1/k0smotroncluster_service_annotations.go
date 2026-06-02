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
	"encoding/json"

	v1 "k8s.io/api/core/v1"
)

// GetServiceLabels returns the extra labels to apply to the control-plane Service.
// These are stored in the Cluster object annotations during v1beta1→v1beta2 conversion.
func GetServiceLabels(annotations map[string]string) map[string]string {
	return getMapAnnotation(annotations, ServiceAnnotationLabels)
}

// GetServiceAnnotations returns the extra annotations to apply to the control-plane Service.
func GetServiceAnnotations(annotations map[string]string) map[string]string {
	return getMapAnnotation(annotations, ServiceAnnotationAnnotations)
}

// GetServiceExternalTrafficPolicy returns the ExternalTrafficPolicy stored in annotations, if any.
func GetServiceExternalTrafficPolicy(annotations map[string]string) v1.ServiceExternalTrafficPolicyType {
	return v1.ServiceExternalTrafficPolicyType(getStringAnnotation(annotations, ServiceAnnotationExternalTrafficPolicy))
}

// GetServiceLoadBalancerClass returns the LoadBalancerClass stored in annotations, if any.
func GetServiceLoadBalancerClass(annotations map[string]string) *string {
	s := getStringAnnotation(annotations, ServiceAnnotationLoadBalancerClass)
	if s == "" {
		return nil
	}
	return &s
}

func getStringAnnotation(annotations map[string]string, key string) string {
	val, ok := annotations[key]
	if !ok || val == "" {
		return ""
	}
	var s string
	if err := json.Unmarshal([]byte(val), &s); err != nil {
		return ""
	}
	return s
}

func getMapAnnotation(annotations map[string]string, key string) map[string]string {
	val, ok := annotations[key]
	if !ok || val == "" {
		return nil
	}
	var m map[string]string
	if err := json.Unmarshal([]byte(val), &m); err != nil {
		return nil
	}
	return m
}
