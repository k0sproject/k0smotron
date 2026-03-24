//go:build !envtest

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
package k0smotronio

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/k0sproject/k0smotron/api/k0smotron.io/v1beta1"
)

const sampleKubeconfig = `apiVersion: v1
clusters:
- cluster:
    certificate-authority-data: Q0FjZXJ0
    server: https://k8s-default-xxx-xxx-xxx.elb.ap-northeast-1.amazonaws.com:6443
  name: k0s
contexts:
- context:
    cluster: k0s
    user: admin
  name: k0s
current-context: k0s
kind: Config
preferences: {}
users:
- name: admin
  user:
    client-certificate-data: Q0xJRU5UQ0VSVA==
    client-key-data: Q0xJRU5US0VZ
`

const expectedKubeconfig = `apiVersion: v1
clusters:
- cluster:
    certificate-authority-data: Q0FjZXJ0
    server: https://k8s-default-xxx-xxx-xxx.elb.ap-northeast-1.amazonaws.com:6443
  name: wl1-k0s
contexts:
- context:
    cluster: wl1-k0s
    user: wl1-admin
  name: wl1-admin@wl1-k0s
current-context: wl1-admin@wl1-k0s
kind: Config
preferences: {}
users:
- name: wl1-admin
  user:
    client-certificate-data: Q0xJRU5UQ0VSVA==
    client-key-data: Q0xJRU5US0VZ
`

func TestRewriteKubeconfigNames(t *testing.T) {
	out, err := rewriteKubeconfigValues(sampleKubeconfig, &v1beta1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "wl1", Namespace: "default"}})
	if err != nil {
		t.Fatalf("rewriteKubeconfigValues returned error: %v", err)
	}

	gotCfg, err := clientcmd.Load([]byte(out))
	if err != nil {
		t.Fatalf("failed to load processed kubeconfig: %v", err)
	}
	wantCfg, err := clientcmd.Load([]byte(expectedKubeconfig))
	if err != nil {
		t.Fatalf("failed to load expected kubeconfig: %v", err)
	}
	gotBytes, err := clientcmd.Write(*gotCfg)
	if err != nil {
		t.Fatalf("failed to serialize got kubeconfig: %v", err)
	}
	wantBytes, err := clientcmd.Write(*wantCfg)
	if err != nil {
		t.Fatalf("failed to serialize expected kubeconfig: %v", err)
	}
	if string(gotBytes) != string(wantBytes) {
		t.Fatalf("kubeconfig mismatch:\nGot:\n%s\nWant:\n%s", string(gotBytes), string(wantBytes))
	}
}
