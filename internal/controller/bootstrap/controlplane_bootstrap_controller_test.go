/*


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

package bootstrap

import (
	bootstrapv1 "github.com/k0sproject/k0smotron/api/bootstrap/v1beta1"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	bsutil "sigs.k8s.io/cluster-api/bootstrap/util"
	"testing"
)

func Test_createCPInstallCmd(t *testing.T) {
	base := "k0s install controller --force --enable-dynamic-config "
	tests := []struct {
		name  string
		scope *ControllerScope
		want  string
	}{
		{
			name: "with default config",
			scope: &ControllerScope{
				Config: &bootstrapv1.K0sControllerConfig{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test",
					},
					Spec: bootstrapv1.K0sControllerConfigSpec{
						K0sConfigSpec: &bootstrapv1.K0sConfigSpec{},
					},
				},
				ConfigOwner: &bsutil.ConfigOwner{Unstructured: &unstructured.Unstructured{Object: map[string]interface{}{
					"metadata": map[string]interface{}{"name": "test-machine"},
				}}},
			},
			want: base + "--env AUTOPILOT_HOSTNAME=test",
		},
		{
			name: "with args",
			scope: &ControllerScope{
				Config: &bootstrapv1.K0sControllerConfig{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test",
					},
					Spec: bootstrapv1.K0sControllerConfigSpec{
						K0sConfigSpec: &bootstrapv1.K0sConfigSpec{
							Args: []string{"--enable-worker", "--labels=k0sproject.io/foo=bar"},
						},
					},
				},
				ConfigOwner: &bsutil.ConfigOwner{Unstructured: &unstructured.Unstructured{Object: map[string]interface{}{
					"metadata": map[string]interface{}{"name": "test-machine"},
				}}},
				WorkerEnabled: true,
			},
			want: base + "--env AUTOPILOT_HOSTNAME=test --labels=k0smotron.io/machine-name=test-machine --enable-worker --labels=k0sproject.io/foo=bar --kubelet-extra-args=\"--hostname-override=test-machine\"",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, createCPInstallCmd(tt.scope))
		})
	}
}
