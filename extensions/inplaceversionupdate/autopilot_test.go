//go:build extension

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

package inplaceversionupdate

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/kubernetes/fake"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"

	bootstrapv1 "github.com/k0sproject/k0smotron/v2/api/bootstrap/v1beta2"
)

func node(name string, labels map[string]string) *corev1.Node {
	return &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels}}
}

func TestResolveWorkerNode(t *testing.T) {
	clientset := fake.NewSimpleClientset(
		// node named after the host, not the Machine (useSystemHostname: true)
		node("dom836", map[string]string{bootstrapv1.MachineNameNodeLabel: "md-0-abcde"}),
		// node named after the Machine (hostname override)
		node("md-1-fghij", map[string]string{bootstrapv1.MachineNameNodeLabel: "md-1-fghij"}),
		node("dup-a", map[string]string{bootstrapv1.MachineNameNodeLabel: "md-2-klmno"}),
		node("dup-b", map[string]string{bootstrapv1.MachineNameNodeLabel: "md-2-klmno"}),
		node("unlabeled", nil),
	)

	nodeName, err := resolveWorkerNode(context.Background(), clientset, "md-0-abcde")
	require.NoError(t, err)
	assert.Equal(t, "dom836", nodeName)

	nodeName, err = resolveWorkerNode(context.Background(), clientset, "md-1-fghij")
	require.NoError(t, err)
	assert.Equal(t, "md-1-fghij", nodeName)

	_, err = resolveWorkerNode(context.Background(), clientset, "md-2-klmno")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "found 2")

	_, err = resolveWorkerNode(context.Background(), clientset, "unlabeled")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no node with label selector")
}

func planWithTarget(target string, discovery map[string]any) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"spec": map[string]any{
			"commands": []any{
				map[string]any{
					"k0supdate": map[string]any{
						"version": "v1.35.4+k0s.0",
						"targets": map[string]any{
							target: map[string]any{"discovery": discovery},
						},
					},
				},
			},
		},
	}}
}

func TestPlanTargetsMachine(t *testing.T) {
	worker := &clusterv1.Machine{ObjectMeta: metav1.ObjectMeta{Name: "md-0-abcde"}}
	controlPlane := &clusterv1.Machine{ObjectMeta: metav1.ObjectMeta{Name: "cp-0"}}

	selectorPlan := planWithTarget("workers", map[string]any{
		"selector": map[string]any{"labels": workerNodeSelector("md-0-abcde")},
	})
	legacyStaticWorkerPlan := planWithTarget("workers", map[string]any{
		"static": map[string]any{"nodes": []any{"md-0-abcde"}},
	})
	externalSelectorPlan := planWithTarget("workers", map[string]any{
		"selector": map[string]any{"labels": "kubernetes.io/os=linux, " + workerNodeSelector("md-0-abcde")},
	})
	otherWorkerPlan := planWithTarget("workers", map[string]any{
		"selector": map[string]any{"labels": workerNodeSelector("md-1-fghij")},
	})
	controlPlanePlan := planWithTarget("controllers", map[string]any{
		"static": map[string]any{"nodes": []any{"cp-0"}},
	})

	testCases := []struct {
		name           string
		plan           *unstructured.Unstructured
		machine        *clusterv1.Machine
		isControlPlane bool
		want           bool
	}{
		{name: "worker targeted by label selector", plan: selectorPlan, machine: worker, want: true},
		{name: "worker targeted by a selector with further labels", plan: externalSelectorPlan, machine: worker, want: true},
		{name: "worker targeted statically by a previous extension version", plan: legacyStaticWorkerPlan, machine: worker, want: true},
		{name: "worker not targeted", plan: otherWorkerPlan, machine: worker, want: false},
		{name: "control plane plan does not target worker", plan: controlPlanePlan, machine: worker, want: false},
		{name: "control plane targeted statically", plan: controlPlanePlan, machine: controlPlane, isControlPlane: true, want: true},
		{name: "control plane ignores worker selector", plan: selectorPlan, machine: controlPlane, isControlPlane: true, want: false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := planTargetsMachine(tc.plan, tc.machine, tc.isControlPlane)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}

	_, err := planTargetsMachine(&unstructured.Unstructured{Object: map[string]any{"spec": map[string]any{}}}, worker, false)
	assert.Error(t, err)
}

func TestSelectorTargetsMachine(t *testing.T) {
	testCases := []struct {
		name     string
		selector string
		want     bool
	}{
		{name: "empty selector", selector: "", want: false},
		{name: "machine-name label only", selector: "k0smotron.io/machine-name=md-0-abcde", want: true},
		{name: "double equals", selector: "k0smotron.io/machine-name==md-0-abcde", want: true},
		{name: "further labels", selector: "kubernetes.io/os=linux,k0smotron.io/machine-name=md-0-abcde", want: true},
		{name: "set of machines", selector: "k0smotron.io/machine-name in (md-0-abcde, md-1-fghij)", want: true},
		{name: "other machine", selector: "k0smotron.io/machine-name=md-1-fghij", want: false},
		{name: "conflicting requirements", selector: "k0smotron.io/machine-name=md-0-abcde,k0smotron.io/machine-name!=md-0-abcde", want: false},
		{name: "no machine-name label", selector: "kubernetes.io/os=linux", want: false},
		{name: "machine name as another label value", selector: "example.com/machine=md-0-abcde", want: false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := selectorTargetsMachine(tc.selector, "md-0-abcde")
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}

	_, err := selectorTargetsMachine("k0smotron.io/machine-name in (", "md-0-abcde")
	assert.Error(t, err)
}
