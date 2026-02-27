//go:build !envtest

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

package k0smotronio

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	km "github.com/k0sproject/k0smotron/api/k0smotron.io/v1beta2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestGetControllerFlags(t *testing.T) {
	tests := []struct {
		name   string
		kmc    km.Cluster
		result string
	}{
		{
			"Undefined flags must not panic",
			km.Cluster{},
			"--config=/etc/k0s/k0s.yaml --enable-dynamic-config",
		},
		{
			"Empty flags",
			km.Cluster{Spec: km.ClusterSpec{ControlPlaneFlags: []string{}}},
			"--config=/etc/k0s/k0s.yaml --enable-dynamic-config",
		},
		{
			"Multiple flags",
			km.Cluster{Spec: km.ClusterSpec{ControlPlaneFlags: []string{"--foo=bar", "--bar=baz"}}},
			"--foo=bar --bar=baz --config=/etc/k0s/k0s.yaml --enable-dynamic-config",
		},
		{
			"Override dynamic config",
			km.Cluster{Spec: km.ClusterSpec{ControlPlaneFlags: []string{"--enable-dynamic-config=false"}}},
			"--enable-dynamic-config=false --config=/etc/k0s/k0s.yaml",
		},
		{
			"Override config path",
			km.Cluster{Spec: km.ClusterSpec{ControlPlaneFlags: []string{"--config=/custom/path"}}},
			"--config=/custom/path --enable-dynamic-config",
		},
	}

	for _, test := range tests {
		assert.Equal(t, test.result, getControllerFlags(&test.kmc), test.name)
	}
}

func TestKineDataSourceURLSubstitution(t *testing.T) {
	if _, err := exec.LookPath("sed"); err != nil {
		t.Skip("sed command not available, skipping test")
	}

	tests := []struct {
		name                 string
		kineDataSourceURL    string
		expectedSubstitution string
	}{
		{
			name:                 "URL without ampersands",
			kineDataSourceURL:    "postgres://user:pass@host:5432/db?sslmode=disable",
			expectedSubstitution: "postgres://user:pass@host:5432/db?sslmode=disable",
		},
		{
			name:                 "URL with single ampersand",
			kineDataSourceURL:    "postgres://user:pass@host:5432/db?param1=value1&param2=value2",
			expectedSubstitution: "postgres://user:pass@host:5432/db?param1=value1&param2=value2",
		},
		{
			name:                 "URL with multiple ampersands",
			kineDataSourceURL:    "postgres://user:pass@host:5432/db?param1=value1&param2=value2&param3=value3",
			expectedSubstitution: "postgres://user:pass@host:5432/db?param1=value1&param2=value2&param3=value3",
		},
		{
			name:                 "URL with ampersand in password",
			kineDataSourceURL:    "postgres://user:pa&ss@host:5432/db",
			expectedSubstitution: "postgres://user:pa&ss@host:5432/db",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			kmc := &km.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "default",
				},
				Spec: km.ClusterSpec{
					Service: km.ServiceSpec{
						APIPort: 6443,
					},
					KineDataSourceURL: tc.kineDataSourceURL,
				},
			}

			scheme := runtime.NewScheme()
			require.NoError(t, km.AddToScheme(scheme))

			client := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(kmc).
				Build()

			scope := &kmcScope{
				client: client,
			}

			cm, err := scope.generateEntrypointCM(kmc)
			require.NoError(t, err)

			entrypointScript := cm.Data["k0smotron-entrypoint.sh"]

			lines := strings.Split(entrypointScript, "\n")
			var printfLine, sedLine string
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if strings.Contains(line, "escaped_url=$(printf") && strings.Contains(line, "K0SMOTRON_KINE_DATASOURCE_URL") {
					printfLine = line
				}
				if strings.Contains(line, "sedi") && strings.Contains(line, "$escaped_url") {
					sedLine = line
				}
			}
			require.NotEmpty(t, printfLine, "printf command not found in entrypoint script")
			require.NotEmpty(t, sedLine, "sedi command not found in entrypoint script")

			testK0sConfig := `apiVersion: k0s.k0sproject.io/v1beta1
kind: ClusterConfig
metadata:
  name: k0s
spec:
  storage:
    type: kine
    kine:
      dataSource: ` + kineDataSourceURLPlaceholder + `
  api:
    port: 6443`

			tmpFile, err := os.CreateTemp("", "k0s-test-*.yaml")
			require.NoError(t, err)
			t.Cleanup(func() { os.Remove(tmpFile.Name()) })

			_, err = tmpFile.WriteString(testK0sConfig)
			require.NoError(t, err)
			require.NoError(t, tmpFile.Close())

			os.Setenv("K0SMOTRON_KINE_DATASOURCE_URL", tc.kineDataSourceURL)
			t.Cleanup(func() { os.Unsetenv("K0SMOTRON_KINE_DATASOURCE_URL") })

			actualSediCmd := strings.Replace(sedLine, "/etc/k0s/k0s.yaml", tmpFile.Name(), 1)

			combinedScript := universalSedInplace + "\n" + printfLine + "\n" + actualSediCmd

			cmd := exec.Command("sh", "-c", combinedScript)
			cmd.Env = append(os.Environ(), "K0SMOTRON_KINE_DATASOURCE_URL="+tc.kineDataSourceURL)

			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Logf("Combined script: %s", combinedScript)
				t.Logf("Script output: %s", string(output))
			}
			require.NoError(t, err, "Shell script failed: %s", string(output))

			modifiedContent, err := os.ReadFile(tmpFile.Name())
			require.NoError(t, err)

			modifiedStr := string(modifiedContent)
			assert.Contains(t, modifiedStr, tc.expectedSubstitution,
				"Expected URL %q not found in modified config", tc.expectedSubstitution)
			assert.NotContains(t, modifiedStr, kineDataSourceURLPlaceholder,
				"Placeholder should be completely replaced")
		})
	}
}
