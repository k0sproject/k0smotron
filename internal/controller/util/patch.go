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

package util

import (
	"encoding/json"
	"fmt"

	jsonpatch "github.com/evanphx/json-patch/v5"
	km "github.com/k0sproject/k0smotron/api/k0smotron.io/v1beta1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	"sigs.k8s.io/controller-runtime/pkg/client"
	sigsyaml "sigs.k8s.io/yaml"
)

const componentLabel = "app.kubernetes.io/component"

// ApplyComponentPatches applies user-defined patches to a resource based on matching
// resourceType (Kind) and component label. Patches are applied in the order they appear.
// The scheme is required for strategic merge patches to resolve the patch metadata.
func ApplyComponentPatches(scheme *runtime.Scheme, obj client.Object, patches []km.ComponentPatch) error {
	if len(patches) == 0 {
		return nil
	}

	gvks, _, err := scheme.ObjectKinds(obj)
	if err != nil || len(gvks) == 0 {
		return fmt.Errorf("could not get GVK for object: %w", err)
	}
	kind := gvks[0].Kind
	labels := obj.GetLabels()
	component := labels[componentLabel]

	currentData, err := json.Marshal(obj)
	if err != nil {
		return fmt.Errorf("marshal object for patching: %w", err)
	}

	applied := false
	for i, p := range patches {
		if p.ResourceType != kind || p.Component != component {
			continue
		}

		patchJSON, convErr := sigsyaml.YAMLToJSON([]byte(p.Patch))
		if convErr != nil {
			return fmt.Errorf("convert patch at index %d to JSON: %w", i, convErr)
		}

		switch p.Type {
		case km.JSONPatchType:
			currentData, err = applyJSONPatch(currentData, patchJSON)
		case km.StrategicMergePatchType:
			currentData, err = applyStrategicMergePatch(scheme, &gvks[0], currentData, patchJSON)
		case km.MergePatchType:
			currentData, err = applyMergePatch(currentData, patchJSON)
		default:
			return fmt.Errorf("unknown patch type %q for patch at index %d", p.Type, i)
		}
		if err != nil {
			return fmt.Errorf("apply patch at index %d (type=%s, resourceType=%s, component=%s): %w",
				i, p.Type, p.ResourceType, p.Component, err)
		}
		applied = true
	}

	if applied {
		if err := json.Unmarshal(currentData, obj); err != nil {
			return fmt.Errorf("unmarshal patched object: %w", err)
		}
	}
	return nil
}

func applyJSONPatch(original, patchData []byte) ([]byte, error) {
	p, err := jsonpatch.DecodePatch(patchData)
	if err != nil {
		return nil, fmt.Errorf("decode json patch: %w", err)
	}
	return p.Apply(original)
}

func applyStrategicMergePatch(scheme *runtime.Scheme, gvk *schema.GroupVersionKind, original, patchData []byte) ([]byte, error) {
	empty, err := scheme.New(*gvk)
	if err != nil {
		return nil, fmt.Errorf("create empty struct for strategic merge: %w", err)
	}
	return strategicpatch.StrategicMergePatch(original, patchData, empty)
}

func applyMergePatch(original, patchData []byte) ([]byte, error) {
	return jsonpatch.MergeMergePatches(original, patchData)
}
