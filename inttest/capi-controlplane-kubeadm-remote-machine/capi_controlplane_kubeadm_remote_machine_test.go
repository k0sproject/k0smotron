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

// Package capicontrolplanekubeadmremotemachine exercises the upstream
// cluster-api kubeadm bootstrap and control plane providers against
// k0smotron's RemoteMachine infrastructure provider: a kindest/node-based
// container stands in for a pre-provisioned "bare metal" host, is
// registered as a PooledRemoteMachine, and is then picked up by a
// RemoteMachineTemplate referenced from a KubeadmControlPlane, exactly as a
// real pool of pre-provisioned hosts would be consumed.
//
// The node container is started directly with docker rather than through
// footloose/bootloose: kindest/node images need the exact same
// privileged/cgroupns/tmpfs settings Cluster API's own docker provider uses
// (see cluster-api's test/infrastructure/docker/internal/docker/manager.go),
// which footloose's machine config has no way to express.
package capicontrolplanekubeadmremotemachine

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"text/template"
	"time"

	infra "github.com/k0sproject/k0smotron/v2/api/infrastructure/v1beta2"
	"github.com/k0sproject/k0smotron/v2/inttest/util"

	"github.com/stretchr/testify/suite"
	"golang.org/x/crypto/ssh"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	clusterName   = "kubeadm-rm-test"
	kcpName       = "kubeadm-rm-test"
	nodeContainer = "kubeadm-rm-test-node"
	dockerNetwork = "kind"
)

type KubeadmRemoteMachineSuite struct {
	suite.Suite

	ctx        context.Context
	client     *kubernetes.Clientset
	restConfig *rest.Config
	privateKey []byte
	publicKey  []byte
}

func (s *KubeadmRemoteMachineSuite) SetupSuite() {
	kubeConfigPath := os.Getenv("KUBECONFIG")
	s.Require().NotEmpty(kubeConfigPath, "KUBECONFIG env var must be set and point to kind cluster")

	restCfg, err := clientcmd.BuildConfigFromFlags("", kubeConfigPath)
	s.Require().NoError(err)
	s.restConfig = restCfg

	kubeClient, err := kubernetes.NewForConfig(restCfg)
	s.Require().NoError(err)
	s.client = kubeClient

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	s.Require().NoError(err)
	privateKeyBytes := x509.MarshalPKCS1PrivateKey(privateKey)
	s.privateKey = pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: privateKeyBytes})

	sshPublicKey, err := ssh.NewPublicKey(&privateKey.PublicKey)
	s.Require().NoError(err)
	s.publicKey = ssh.MarshalAuthorizedKey(sshPublicKey)

	s.ctx, _ = util.NewSuiteContext(s.T())
}

func TestKubeadmRemoteMachineSuite(t *testing.T) {
	suite.Run(t, &KubeadmRemoteMachineSuite{})
}

func (s *KubeadmRemoteMachineSuite) TestKubeadmControlPlaneOnRemoteMachine() {
	ctx := s.ctx

	s.T().Log("starting the kubeadm-node container that stands in for a pre-provisioned host")
	s.startNodeContainer()
	defer func() {
		keep := os.Getenv("KEEP_AFTER_TEST")
		if keep == "true" {
			return
		}
		if keep == "on-failure" && s.T().Failed() {
			return
		}
		s.T().Log("deleting cluster objects")
		s.Require().NoError(util.DeleteCluster(clusterName + "-cluster"))
		s.T().Log("removing the node container")
		_ = exec.Command("docker", "rm", "-f", nodeContainer).Run()
	}()

	workerIP := s.getNodeIP()
	s.Require().NotEmpty(workerIP)

	s.T().Log("pushing public key to the node's authorized_keys")
	s.pushSSHKey()
	// Let's give the SSH service some time to start
	time.Sleep(5 * time.Second)

	s.createCluster(workerIP)

	s.T().Log("cluster objects applied, waiting for the KubeadmControlPlane to provision its RemoteMachine")

	expectedProviderID := fmt.Sprintf("remote-machine://%s:22", workerIP)
	controlPlaneMachineName := ""
	// nolint:staticcheck
	err := wait.PollImmediateUntilWithContext(ctx, 5*time.Second, func(ctx context.Context) (bool, error) {
		machines, err := util.GetControlPlaneMachinesByKcpName(ctx, kcpName, "default", s.client)
		if err != nil || len(machines) != 1 {
			return false, nil
		}
		controlPlaneMachineName = machines[0].GetName()

		rm, err := s.getRemoteMachine(controlPlaneMachineName, "default")
		if err != nil {
			s.T().Log(err)
			return false, nil
		}

		return rm.Status.Initialization.Provisioned != nil && *rm.Status.Initialization.Provisioned && expectedProviderID == rm.Spec.ProviderID, nil
	})
	s.Require().NoError(err)

	s.T().Log("waiting to see the workload cluster kubeconfig secret")
	s.Require().NoError(util.WaitForSecret(ctx, s.client, clusterName+"-cluster-kubeconfig", "default"))

	// Plain kubeadm doesn't ship a CNI, unlike k0smotron's own control plane
	// providers: the node stays NotReady (NetworkPluginNotReady) until one is
	// installed, same as kubeadm init's own on-screen instructions say.
	s.T().Log("installing the Calico CNI on the workload cluster")
	s.installCNI(ctx)

	s.T().Log("waiting for the node to be ready")
	// The Node registers under the container's own hostname, not the CAPI
	// Machine's (KCP-generated, random-suffixed) name.
	err = wait.PollUntilContextCancel(ctx, time.Second, true, func(_ context.Context) (done bool, err error) {
		node, err := s.getNode(nodeContainer)
		if err != nil {
			return false, nil
		}
		if node.Spec.ProviderID != expectedProviderID {
			return false, nil
		}
		for _, cond := range node.Status.Conditions {
			if cond.Type == corev1.NodeReady {
				return cond.Status == corev1.ConditionTrue, nil
			}
		}
		return false, nil
	})
	s.Require().NoError(err)
}

// startNodeContainer runs the kubeadm-node image with the same
// privileged/security/cgroup settings Cluster API's docker provider uses to
// run kindest/node containers, since that's what the image actually needs
// to boot systemd, containerd and kubelet correctly.
func (s *KubeadmRemoteMachineSuite) startNodeContainer() {
	_ = exec.Command("docker", "rm", "-f", nodeContainer).Run()

	args := []string{
		"run", "-d",
		"--name", nodeContainer,
		"--hostname", nodeContainer,
		"--network", dockerNetwork,
		"--privileged",
		"--security-opt", "seccomp=unconfined",
		"--security-opt", "apparmor=unconfined",
		"--cgroupns=private",
		"--tmpfs", "/tmp",
		"--tmpfs", "/run",
		"-v", "/lib/modules:/lib/modules:ro",
		"-v", "/var",
		"kubeadm-node",
	}
	out, err := exec.Command("docker", args...).CombinedOutput()
	s.Require().NoError(err, "failed to start node container: %s", string(out))
}

func (s *KubeadmRemoteMachineSuite) getNodeIP() string {
	out, err := exec.Command("docker", "inspect", "-f",
		fmt.Sprintf("{{.NetworkSettings.Networks.%s.IPAddress}}", dockerNetwork), nodeContainer).CombinedOutput()
	s.Require().NoError(err, "failed to inspect node container: %s", string(out))
	return strings.TrimSpace(string(out))
}

func (s *KubeadmRemoteMachineSuite) pushSSHKey() {
	cmd := exec.Command("docker", "exec", "-i", nodeContainer, "sh", "-c",
		"mkdir -p /root/.ssh && cat >> /root/.ssh/authorized_keys && chmod 600 /root/.ssh/authorized_keys")
	cmd.Stdin = bytes.NewReader(s.publicKey)
	out, err := cmd.CombinedOutput()
	s.Require().NoError(err, "failed to push SSH key: %s", string(out))
}

func (s *KubeadmRemoteMachineSuite) getRemoteMachine(name, namespace string) (*infra.RemoteMachine, error) {
	apiPath := fmt.Sprintf("/apis/infrastructure.cluster.x-k8s.io/v1beta2/namespaces/%s/remotemachines/%s", namespace, name)
	result, err := s.client.RESTClient().Get().AbsPath(apiPath).DoRaw(s.ctx)
	if err != nil {
		return nil, err
	}
	rm := &infra.RemoteMachine{}
	if err := yaml.Unmarshal(result, rm); err != nil {
		return nil, err
	}
	return rm, nil
}

// kubectlOnNode runs kubectl *inside* the node container against its own
// admin.conf. The workload cluster's API server is a bare process on a plain
// docker container, not a pod behind the management cluster's API server, so
// there is nothing to port-forward through; and its address is the
// container's own IP on the "kind" docker network, which is only reachable
// from inside that network (e.g. from k0smotron's own controller pods) or
// via `docker exec` — not necessarily from wherever `go test` itself runs.
// Docker Desktop (macOS/Windows) in particular does not route the host to
// arbitrary container IPs the way a native Linux docker host does.
func (s *KubeadmRemoteMachineSuite) kubectlOnNode(stdin []byte, args ...string) ([]byte, error) {
	fullArgs := []string{"exec"}
	if stdin != nil {
		fullArgs = append(fullArgs, "-i")
	}
	fullArgs = append(fullArgs, nodeContainer, "kubectl", "--kubeconfig=/etc/kubernetes/admin.conf")
	fullArgs = append(fullArgs, args...)

	cmd := exec.Command("docker", fullArgs...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("kubectl %v: %w: %s", args, err, out)
	}
	return out, nil
}

// installCNI downloads the Calico manifest and applies it to the workload
// cluster. Plain kubeadm clusters come with no CNI at all.
func (s *KubeadmRemoteMachineSuite) installCNI(ctx context.Context) {
	manifestURL := os.Getenv("CALICO_MANIFEST_URL")
	if manifestURL == "" {
		manifestURL = "https://raw.githubusercontent.com/projectcalico/calico/v3.28.2/manifests/calico.yaml"
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, manifestURL, nil)
	s.Require().NoError(err)
	resp, err := http.DefaultClient.Do(req)
	s.Require().NoError(err)
	defer resp.Body.Close()
	s.Require().Equal(http.StatusOK, resp.StatusCode, "failed to download Calico manifest from %s", manifestURL)

	manifest, err := io.ReadAll(resp.Body)
	s.Require().NoError(err)

	_, err = s.kubectlOnNode(manifest, "apply", "-f", "-")
	s.Require().NoError(err)
}

// getNode fetches the workload cluster's Node object by shelling out to
// kubectl on the node itself, see kubectlOnNode.
func (s *KubeadmRemoteMachineSuite) getNode(name string) (*corev1.Node, error) {
	out, err := s.kubectlOnNode(nil, "get", "node", name, "-o", "json")
	if err != nil {
		return nil, err
	}
	node := &corev1.Node{}
	if err := json.Unmarshal(out, node); err != nil {
		return nil, err
	}
	return node, nil
}

func (s *KubeadmRemoteMachineSuite) createCluster(workerIP string) {
	t, err := template.New("cluster").Parse(clusterYaml)
	s.Require().NoError(err)

	k8sVersion := os.Getenv("K8S_VERSION")
	s.Require().NotEmpty(k8sVersion, "K8S_VERSION env var must be set to the kubeadm/kubelet version baked into the kubeadm-node image")

	var clusterYamlBuf bytes.Buffer
	err = t.Execute(&clusterYamlBuf, struct {
		Address    string
		SSHKey     string
		K8SVersion string
	}{
		Address:    workerIP,
		SSHKey:     base64.StdEncoding.EncodeToString(s.privateKey),
		K8SVersion: k8sVersion,
	})
	s.Require().NoError(err)

	tmpDir := s.T().TempDir()
	clusterYamlPath := tmpDir + "/cluster.yaml"
	s.Require().NoError(os.WriteFile(clusterYamlPath, clusterYamlBuf.Bytes(), 0644))
	out, err := exec.Command("kubectl", "apply", "-f", clusterYamlPath).CombinedOutput()
	s.Require().NoError(err, "failed to apply cluster objects: %s", string(out))
}

// The bootstrap and control-plane objects below (KubeadmControlPlane,
// kubeadmConfigSpec) come entirely from the upstream kubeadm providers;
// only the infrastructureRef (RemoteMachineTemplate/PooledRemoteMachine) is
// k0smotron-specific. ignorePreflightErrors/preKubeadmCommands mirror what
// Cluster API's own docker provider (CAPD) uses to run kubeadm inside a
// container acting as a node.
var clusterYaml = `
apiVersion: controlplane.cluster.x-k8s.io/v1beta1
kind: KubeadmControlPlane
metadata:
  name: ` + kcpName + `
  namespace: default
spec:
  replicas: 1
  version: {{ .K8SVersion }}
  machineTemplate:
    infrastructureRef:
      apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
      kind: RemoteMachineTemplate
      name: ` + kcpName + `-cp-template
      namespace: default
  kubeadmConfigSpec:
    clusterConfiguration: {}
    initConfiguration:
      nodeRegistration: &nodeRegistration
        criSocket: unix:///var/run/containerd/containerd.sock
        kubeletExtraArgs:
          # The kubelet sees the docker host's swap/disk, not the container's
          # own, so these can't be trusted the way they would be on a real
          # node. Same fixups CAPD applies for kindest/node containers, see
          # sigs.k8s.io/cluster-api/test/infrastructure/docker/internal/provisioning/cloudinit/writefiles.go.
          fail-swap-on: "false"
          image-gc-high-threshold: "100"
          eviction-hard: "nodefs.available<0%,nodefs.inodesFree<0%,imagefs.available<0%"
          # required because the node container runs with --cgroupns=private,
          # same as kindest/node images from kind >=0.20.
          cgroup-root: "/kubelet"
          runtime-cgroups: "/system.slice/containerd.service"
        ignorePreflightErrors:
        - SystemVerification
        - Swap
        - FileContent--proc-sys-net-bridge-bridge-nf-call-iptables
    joinConfiguration:
      nodeRegistration: *nodeRegistration
    preKubeadmCommands:
    - 'modprobe overlay || true'
    - 'modprobe br_netfilter || true'
    - 'sysctl -w net.bridge.bridge-nf-call-iptables=1'
    - 'sysctl -w net.bridge.bridge-nf-call-ip6tables=1'
    - 'sysctl -w net.ipv4.ip_forward=1'
    # kube-proxy's default conntrack settings try to set the (host-wide,
    # non-namespaced) net.netfilter.nf_conntrack_max sysctl, which fails with
    # "permission denied" from inside a container. maxPerCore: 0 makes
    # kube-proxy skip that write entirely. This appends a KubeProxyConfiguration
    # document to the kubeadm.yaml the files phase already wrote, since kubeadm
    # accepts multiple YAML documents in one --config file. CAPD does the same
    # thing for its own kindest/node machines (see cluster-api's
    # test/infrastructure/docker/internal/provisioning/cloudinit/writefiles.go);
    # our SSH provisioner refuses append:true files, so it's done as a command
    # here instead of via kubeadmConfigSpec.files.
    - |
      cat >> /run/kubeadm/kubeadm.yaml <<'EOF'
      ---
      apiVersion: kubeproxy.config.k8s.io/v1alpha1
      kind: KubeProxyConfiguration
      conntrack:
        maxPerCore: 0
      EOF
---
apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
kind: RemoteCluster
metadata:
  name: ` + clusterName + `
  namespace: default
spec:
---
apiVersion: cluster.x-k8s.io/v1beta1
kind: Cluster
metadata:
  name: ` + clusterName + `-cluster
  namespace: default
spec:
  clusterNetwork:
    pods:
      cidrBlocks:
      - 192.168.0.0/16
    serviceDomain: cluster.local
    services:
      cidrBlocks:
      - 10.128.0.0/12
  controlPlaneEndpoint:
    host: {{ .Address }}
    port: 6443
  controlPlaneRef:
    apiVersion: controlplane.cluster.x-k8s.io/v1beta1
    kind: KubeadmControlPlane
    name: ` + kcpName + `
  infrastructureRef:
    apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
    kind: RemoteCluster
    name: ` + clusterName + `
---
apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
kind: RemoteMachineTemplate
metadata:
  name: ` + kcpName + `-cp-template
  namespace: default
spec:
  template:
    spec:
      pool: default
---
apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
kind: PooledRemoteMachine
metadata:
  name: ` + clusterName + `-0
  namespace: default
spec:
  pool: default
  machine:
    address: {{ .Address }}
    port: 22
    user: root
    sshKeyRef:
      name: kubeadm-rm-test-key
---
apiVersion: v1
kind: Secret
metadata:
  name: kubeadm-rm-test-key
  namespace: default
data:
  value: {{ .SSHKey }}
type: Opaque
`
