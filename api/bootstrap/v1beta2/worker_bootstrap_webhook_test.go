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

package v1beta2

import (
	"context"
	"testing"

	"github.com/k0sproject/k0smotron/internal/provisioner"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

func TestK0sWorkerConfigValidate(t *testing.T) {
	testCases := []struct {
		name             string
		in               *K0sWorkerConfig
		expectedWarnings admission.Warnings
		expectingError   bool
	}{
		{
			name: "valid config passes validation",
			in: &K0sWorkerConfig{
				Spec: K0sWorkerConfigSpec{
					Version: "v1.27.4+k0s.0",
					Files: []File{
						{
							ContentFrom: &ContentSource{
								SecretRef: &ContentSourceRef{
									Name: "my-secret",
									Key:  "my-key",
								},
							},
						},
						{
							File: provisioner.File{
								Path:    "/one/path/to/file",
								Content: "some-content",
							},
						},
						{
							File: provisioner.File{
								Path:    "/another/path/to/file",
								Content: "some-content",
							},
						},
					},
				},
			},
			expectedWarnings: nil,
			expectingError:   false,
		},
		{
			name: "err for unsupported k0s version",
			in: &K0sWorkerConfig{
				Spec: K0sWorkerConfigSpec{
					Version: "v1.27.4-k0s.0",
				},
			},
			expectedWarnings: nil,
			expectingError:   true,
		},
		{
			name: "err for invalid files declared in config: content and contentFrom conflict",
			in: &K0sWorkerConfig{
				Spec: K0sWorkerConfigSpec{
					Version: "v1.27.4+k0s.0",
					Files: []File{
						{
							File: provisioner.File{
								Content: "some-content",
							},
							ContentFrom: &ContentSource{
								SecretRef: &ContentSourceRef{
									Name: "my-secret",
									Key:  "my-key",
								},
							},
						},
					},
				},
			},
			expectedWarnings: nil,
			expectingError:   true,
		},
		{
			name: "err for invalid files declared in config: not content",
			in: &K0sWorkerConfig{
				Spec: K0sWorkerConfigSpec{
					Version: "v1.27.4+k0s.0",
					Files: []File{
						{
							File: provisioner.File{
								Content: "",
							},
						},
					},
				},
			},
			expectedWarnings: nil,
			expectingError:   true,
		},
		{
			name: "err for invalid files declared in config: contentFrom configmap and secret conflict",
			in: &K0sWorkerConfig{
				Spec: K0sWorkerConfigSpec{
					Version: "v1.27.4+k0s.0",
					Files: []File{
						{
							ContentFrom: &ContentSource{
								SecretRef: &ContentSourceRef{
									Name: "my-secret",
									Key:  "my-key",
								},
								ConfigMapRef: &ContentSourceRef{
									Name: "my-configmap",
									Key:  "my-key",
								},
							},
						},
					},
				},
			},
			expectedWarnings: nil,
			expectingError:   true,
		},
		{
			name: "err for invalid files declared in config: contentFrom configmap name missing",
			in: &K0sWorkerConfig{
				Spec: K0sWorkerConfigSpec{
					Version: "v1.27.4+k0s.0",
					Files: []File{
						{
							ContentFrom: &ContentSource{
								ConfigMapRef: &ContentSourceRef{
									Name: "",
									Key:  "my-key",
								},
							},
						},
					},
				},
			},
			expectedWarnings: nil,
			expectingError:   true,
		},
		{
			name: "err for invalid files declared in config: contentFrom secret name missing",
			in: &K0sWorkerConfig{
				Spec: K0sWorkerConfigSpec{
					Version: "v1.27.4+k0s.0",
					Files: []File{
						{
							ContentFrom: &ContentSource{
								SecretRef: &ContentSourceRef{
									Name: "",
									Key:  "my-key",
								},
							},
						},
					},
				},
			},
			expectedWarnings: nil,
			expectingError:   true,
		},
		{
			name: "err for invalid files declared in config: contentFrom secret name missing",
			in: &K0sWorkerConfig{
				Spec: K0sWorkerConfigSpec{
					Version: "v1.27.4+k0s.0",
					Files: []File{
						{
							File: provisioner.File{
								Path: "same-path",
							},
						},
						{
							File: provisioner.File{
								Path: "same-path",
							},
						},
						{
							File: provisioner.File{
								Path: "same-path",
							},
						},
					},
				},
			},
			expectedWarnings: nil,
			expectingError:   true,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			validator := &K0sWorkerConfigValidator{}
			warnings, err := validator.ValidateCreate(context.Background(), tc.in)
			if tc.expectingError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Empty(t, warnings)

			warnings, err = validator.ValidateUpdate(context.Background(), nil, tc.in)
			if tc.expectingError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Empty(t, warnings)

		})
	}
}
