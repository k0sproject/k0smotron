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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

// TestProvisionerWarningsForIgnition covers the hint for a config carried over from
// another provisioner, which is a warning rather than a rejection.
func TestProvisionerWarningsForIgnition(t *testing.T) {
	ref := &ContentSource{SecretRef: &ContentSourceRef{Name: "extra", Key: "userdata"}}

	t.Run("ignition with a custom user data ref warns", func(t *testing.T) {
		warnings := ProvisionerWarnings(ProvisionerSpec{
			Type:              provisioner.IgnitionProvisioningFormat,
			CustomUserDataRef: ref,
		}, field.NewPath("spec"))

		require.Len(t, warnings, 1)
		require.Equal(t, "spec.provisioner.customUserDataRef is ignored by the ignition provisioner, use spec.provisioner.ignition.additionalConfig instead", warnings[0])
	})

	t.Run("cloud-init with the same ref is silent", func(t *testing.T) {
		require.Empty(t, ProvisionerWarnings(ProvisionerSpec{CustomUserDataRef: ref}, field.NewPath("spec")))
	})

	t.Run("ignition without the ref is silent", func(t *testing.T) {
		require.Empty(t, ProvisionerWarnings(ProvisionerSpec{
			Type: provisioner.IgnitionProvisioningFormat,
		}, field.NewPath("spec")))
	})

	t.Run("the worker webhook surfaces it and still admits", func(t *testing.T) {
		cfg := &K0sWorkerConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "w"},
			Spec: K0sWorkerConfigSpec{
				Version: "v1.30.0+k0s.0",
				Provisioner: ProvisionerSpec{
					Type:              provisioner.IgnitionProvisioningFormat,
					CustomUserDataRef: ref,
				},
			},
		}

		warnings, err := (&K0sWorkerConfigValidator{}).ValidateCreate(context.Background(), cfg)

		require.NoError(t, err, "the config is still accepted")
		require.Len(t, warnings, 1)
	})
}

// TestProvisionerWarningsOnWorkerUpdate covers the update entry point, which is a separate
// method on the validator and so a separate place the warning can go missing.
func TestProvisionerWarningsOnWorkerUpdate(t *testing.T) {
	cfg := &K0sWorkerConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "w"},
		Spec: K0sWorkerConfigSpec{
			Version: "v1.30.0+k0s.0",
			Provisioner: ProvisionerSpec{
				Type:              provisioner.IgnitionProvisioningFormat,
				CustomUserDataRef: &ContentSource{SecretRef: &ContentSourceRef{Name: "extra", Key: "userdata"}},
			},
		},
	}

	warnings, err := (&K0sWorkerConfigValidator{}).ValidateUpdate(t.Context(), cfg, cfg)

	require.NoError(t, err, "the config is still accepted")
	require.Len(t, warnings, 1)
	require.Equal(t, "spec.provisioner.customUserDataRef is ignored by the ignition provisioner, use spec.provisioner.ignition.additionalConfig instead", warnings[0])
}

// TestValidateProvisionerRejectsConflictingUserDataRef covers the conflict the schema
// cannot catch, since each reference is optional on its own. The same shape is already
// rejected under a file's contentFrom.
func TestValidateProvisionerRejectsConflictingUserDataRef(t *testing.T) {
	secretRef := &ContentSourceRef{Name: "extra", Key: "userdata"}
	configMapRef := &ContentSourceRef{Name: "other", Key: "userdata"}

	for _, tc := range []struct {
		name    string
		ref     *ContentSource
		wantErr bool
	}{
		{
			name:    "both references is a conflict",
			ref:     &ContentSource{SecretRef: secretRef, ConfigMapRef: configMapRef},
			wantErr: true,
		},
		{
			name: "a secret reference alone is fine",
			ref:  &ContentSource{SecretRef: secretRef},
		},
		{
			name: "a config map reference alone is fine",
			ref:  &ContentSource{ConfigMapRef: configMapRef},
		},
		{
			name: "no reference at all is fine",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			errs := ValidateProvisioner(ProvisionerSpec{CustomUserDataRef: tc.ref}, field.NewPath("spec"))

			if !tc.wantErr {
				require.Empty(t, errs)

				return
			}

			require.Len(t, errs, 1)
			require.Equal(t, "spec.provisioner.customUserDataRef", errs[0].Field)
			require.Contains(t, errs[0].Detail, conflictingContentSourceMsg)
		})
	}

	// The provisioner type does not enter into it, since the conflict is undecidable
	// whichever one reads the field.
	t.Run("the conflict is rejected for ignition too", func(t *testing.T) {
		errs := ValidateProvisioner(ProvisionerSpec{
			Type:              provisioner.IgnitionProvisioningFormat,
			CustomUserDataRef: &ContentSource{SecretRef: secretRef, ConfigMapRef: configMapRef},
		}, field.NewPath("spec"))

		require.Len(t, errs, 1)
	})
}

// TestWorkerConfigRatchetsProvisionerErrors covers an object admitted before a rule
// existed staying updatable, or the controller cannot strip its own finalizer.
func TestWorkerConfigRatchetsProvisionerErrors(t *testing.T) {
	cfg := func(ref *ContentSource) *K0sWorkerConfig {
		return &K0sWorkerConfig{
			Spec: K0sWorkerConfigSpec{
				Version:     "v1.30.0+k0s.0",
				Provisioner: ProvisionerSpec{CustomUserDataRef: ref},
			},
		}
	}
	conflicting := &ContentSource{
		SecretRef:    &ContentSourceRef{Name: "extra", Key: "userdata"},
		ConfigMapRef: &ContentSourceRef{Name: "other", Key: "userdata"},
	}
	clean := &ContentSource{SecretRef: &ContentSourceRef{Name: "extra", Key: "userdata"}}

	v := &K0sWorkerConfigValidator{}

	t.Run("create is rejected", func(t *testing.T) {
		_, err := v.ValidateCreate(t.Context(), cfg(conflicting))

		require.ErrorContains(t, err, "only one of secretRef or configMapRef")
	})

	t.Run("introducing the conflict on update is rejected", func(t *testing.T) {
		_, err := v.ValidateUpdate(t.Context(), cfg(clean), cfg(conflicting))

		require.ErrorContains(t, err, "only one of secretRef or configMapRef")
	})

	t.Run("an update that inherits the conflict is allowed", func(t *testing.T) {
		_, err := v.ValidateUpdate(t.Context(), cfg(conflicting), cfg(conflicting))

		require.NoError(t, err)
	})
}

// TestValidateProvisionerMatchesTheFileChecks covers the gaps the schema leaves, which
// a file's contentFrom already guards and which otherwise only fail at reconcile.
func TestValidateProvisionerMatchesTheFileChecks(t *testing.T) {
	for _, tc := range []struct {
		name string
		ref  *ContentSource
		want string
	}{
		{
			name: "neither source is rejected",
			ref:  &ContentSource{},
			want: noContentSourceMsg,
		},
		{
			name: "an empty secret name is rejected",
			ref:  &ContentSource{SecretRef: &ContentSourceRef{Key: "userdata"}},
			want: "name is required",
		},
		{
			name: "an empty config map name is rejected",
			ref:  &ContentSource{ConfigMapRef: &ContentSourceRef{Key: "userdata"}},
			want: "name is required",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			errs := ValidateProvisioner(ProvisionerSpec{CustomUserDataRef: tc.ref}, field.NewPath("spec"))

			require.Len(t, errs, 1)
			require.Contains(t, errs[0].Error(), tc.want)
			require.Contains(t, errs[0].Field, "spec.provisioner.customUserDataRef")
		})
	}
}
