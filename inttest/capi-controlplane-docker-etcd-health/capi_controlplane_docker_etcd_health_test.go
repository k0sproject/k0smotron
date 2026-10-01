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

package capicontrolplanedockeretcdhealth

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/k0sproject/k0smotron/v2/inttest/util"

	"github.com/stretchr/testify/suite"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/json"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/cluster-api/util/conditions"

	cpv1beta2 "github.com/k0sproject/k0smotron/v2/api/controlplane/v1beta2"
)

const (
	clusterName = "docker-test-cluster"
	kcpName     = "docker-test-cluster-docker-test"

	etcdMembersAPIPath = "/apis/etcd.k0sproject.io/v1beta1/etcdmembers"
)

type CAPIControlPlaneDockerEtcdHealthSuite struct {
	suite.Suite
	client           *kubernetes.Clientset
	kmcKC            *kubernetes.Clientset
	clusterYamlsPath string
	scaleDownPath    string
	ctx              context.Context
}

func TestCAPIControlPlaneDockerEtcdHealthSuite(t *testing.T) {
	suite.Run(t, &CAPIControlPlaneDockerEtcdHealthSuite{})
}

func (s *CAPIControlPlaneDockerEtcdHealthSuite) SetupSuite() {
	kubeConfigPath := os.Getenv("KUBECONFIG")
	s.Require().NotEmpty(kubeConfigPath, "KUBECONFIG env var must be set and point to kind cluster")

	restCfg, err := clientcmd.BuildConfigFromFlags("", kubeConfigPath)
	s.Require().NoError(err)
	kubeClient, err := kubernetes.NewForConfig(restCfg)
	s.Require().NoError(err)
	s.client = kubeClient

	tmpDir := s.T().TempDir()
	s.clusterYamlsPath = tmpDir + "/cluster.yaml"
	s.Require().NoError(os.WriteFile(s.clusterYamlsPath, []byte(dockerClusterYaml), 0644))
	s.scaleDownPath = tmpDir + "/scaledown.yaml"
	s.Require().NoError(os.WriteFile(s.scaleDownPath, []byte(controlPlaneScaleDown), 0644))

	s.ctx, _ = util.NewSuiteContext(s.T())
}

func (s *CAPIControlPlaneDockerEtcdHealthSuite) TestCAPIControlPlaneDockerEtcdHealth() {
	s.apply(s.clusterYamlsPath)
	defer func() {
		keep := os.Getenv("KEEP_AFTER_TEST")
		if keep == "true" || (keep == "on-failure" && s.T().Failed()) {
			return
		}
		s.T().Log("Deleting cluster objects")
		s.Require().NoError(util.DeleteCluster(clusterName))
	}()

	s.kmcKC = s.workloadClient()

	s.T().Log("waiting for three control plane machines with a joined etcd member")
	s.Require().NoError(wait.PollUntilContextCancel(s.ctx, 5*time.Second, true, func(ctx context.Context) (bool, error) {
		members, err := s.etcdMembers(ctx, s.kmcKC)
		if err != nil {
			return false, nil
		}
		if len(members) != 3 {
			return false, nil
		}
		for _, joined := range members {
			if !joined {
				return false, nil
			}
		}

		return true, nil
	}), "three etcd members should join")

	s.T().Log("every machine should report its member healthy")
	s.Require().NoError(s.waitForEtcdCondition("", metav1.ConditionTrue),
		"the condition has to be published for every machine once its member is joined")

	// Deliberately not the oldest machine, so a later deletion cannot be explained by age.
	broken := s.machineNames()[1]
	s.T().Logf("making the etcd member of %s leave", broken)
	s.Require().NoError(s.markMemberToLeave(s.kmcKC, broken))

	s.T().Log("the machine whose member left should report it")
	s.Require().NoError(s.waitForEtcdCondition(broken, metav1.ConditionFalse),
		"a member that has left has to surface on the machine")

	// k0s clears spec.leave and rejoins the member when k0scontroller restarts, so the node
	// is stopped once the member is out and cannot come back.
	s.T().Logf("stopping %s so its member cannot rejoin", broken)
	out, err := exec.Command("docker", "stop", broken).CombinedOutput()
	s.Require().NoError(err, "failed to stop %s: %s", broken, string(out))

	s.T().Log("the other members should still report healthy")
	for _, name := range s.machineNames() {
		if name == broken {
			continue
		}
		s.Require().NoError(s.waitForEtcdCondition(name, metav1.ConditionTrue),
			"only the machine whose member left may report unhealthy")
	}

	// The election is covered in process instead, since a member cannot be held out long
	// enough here, k0s rejoins it while the node is up and the list stops reading once down.
	s.T().Log("the control plane should still scale down with a member missing")
	s.apply(s.scaleDownPath)

	s.Require().NoError(wait.PollUntilContextTimeout(s.ctx, 10*time.Second, 15*time.Minute, true,
		func(ctx context.Context) (bool, error) {
			machines, err := util.GetControlPlaneMachinesByKcpName(ctx, kcpName, "default", s.client)
			if err != nil {
				return false, nil
			}

			return len(machines) == 2, nil
		}), "a control plane with a member missing still has to reach the replica count")
}

func (s *CAPIControlPlaneDockerEtcdHealthSuite) waitForEtcdCondition(name string, want metav1.ConditionStatus) error {
	return wait.PollUntilContextTimeout(s.ctx, 5*time.Second, 10*time.Minute, true, func(ctx context.Context) (bool, error) {
		machines, err := util.GetControlPlaneMachinesByKcpName(ctx, kcpName, "default", s.client)
		if err != nil || len(machines) == 0 {
			return false, nil
		}

		matched := 0
		for _, m := range machines {
			if name != "" && m.Name != name {
				continue
			}
			matched++

			got := conditions.Get(&m, cpv1beta2.K0sControlPlaneMachineEtcdMemberHealthyCondition)
			if got == nil || got.Status != want {
				return false, nil
			}
		}

		// Nothing matched means nothing was checked, which must not read as success.
		return matched > 0, nil
	})
}

// etcdMembers returns each member name against whether it is still joined.
func (s *CAPIControlPlaneDockerEtcdHealthSuite) etcdMembers(ctx context.Context, kmcKC *kubernetes.Clientset) (map[string]bool, error) {
	raw, err := kmcKC.RESTClient().Get().AbsPath(etcdMembersAPIPath).DoRaw(ctx)
	if err != nil {
		return nil, err
	}

	var list struct {
		Items []struct {
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
			Status struct {
				Conditions []struct {
					Type   string `json:"type"`
					Status string `json:"status"`
				} `json:"conditions"`
			} `json:"status"`
		} `json:"items"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, err
	}

	members := make(map[string]bool, len(list.Items))
	for _, item := range list.Items {
		for _, c := range item.Status.Conditions {
			if c.Type == "Joined" {
				members[item.Metadata.Name] = c.Status == string(metav1.ConditionTrue)
			}
		}
	}

	return members, nil
}

// markMemberToLeave is what k0smotron itself patches when scaling a machine down. k0s
// takes the member out and stops k0scontroller on that node, so Joined goes to False.
func (s *CAPIControlPlaneDockerEtcdHealthSuite) markMemberToLeave(kmcKC *kubernetes.Clientset, name string) error {
	return kmcKC.RESTClient().
		Patch(types.MergePatchType).
		AbsPath(etcdMembersAPIPath + "/" + name).
		Body([]byte(`{"spec":{"leave":true}}`)).
		Do(s.ctx).
		Error()
}

func (s *CAPIControlPlaneDockerEtcdHealthSuite) machineNames() []string {
	machines, err := util.GetControlPlaneMachinesByKcpName(s.ctx, kcpName, "default", s.client)
	s.Require().NoError(err)
	s.Require().Len(machines, 3)

	slices.SortStableFunc(machines, func(a, b clusterv1.Machine) int {
		return a.CreationTimestamp.Time.Compare(b.CreationTimestamp.Time)
	})

	names := make([]string, 0, len(machines))
	for _, m := range machines {
		names = append(names, m.Name)
	}

	return names
}

func (s *CAPIControlPlaneDockerEtcdHealthSuite) workloadClient() *kubernetes.Clientset {
	var localPort int
	s.Require().NoError(wait.PollUntilContextCancel(s.ctx, 1*time.Second, true, func(_ context.Context) (bool, error) {
		localPort, _ = getLBPort(clusterName + "-lb")

		return localPort > 0, nil
	}))

	s.T().Log("waiting to see admin kubeconfig secret")
	kmcKC, err := util.GetKMCClientSet(s.ctx, s.client, clusterName, "default", localPort)
	s.Require().NoError(err)

	s.Require().NoError(wait.PollUntilContextCancel(s.ctx, 1*time.Second, true, func(ctx context.Context) (bool, error) {
		b, _ := kmcKC.RESTClient().Get().AbsPath("/healthz").DoRaw(ctx)

		return string(b) == "ok", nil
	}))

	return kmcKC
}

func (s *CAPIControlPlaneDockerEtcdHealthSuite) apply(path string) {
	out, err := exec.Command("kubectl", "apply", "-f", path).CombinedOutput()
	s.Require().NoError(err, "failed to apply %s: %s", path, string(out))
}

func getLBPort(name string) (int, error) {
	b, err := exec.Command("docker", "inspect", name, "--format", "{{json .NetworkSettings.Ports}}").Output()
	if err != nil {
		return 0, fmt.Errorf("failed to get inspect info from container %s: %w", name, err)
	}

	var ports map[string][]map[string]string
	if err := json.Unmarshal(b, &ports); err != nil {
		return 0, fmt.Errorf("failed to unmarshal inspect info from container %s: %w", name, err)
	}

	return strconv.Atoi(ports["6443/tcp"][0]["HostPort"])
}

var dockerClusterYaml = `
apiVersion: cluster.x-k8s.io/v1beta1
kind: Cluster
metadata:
  name: docker-test-cluster
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
  controlPlaneRef:
    apiVersion: controlplane.cluster.x-k8s.io/v1beta1
    kind: K0sControlPlane
    name: docker-test-cluster-docker-test
  infrastructureRef:
    apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
    kind: DevCluster
    name: docker-test
---
apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
kind: DevMachineTemplate
metadata:
  name: docker-test-cp-template
  namespace: default
spec:
  template:
    spec:
      backend:
        docker:
          customImage: kindest/node:v1.31.0
---
apiVersion: controlplane.cluster.x-k8s.io/v1beta1
kind: K0sControlPlane
metadata:
  name: docker-test-cluster-docker-test
spec:
  replicas: 3
  version: v1.32.2+k0s.0
  k0sConfigSpec:
    k0s:
      apiVersion: k0s.k0sproject.io/v1beta1
      kind: ClusterConfig
      metadata:
        name: k0s
      spec:
        api:
          extraArgs:
            anonymous-auth: "true"
        telemetry:
          enabled: false
  machineTemplate:
    infrastructureRef:
      apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
      kind: DevMachineTemplate
      name: docker-test-cp-template
      namespace: default
---
apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
kind: DevCluster
metadata:
  name: docker-test
  namespace: default
spec:
  backend:
    docker:
      loadBalancer:
        customHAProxyConfigTemplateRef:
          name: ha-proxy-config
---
apiVersion: v1
data:
  value: |
    # generated by kind
    global
      log /dev/log local0
      log /dev/log local1 notice
      daemon
      # limit memory usage to approximately 18 MB
      # (see https://github.com/kubernetes-sigs/kind/pull/3115)
      maxconn 100000

    resolvers docker
      nameserver dns 127.0.0.11:53

    defaults
      log global
      mode tcp
      option dontlognull
      # TODO: tune these
      timeout connect 5000
      timeout client 50000
      timeout server 50000
      # allow to boot despite dns don't resolve backends
      default-server init-addr none

    frontend stats
      mode http
      bind *:8404
      stats enable
      stats uri /stats
      stats refresh 1s
      stats admin if TRUE

    frontend control-plane
      bind *:{{ .FrontendControlPlanePort }}
      {{ if .IPv6 -}}
      bind :::{{ .FrontendControlPlanePort }};
      {{- end }}
      default_backend kube-apiservers

    backend kube-apiservers
      default-server inter 2s fall 2 rise 3
      timeout connect 2s
      timeout server 5s
      retries 3
      option redispatch
      option httpchk GET /healthz
      {{range $server, $backend := .BackendServers}}
      server {{ $server }} {{ JoinHostPort $backend.Address $.BackendControlPlanePort }} weight {{ $backend.Weight }} check check-ssl verify none resolvers docker resolve-prefer {{ if $.IPv6 -}} ipv6 {{- else -}} ipv4 {{- end }}
      {{- end}}
kind: ConfigMap
metadata:
  name: ha-proxy-config
`

// The update changes an extra arg as well as the replica count, so every machine is
// outdated and the election decides on etcd health rather than on age.
var controlPlaneScaleDown = strings.Replace(
	strings.Replace(controlPlaneBase, "REPLICAS", "2", 1), "EXTRA_ARG", "audit-log-maxage: \"9\"", 1)

var controlPlaneBase = `
apiVersion: controlplane.cluster.x-k8s.io/v1beta1
kind: K0sControlPlane
metadata:
  name: docker-test-cluster-docker-test
spec:
  replicas: REPLICAS
  version: v1.32.2+k0s.0
  k0sConfigSpec:
    k0s:
      apiVersion: k0s.k0sproject.io/v1beta1
      kind: ClusterConfig
      metadata:
        name: k0s
      spec:
        api:
          extraArgs:
            anonymous-auth: "true"
            EXTRA_ARG
        telemetry:
          enabled: false
  machineTemplate:
    infrastructureRef:
      apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
      kind: DevMachineTemplate
      name: docker-test-cp-template
      namespace: default
`
