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

package v1beta2

import (
	"context"
	"testing"

	"github.com/k0sproject/k0smotron/v2/internal/provisioner"
	"github.com/stretchr/testify/require"
)

func TestK0sControllerConfigValidate(t *testing.T) {
	testCases := []struct {
		name           string
		in             *K0sControllerConfig
		expectingError bool
	}{
		{
			name: "valid config passes validation",
			in: &K0sControllerConfig{
				Spec: K0sControllerConfigSpec{
					Version: "v1.27.4+k0s.0",
					K0sConfigSpec: &K0sConfigSpec{
						Files: []File{
							{
								File: provisioner.File{
									Path:    "/tmp/some-file",
									Content: "some-content",
									Owner:   "root:root",
								},
							},
						},
					},
				},
			},
		},
		{
			name: "a config carrying nothing but a version passes validation",
			in: &K0sControllerConfig{
				Spec: K0sControllerConfigSpec{Version: "v1.27.4+k0s.0"},
			},
		},
		{
			name: "an owner that the chown could read as an argument is rejected",
			in: &K0sControllerConfig{
				Spec: K0sControllerConfigSpec{
					K0sConfigSpec: &K0sConfigSpec{
						Files: []File{
							{
								File: provisioner.File{
									Path:    "/tmp/some-file",
									Content: "some-content",
									Owner:   "--reference=/etc/shadow",
								},
							},
						},
					},
				},
			},
			expectingError: true,
		},
		{
			name: "an owner no PowerShell render can apply is rejected",
			in: &K0sControllerConfig{
				Spec: K0sControllerConfigSpec{
					K0sConfigSpec: &K0sConfigSpec{
						Provisioner: ProvisionerSpec{Type: provisioner.PowershellProvisioningFormat},
						Files: []File{
							{
								File: provisioner.File{
									Path:    `C:\some-file`,
									Content: "some-content",
									Owner:   "root:root",
								},
							},
						},
					},
				},
			},
			expectingError: true,
		},
	}

	v := &K0sControllerConfigValidator{}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := v.ValidateCreate(context.Background(), tc.in)
			if tc.expectingError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			// An update reaches the same object, so it must reach the same verdict.
			_, err = v.ValidateUpdate(context.Background(), tc.in, tc.in)
			if tc.expectingError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestK0sControllerConfigValidatorRejectsAnotherType covers the type assertion, since the
// webhook is registered for one kind and a mismatch means a misconfigured registration.
func TestK0sControllerConfigValidatorRejectsAnotherType(t *testing.T) {
	v := &K0sControllerConfigValidator{}

	_, err := v.ValidateCreate(context.Background(), &K0sWorkerConfig{})
	require.ErrorContains(t, err, "expected a K0sControllerConfig")

	_, err = v.ValidateUpdate(context.Background(), nil, &K0sWorkerConfig{})
	require.ErrorContains(t, err, "expected a K0sControllerConfig")
}

func TestK0sControllerConfigValidateDeleteAllowsEverything(t *testing.T) {
	warnings, err := (&K0sControllerConfigValidator{}).ValidateDelete(context.Background(), &K0sControllerConfig{})

	require.NoError(t, err)
	require.Nil(t, warnings)
}
