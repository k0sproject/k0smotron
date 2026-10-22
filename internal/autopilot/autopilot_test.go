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

package autopilot

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func unmarshalPlan(t *testing.T, params *PlanParameters) *unstructured.Unstructured {
	t.Helper()

	raw, err := buildPlan(params)
	require.NoError(t, err)

	plan := &unstructured.Unstructured{}
	require.NoError(t, json.Unmarshal(raw, &plan.Object))

	return plan
}

func TestBuildPlanStaticDiscovery(t *testing.T) {
	plan := unmarshalPlan(t, &PlanParameters{
		ID:        "id-1",
		Timestamp: "1700000000",
		Version:   "v1.35.4+k0s.0",
		Target:    ControllersTarget,
		Nodes:     []string{"cp-0", "cp-1"},
	})

	assert.Equal(t, "autopilot.k0sproject.io/v1beta2", plan.GetAPIVersion())
	assert.Equal(t, "Plan", plan.GetKind())
	assert.Equal(t, "autopilot", plan.GetName())

	id, _, _ := unstructured.NestedString(plan.Object, "spec", "id")
	assert.Equal(t, "id-1", id)
	timestamp, _, _ := unstructured.NestedString(plan.Object, "spec", "timestamp")
	assert.Equal(t, "1700000000", timestamp)

	version, err := GetPlanTargetVersion(plan)
	require.NoError(t, err)
	assert.Equal(t, "v1.35.4+k0s.0", version)

	targets, err := planTargets(plan)
	require.NoError(t, err)
	require.Contains(t, targets, "controllers")
	require.NotContains(t, targets, "workers")

	nodes, found, err := unstructured.NestedStringSlice(targets, "controllers", "discovery", "static", "nodes")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, []string{"cp-0", "cp-1"}, nodes)

	_, found, err = unstructured.NestedMap(targets, "controllers", "discovery", "selector")
	require.NoError(t, err)
	assert.False(t, found)

	commands, _, _ := unstructured.NestedSlice(plan.Object, "spec", "commands")
	platforms, _, _ := unstructured.NestedMap(commands[0].(map[string]any), "k0supdate", "platforms")
	amd64URL, _, _ := unstructured.NestedString(platforms, "linux-amd64", "url")
	assert.Equal(t, "https://get.k0sproject.io/v1.35.4+k0s.0/k0s-v1.35.4+k0s.0-amd64", amd64URL)
	arm64URL, _, _ := unstructured.NestedString(platforms, "linux-arm64", "url")
	assert.Equal(t, "https://get.k0sproject.io/v1.35.4+k0s.0/k0s-v1.35.4+k0s.0-arm64", arm64URL)
}

func TestBuildPlanSelectorDiscovery(t *testing.T) {
	plan := unmarshalPlan(t, &PlanParameters{
		ID:           "id-2",
		Timestamp:    "1700000000",
		Version:      "v1.35.4+k0s.0",
		Target:       WorkersTarget,
		NodeSelector: "k0smotron.io/machine-name=md-0-abcde",
	})

	targets, err := planTargets(plan)
	require.NoError(t, err)
	require.Contains(t, targets, "workers")

	labels, found, err := unstructured.NestedString(targets, "workers", "discovery", "selector", "labels")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "k0smotron.io/machine-name=md-0-abcde", labels)

	_, found, err = unstructured.NestedMap(targets, "workers", "discovery", "static")
	require.NoError(t, err)
	assert.False(t, found)
}

func TestBuildPlanCustomDownloadURL(t *testing.T) {
	plan := unmarshalPlan(t, &PlanParameters{
		ID:          "id-3",
		Timestamp:   "1700000000",
		Version:     "v1.35.4+k0s.0",
		DownloadURL: "https://mirror.example.com/k0s",
		Target:      WorkersTarget,
		Nodes:       []string{"worker-0"},
	})

	commands, _, _ := unstructured.NestedSlice(plan.Object, "spec", "commands")
	platforms, _, _ := unstructured.NestedMap(commands[0].(map[string]any), "k0supdate", "platforms")
	for _, platform := range []string{"linux-amd64", "linux-arm64", "linux-arm"} {
		url, _, _ := unstructured.NestedString(platforms, platform, "url")
		assert.Equal(t, "https://mirror.example.com/k0s", url, platform)
	}
}

func TestBuildPlanValidation(t *testing.T) {
	testCases := []struct {
		name   string
		params PlanParameters
	}{
		{
			name:   "missing target",
			params: PlanParameters{Version: "v1.35.4+k0s.0", Nodes: []string{"worker-0"}},
		},
		{
			name:   "neither nodes nor selector",
			params: PlanParameters{Version: "v1.35.4+k0s.0", Target: WorkersTarget},
		},
		{
			name: "both nodes and selector",
			params: PlanParameters{
				Version:      "v1.35.4+k0s.0",
				Target:       WorkersTarget,
				Nodes:        []string{"worker-0"},
				NodeSelector: "k0smotron.io/machine-name=md-0-abcde",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := buildPlan(&tc.params)
			assert.Error(t, err)
		})
	}
}

func TestGetPlanTargetNodesAndSelector(t *testing.T) {
	staticPlan := unmarshalPlan(t, &PlanParameters{
		Version: "v1.35.4+k0s.0",
		Target:  ControllersTarget,
		Nodes:   []string{"cp-0"},
	})
	selectorPlan := unmarshalPlan(t, &PlanParameters{
		Version:      "v1.35.4+k0s.0",
		Target:       WorkersTarget,
		NodeSelector: "k0smotron.io/machine-name=md-0-abcde",
	})

	nodes, err := GetPlanTargetNodes(staticPlan)
	require.NoError(t, err)
	assert.Equal(t, []string{"cp-0"}, nodes)
	selector, err := GetPlanTargetSelector(staticPlan, ControllersTarget)
	require.NoError(t, err)
	assert.Empty(t, selector)

	nodes, err = GetPlanTargetNodes(selectorPlan)
	require.NoError(t, err)
	assert.Empty(t, nodes)
	selector, err = GetPlanTargetSelector(selectorPlan, WorkersTarget)
	require.NoError(t, err)
	assert.Equal(t, "k0smotron.io/machine-name=md-0-abcde", selector)
	selector, err = GetPlanTargetSelector(selectorPlan, ControllersTarget)
	require.NoError(t, err)
	assert.Empty(t, selector)

	noCommands := &unstructured.Unstructured{Object: map[string]any{"spec": map[string]any{}}}
	_, err = GetPlanTargetNodes(noCommands)
	assert.Error(t, err)
	_, err = GetPlanTargetSelector(noCommands, WorkersTarget)
	assert.Error(t, err)
}
