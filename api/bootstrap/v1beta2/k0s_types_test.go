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
