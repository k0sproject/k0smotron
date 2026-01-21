//go:build !envtest

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
	"github.com/k0sproject/version"
	"testing"

	bootstrapv1 "github.com/k0sproject/k0smotron/api/bootstrap/v1beta1"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	bsutil "sigs.k8s.io/cluster-api/bootstrap/util"
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
				installArgs: []string{"--enable-worker", "--labels=k0sproject.io/foo=bar"},
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

func TestController_genK0sCommands(t *testing.T) {
	tests := []struct {
		scope      *ControllerScope
		installCmd string
		want       []string
	}{
		{
			scope: &ControllerScope{
				currentKCPVersion: version.MustParse("v1.31.0"),
				Config: &bootstrapv1.K0sControllerConfig{
					ObjectMeta: metav1.ObjectMeta{Name: "test"},
					Spec: bootstrapv1.K0sControllerConfigSpec{
						Version:       "v1.31.0",
						K0sConfigSpec: &bootstrapv1.K0sConfigSpec{},
					},
				},
			},
			installCmd: "k0s install controller --force --enable-dynamic-config",
			want: []string{
				"curl -sSfL --retry 5 https://get.k0s.sh | K0S_INSTALL_PATH=/usr/local/bin K0S_VERSION=v1.31.0 sh",
				"(command -v systemctl > /dev/null 2>&1 && (cp /k0s/k0sleave.service /etc/systemd/system/k0sleave.service && systemctl daemon-reload && systemctl enable k0sleave.service && systemctl start --no-block k0sleave.service) || true)",
				"(command -v rc-service > /dev/null 2>&1 && (cp /k0s/k0sleave-openrc /etc/init.d/k0sleave && rc-update add k0sleave shutdown) || true)",
				"(command -v service > /dev/null 2>&1 && (cp /k0s/k0sleave-sysv /etc/init.d/k0sleave && update-rc.d k0sleave defaults && service k0sleave start) || true)",
				"k0s install controller --force --enable-dynamic-config",
				"k0s start",
			},
		},
		{
			scope: &ControllerScope{
				currentKCPVersion: version.MustParse("v1.31.6"),
				Config: &bootstrapv1.K0sControllerConfig{
					ObjectMeta: metav1.ObjectMeta{Name: "test"},
					Spec: bootstrapv1.K0sControllerConfigSpec{
						Version:       "v1.31.6",
						K0sConfigSpec: &bootstrapv1.K0sConfigSpec{},
					},
				},
			},
			installCmd: "k0s install controller --force --enable-dynamic-config",
			want: []string{
				"curl -sSfL --retry 5 https://get.k0s.sh | K0S_INSTALL_PATH=/usr/local/bin K0S_VERSION=v1.31.6 sh",
				"k0s install controller --force --enable-dynamic-config",
				"k0s start",
			},
		},
		{
			scope: &ControllerScope{
				currentKCPVersion: version.MustParse("v1.31.6+k0s.0"),
				Config: &bootstrapv1.K0sControllerConfig{
					ObjectMeta: metav1.ObjectMeta{Name: "test"},
					Spec: bootstrapv1.K0sControllerConfigSpec{
						Version:       "v1.31.6+k0s.0",
						K0sConfigSpec: &bootstrapv1.K0sConfigSpec{},
					},
				},
			},
			installCmd: "k0s install controller --force --enable-dynamic-config",
			want: []string{
				"curl -sSfL --retry 5 https://get.k0s.sh | K0S_INSTALL_PATH=/usr/local/bin K0S_VERSION=v1.31.6+k0s.0 sh",
				"k0s install controller --force --enable-dynamic-config",
				"k0s start",
			},
		},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			c := &ControlPlaneController{}
			commands, _, err := c.genK0sCommands(tt.scope, tt.installCmd)
			require.NoError(t, err)
			require.Equal(t, tt.want, commands)
		})
	}
}
