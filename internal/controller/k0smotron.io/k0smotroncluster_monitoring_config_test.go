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
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	km "github.com/k0sproject/k0smotron/v2/api/k0smotron.io/v1beta2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestGenerateMonitoringCM_storageJobs(t *testing.T) {
	tests := []struct {
		name        string
		storageType km.StorageType
		wantEtcd    bool
		wantKine    bool
	}{
		{name: "unset storage type keeps etcd job", storageType: "", wantEtcd: true},
		{name: "etcd", storageType: km.StorageTypeEtcd, wantEtcd: true},
		{name: "kine", storageType: km.StorageTypeKine, wantKine: true},
		{name: "nats has neither", storageType: km.StorageTypeNATS},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			kmc := &km.Cluster{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
				Spec:       km.ClusterSpec{Storage: km.StorageSpec{Type: tc.storageType}},
			}

			cm, err := newCertTestScope().generateMonitoringCM(kmc)
			require.NoError(t, err)

			cfg := cm.Data["prometheus.yml"]
			assert.Contains(t, cfg, "k0smotron_cluster_metrics")
			assert.Equal(t, tc.wantEtcd, strings.Contains(cfg, `job_name: "k0smotron_etcd_metrics"`))
			assert.Equal(t, tc.wantKine, strings.Contains(cfg, `job_name: "k0smotron_kine_metrics"`))
			if tc.wantKine {
				assert.Contains(t, cfg, `targets: ["localhost:2380"]`)
			}
		})
	}
}
