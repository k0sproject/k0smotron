package k0smotronio

import (
	"testing"

	"k8s.io/client-go/tools/clientcmd"
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
	out, err := rewriteKubeconfigNames(sampleKubeconfig, "wl1")
	if err != nil {
		t.Fatalf("rewriteKubeconfigNames returned error: %v", err)
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
