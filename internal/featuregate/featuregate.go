/*
Copyright 2025.

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

package featuregate

import (
	"fmt"
	"strconv"
	"strings"
)

func init() {
	effectiveFeatureGates = &FeatureGates{
		featureMap: defaultFeatureMap,
	}
}

// Feature represents a feature gate name.
type Feature string

// FeatureGates manages the state of feature gates.
type FeatureGates struct {
	featureMap map[Feature]FeatureGate
}

// FeatureGate represents a single feature gate with its enabled state.
type FeatureGate struct {
	// Enabled determines if the feature is enabled.
	// When declared, it should set to its default value, which may be
	// overwritten based on CLI flags.
	Enabled bool
}

// isEnabled checks if a feature is enabled.
func (fg *FeatureGates) isEnabled(feature Feature) bool {
	if f, ok := fg.featureMap[feature]; ok {
		return f.Enabled
	}
	return false
}

// Configure parses the provided flag string and configures the
// feature gates accordingly. This function is not thread-safe and is only
// expected to be called once during the initialization phase.
func Configure(flag string) error {
	featureMap, err := parseFeatureGateFlags(flag)
	if err != nil {
		return err
	}

	// Apply the parsed feature gates
	for feature, enabled := range featureMap {
		gate, exists := effectiveFeatureGates.featureMap[Feature(feature)]
		if !exists {
			return fmt.Errorf("unknown feature gate %q", feature)
		}
		gate.Enabled = enabled
		effectiveFeatureGates.featureMap[Feature(feature)] = gate
	}

	return nil
}

// parseFeatureGateFlags parses a comma-separated string of key=value pairs
// and returns a map of feature names to their enabled state.
func parseFeatureGateFlags(flag string) (map[string]bool, error) {
	if flag == "" {
		return nil, nil
	}

	featureMap := make(map[string]bool)

	for _, pair := range strings.Split(flag, ",") {
		key, value, found := strings.Cut(pair, "=")
		if !found {
			return nil, fmt.Errorf("invalid feature gate format %q, expected key=value", pair)
		}

		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)

		if key == "" {
			return nil, fmt.Errorf("empty feature gate key in %q", pair)
		}

		enabled, err := strconv.ParseBool(value)
		if err != nil {
			return nil, fmt.Errorf("invalid feature gate value %q for key %q, expected true or false", value, key)
		}
		featureMap[key] = enabled
	}

	return featureMap, nil
}

// IsEnabled checks if a feature is enabled in the effectiveFeatureGates singleton
// This function expects that effectiveFeatureGates has been initialized and otherwise
// will panic.
func IsEnabled(feature Feature) bool {
	return effectiveFeatureGates.isEnabled(feature)
}
