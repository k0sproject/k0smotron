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
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseFeatureGateFlags(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expected    map[string]bool
		expectError bool
	}{
		{
			name:     "empty string",
			input:    "",
			expected: nil,
		},
		{
			name:     "multiple features",
			input:    "Feature1=true,Feature2=false, Feature3 = true ",
			expected: map[string]bool{"Feature1": true, "Feature2": false, "Feature3": true},
		},
		{
			name:     "multiple features with disabling default feature",
			input:    "Feature1=false,Feature2=false, Feature3 = true ",
			expected: map[string]bool{"Feature1": false, "Feature2": false, "Feature3": true},
		},
		{
			name:     "case insensitive values",
			input:    "Feature1=TRUE,Feature2=False,Feature3=True",
			expected: map[string]bool{"Feature1": true, "Feature2": false, "Feature3": true},
		},
		{
			name:        "missing value",
			input:       "Feature1",
			expectError: true,
		},
		{
			name:        "empty key",
			input:       " = true",
			expectError: true,
		},
		{
			name:        "invalid value",
			input:       "Feature1=maybe",
			expectError: true,
		},
		{
			name:        "multiple equals signs",
			input:       "Feature1=true=false",
			expectError: true,
		},
		{
			name:        "only spaces",
			input:       "   ",
			expectError: true,
		},
		{
			name:        "empty entries forbidden",
			input:       "Feature1=true,,Feature2=false,",
			expectError: true,
		},
	}

	defaultFeatureMap = map[Feature]FeatureGate{
		"Feature1": {Enabled: false},
		"Feature2": {Enabled: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseFeatureGateFlags(tt.input)

			if tt.expectError {
				assert.Error(t, err, "Expected an error but got none")
			} else {
				assert.NoError(t, err, "Unexpected error")
			}

			if tt.expected == nil {
				assert.Nil(t, result, "Expected result to be nil for empty input")
				return
			}
			assert.Len(t, result, len(tt.expected), "Expected feature map length does not match")

			for key, expectedValue := range tt.expected {
				assert.Equal(t, expectedValue, result[key], "Expected value for key %q does not match", key)
			}
		})
	}
}
