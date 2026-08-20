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

	"github.com/k0sproject/k0smotron/v2/internal/provisioner"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/util/validation/field"
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

func TestValidateFileOwnerChecksEveryFile(t *testing.T) {
	spec := &K0sWorkerConfigSpec{
		Files: []File{
			{File: provisioner.File{Path: "/a", Content: "x", Owner: "etcd:etcd"}},
			{File: provisioner.File{Path: "/b", Content: "x", Owner: "root; rm -rf /"}},
		},
	}

	errs := spec.validateFiles(field.NewPath("spec"))

	require.Len(t, errs, 1)
	require.Equal(t, "spec.files[1].owner", errs[0].Field, "the index must follow the offending file")
}

func TestValidateFileOwner(t *testing.T) {
	tests := []struct {
		name     string
		format   provisioner.ProvisioningFormat
		platform Platform
		owner    string
		wantErr  string
	}{
		{name: "empty owner is allowed", owner: ""},
		{name: "user only", owner: "root"},
		{name: "user and group", owner: "etcd:etcd"},
		{name: "dots, dashes and underscores", owner: "sys_user.1:sys-group"},
		{name: "shell metacharacters are rejected", owner: "root; rm -rf /", wantErr: ownerFormatMsg},
		{name: "command substitution is rejected", owner: "$(id -u)", wantErr: ownerFormatMsg},
		{name: "spaces are rejected", owner: "root root", wantErr: ownerFormatMsg},
		{name: "trailing separator is rejected", owner: "root:", wantErr: ownerFormatMsg},
		{name: "more than one separator is rejected", owner: "a:b:c", wantErr: ownerFormatMsg},
		{
			name:    "owner is rejected for the powershell format",
			format:  provisioner.PowershellProvisioningFormat,
			owner:   "root:root",
			wantErr: ownerOnPowerShellMsg,
		},
		{
			name:    "owner is rejected for the powershell xml format",
			format:  provisioner.PowershellXMLProvisioningFormat,
			owner:   "root:root",
			wantErr: ownerOnPowerShellMsg,
		},
		{
			name:   "owner is allowed for the ignition format",
			format: provisioner.IgnitionProvisioningFormat,
			owner:  "etcd:etcd",
		},
		{
			name:     "owner is rejected on the windows platform",
			platform: PlatformWindows,
			owner:    "root:root",
			wantErr:  ownerOnPowerShellMsg,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := &K0sWorkerConfigSpec{
				Provisioner: ProvisionerSpec{Type: tt.format, Platform: tt.platform},
				Files: []File{
					{File: provisioner.File{Path: "/etc/thing", Content: "x", Owner: tt.owner}},
				},
			}

			errs := spec.validateFiles(field.NewPath("spec"))

			if tt.wantErr == "" {
				require.Empty(t, errs)
				return
			}

			require.Len(t, errs, 1)
			require.Equal(t, "spec.files[0].owner", errs[0].Field)
			require.Contains(t, errs[0].Detail, tt.wantErr)
		})
	}
}
