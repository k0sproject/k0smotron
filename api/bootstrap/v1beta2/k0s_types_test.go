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

package v1beta2

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

func TestK0sConfigSpecWorkerEnabled(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want bool
	}{
		{name: "no args", args: nil, want: false},
		{name: "controller only", args: []string{"--no-taints"}, want: false},
		{name: "enable-worker", args: []string{"--enable-worker"}, want: true},
		{name: "enable-worker=true", args: []string{"--enable-worker=true"}, want: true},
		// --single puts k0s in SingleNodeMode, which runs workloads just like
		// controller+worker does, so it has to count as worker-enabled too.
		{name: "single", args: []string{"--single"}, want: true},
		{name: "single among others", args: []string{"--no-taints", "--single"}, want: true},
		{name: "unrelated flag with worker substring", args: []string{"--enable-worker-foo"}, want: false},
		// pflag parses a bool value with ParseBool, so every one of these is
		// accepted by k0s and has to mean the same thing here.
		{name: "enable-worker=1", args: []string{"--enable-worker=1"}, want: true},
		{name: "enable-worker=t", args: []string{"--enable-worker=t"}, want: true},
		{name: "enable-worker=TRUE", args: []string{"--enable-worker=TRUE"}, want: true},
		{name: "single=true", args: []string{"--single=true"}, want: true},
		{name: "enable-worker=false", args: []string{"--enable-worker=false"}, want: false},
		{name: "enable-worker=0", args: []string{"--enable-worker=0"}, want: false},
		{name: "single=False", args: []string{"--single=False"}, want: false},
		// k0s itself refuses to start on these, so worker mode is unreachable.
		{name: "value pflag rejects", args: []string{"--enable-worker=yes"}, want: false},
		{name: "empty value", args: []string{"--enable-worker="}, want: false},
		{name: "a false value does not mask a later true one", args: []string{"--enable-worker=false", "--single"}, want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spec := &K0sConfigSpec{Args: tc.args}
			assert.Equal(t, tc.want, spec.WorkerEnabled())

			// The wrappers must agree with the spec-level helper.
			cfg := &K0sControllerConfig{Spec: K0sControllerConfigSpec{K0sConfigSpec: spec}}
			assert.Equal(t, tc.want, cfg.WorkerEnabled())
		})
	}
}

func TestK0sConfigSpecWorkerEnabledNilSpec(t *testing.T) {
	// K0sControllerConfigSpec embeds *K0sConfigSpec, so the nil case is reachable.
	var spec *K0sConfigSpec
	assert.False(t, spec.WorkerEnabled())
}

func TestK0sConfigSpecSingleNodeEnabled(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want bool
	}{
		{name: "no args", args: nil, want: false},
		{name: "single", args: []string{"--single"}, want: true},
		{name: "single among others", args: []string{"--no-taints", "--single"}, want: true},
		// The spellings the old bare match missed, which is what sent them past the guard.
		{name: "single=true", args: []string{"--single=true"}, want: true},
		{name: "single=1", args: []string{"--single=1"}, want: true},
		{name: "single=T", args: []string{"--single=T"}, want: true},
		{name: "single=false", args: []string{"--single=false"}, want: false},
		{name: "value pflag rejects", args: []string{"--single=yes"}, want: false},
		// Narrower than WorkerEnabled, since a multi controller cluster running workloads
		// on its controllers is not single node.
		{name: "enable-worker alone", args: []string{"--enable-worker"}, want: false},
		{name: "enable-worker=true alone", args: []string{"--enable-worker=true"}, want: false},
		{name: "unrelated flag with single substring", args: []string{"--single-foo"}, want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, (&K0sConfigSpec{Args: tc.args}).SingleNodeEnabled())
		})
	}
}
func TestK0sWorkerConfigSpec_validateWindows(t *testing.T) {
	pathPrefix := field.NewPath("spec")

	tests := []struct {
		name     string
		platform Platform
		version  string
		wantErrs int
	}{
		// Version edge cases are covered by TestValidateWindowsK0sVersion.
		{name: "linux platform ignores version", platform: PlatformLinux, version: "v1.0.0+k0s.0", wantErrs: 0},
		{name: "windows platform empty version is resolved later from the Machine", platform: PlatformWindows, version: "", wantErrs: 0},
		{name: "windows platform unsupported version", platform: PlatformWindows, version: "v1.34.1+k0s.0", wantErrs: 1},
		{name: "windows platform supported version", platform: PlatformWindows, version: "v1.34.2+k0s.0", wantErrs: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cs := K0sWorkerConfigSpec{
				Provisioner: ProvisionerSpec{Platform: tt.platform},
				Version:     tt.version,
			}
			got := cs.validateWindows(pathPrefix)
			assert.Len(t, got, tt.wantErrs)
		})
	}
}

func TestK0sConfigSpecSingleNodeEnabledNilSpec(t *testing.T) {
	var spec *K0sConfigSpec
	assert.False(t, spec.SingleNodeEnabled())

}

func TestValidateWindowsK0sVersion(t *testing.T) {
	tests := []struct {
		name    string
		version string
		wantErr bool
	}{
		{name: "empty version", version: "", wantErr: true},
		{name: "invalid version format", version: "not-a-version", wantErr: true},
		{name: "version below minimum", version: "v1.34.1+k0s.0", wantErr: true},
		{name: "version equal to minimum", version: "v1.34.2+k0s.0", wantErr: false},
		{name: "version above minimum", version: "v1.35.0+k0s.0", wantErr: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateWindowsK0sVersion(tt.version)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestK0sWorkerConfigSpec_validateVersion(t *testing.T) {
	pathPrefix := field.NewPath("spec")

	tests := []struct {
		name         string
		version      string
		wantWarnings admission.Warnings
		wantErrs     int
	}{
		{name: "empty version", version: "", wantWarnings: nil, wantErrs: 0},
		{name: "regular version", version: "v1.30.0", wantWarnings: admission.Warnings{deprecatedK0sConfigVersionField}, wantErrs: 0},
		{name: "k0s specific version with dash suffix is rejected", version: "v1.30.0-k0s.0", wantWarnings: admission.Warnings{deprecatedK0sConfigVersionField}, wantErrs: 1},
		{name: "k0s specific version with plus suffix is accepted", version: "v1.30.0+k0s.0", wantWarnings: admission.Warnings{deprecatedK0sConfigVersionField}, wantErrs: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cs := K0sWorkerConfigSpec{Version: tt.version}
			gotWarnings, gotErrs := cs.validateVersion(pathPrefix)
			assert.Equal(t, tt.wantWarnings, gotWarnings)
			assert.Len(t, gotErrs, tt.wantErrs)
		})
	}
}
