//go:build e2e

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

package e2e

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/k0sproject/k0s/inttest/common"
	"github.com/k0sproject/k0smotron/v2/e2e/util"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	capiframework "sigs.k8s.io/cluster-api/test/framework"
	"sigs.k8s.io/cluster-api/test/framework/clusterctl"
	capiutil "sigs.k8s.io/cluster-api/util"
)

// Environment overrides. All of these are auto-discovered from EC2 IMDS when
// the test runs on an EC2 instance (the expected CI topology: the kind
// management/host cluster runs on an EC2 instance in the same VPC as the
// AWS-hosted Windows/Linux workers). They only need to be set explicitly for
// non-EC2 / local runs, or to override the auto-discovered values.
const (
	// envHostIP overrides the host IP baked into the ingress nip.io hostnames.
	// When unset, the host's INTERNAL (private) IPv4 is read from IMDS -- the
	// workers are in the same VPC and reach the host over that private IP.
	envHostIP = "E2E_HOST_IP"
	// envVPCID / envSubnetID / envAZ override the same-VPC placement of the
	// worker machines. When unset they are read from IMDS so CAPA creates the
	// workers in the SAME VPC/subnet/AZ as the host (required for the host's
	// private IP to be reachable from the workers).
	envVPCID    = "E2E_AWS_VPC_ID"
	envSubnetID = "E2E_AWS_SUBNET_ID"
	envAZ       = "E2E_AWS_AVAILABILITY_ZONE"
	// envSkipSGSetup disables the automatic security-group ingress-rule
	// management (authorize on setup / revoke on cleanup).
	envSkipSGSetup = "E2E_SKIP_SG_SETUP"

	// windowsIngressTestName is the e2e spec/interval name for this test, and
	// also the "-flavor" name of the template registered in e2e/config/aws.yaml.
	windowsIngressTestName = "windows-ingress"

	// The k0smotron ingress feature is only supported starting with this k0s
	// version (see api/k0smotron.io/v1beta2/k0smotroncluster_types.go,
	// ingressCompatibleVersions). We intentionally do NOT reuse the shared
	// KUBERNETES_VERSION e2e config variable (default "v1.31.0" in
	// e2e/config/aws.yaml) because that default predates ingress support and
	// would fail the K0smotronControlPlane's admission validation.
	defaultIngressKubernetesVersion = "v1.36.2"

	// ingressPortValue must match the HostPort kind is configured with in
	// e2e/setup.go (ExtraPortMappings) AND the security-group rule this test
	// opens automatically so the AWS workers can reach it.
	ingressPortValue = "32143"

	imdsBase = "http://169.254.169.254"
)

func TestWindowsIngressProvisioning(t *testing.T) {
	setupAndRun(t, windowsIngressProvisioningSpec)
}

// windowsIngressProvisioningSpec validates that a Windows worker node on AWS
// can reach a hosted (mgmt-cluster) K0smotronControlPlane through the
// management cluster's ingress front door, and that the resulting workload
// cluster deploys AND uses both flavors of the node-local Traefik proxy
// DaemonSet (k0smotron-proxy for Linux, k0smotron-proxy-win for Windows).
//
// The test self-configures for its EC2 topology via IMDS: it discovers the
// host's private IP (for the ingress hostnames), the host's VPC/subnet/AZ (so
// the workers land in the same VPC and can reach that private IP), and the
// host's security group + VPC CIDR (to open/close the ingress port
// automatically). See detectHostInternalIP / discoverEC2HostInfo /
// openIngressPortOnHostSG below.
func windowsIngressProvisioningSpec(t *testing.T) {
	testName := windowsIngressTestName

	namespace, _ := util.SetupSpecNamespace(ctx, testName, bootstrapClusterProxy, artifactFolder)

	clusterName := fmt.Sprintf("%s-%s", testName, capiutil.RandomString(6))

	// A SSH key is not strictly needed to reach the cluster over the ingress
	// path, but it is useful for debugging directly on the EC2 instances.
	sshPublicKey := e2eConfig.MustGetVariable(SSHPublicKey)
	if sshPublicKey == "" {
		t.Fatal("SSH public key is not set")
	}
	sshKeyName := e2eConfig.MustGetVariable(SSHKeyName)
	if sshKeyName == "" {
		t.Fatal("SSH key name is not set")
	}

	// Discover EC2 host networking (private IP, VPC, subnet, AZ, VPC CIDR,
	// security groups) from IMDS. ok=false means IMDS was unavailable (e.g. a
	// non-EC2 run) -- in that case everything must be supplied via env vars.
	hostInfo, ok := discoverEC2HostInfo(t)
	if !ok {
		t.Log("EC2 IMDS unavailable; relying entirely on E2E_* environment overrides")
		hostInfo = &ec2HostInfo{}
	}

	// Host IP for the ingress nip.io hostnames: E2E_HOST_IP wins, else the
	// host's private IPv4 from IMDS.
	hostIP := detectHostInternalIP(t)
	require.NotEmpty(t, hostIP, "host IP could not be determined; set %s or run on EC2 (IMDS)", envHostIP)

	vpcID := firstNonEmpty(os.Getenv(envVPCID), hostInfo.vpcID)
	subnetID := firstNonEmpty(os.Getenv(envSubnetID), hostInfo.subnetID)
	az := firstNonEmpty(os.Getenv(envAZ), hostInfo.availabilityZone)
	require.NotEmpty(t, vpcID, "VPC id could not be determined; set %s or run on EC2 (IMDS)", envVPCID)
	require.NotEmpty(t, subnetID, "subnet id could not be determined; set %s or run on EC2 (IMDS)", envSubnetID)
	require.NotEmpty(t, az, "availability zone could not be determined; set %s or run on EC2 (IMDS)", envAZ)

	t.Logf("EC2 host networking: privateIP=%s vpc=%s subnet=%s az=%s cidr=%s sgs=%v",
		hostIP, vpcID, subnetID, az, hostInfo.vpcCIDR, hostInfo.securityGroupIDs)

	// Automatically open the ingress port on the host's security group from
	// the VPC CIDR so the same-VPC workers can reach the ingress front door,
	// and register the revoke as cleanup. No-ops gracefully if IMDS didn't
	// yield SG/CIDR data or if E2E_SKIP_SG_SETUP is set.
	revokeSG := openIngressPortOnHostSG(t, hostInfo, ingressPortValue)
	defer revokeSG()

	// Install the management-cluster ingress front door (HAProxy, SSL
	// passthrough). This is the exact same controller/manifest used by
	// ingress_test.go for the docker "ingress" flavor -- reused here as-is.
	installHAProxyIngress(t, bootstrapClusterProxy)

	kubernetesVersion := ensureK0sVersionSuffix(defaultIngressKubernetesVersion)

	workloadClusterTemplate := clusterctl.ConfigCluster(ctx, clusterctl.ConfigClusterInput{
		ClusterctlConfigPath: clusterctlConfigPath,
		KubeconfigPath:       bootstrapClusterProxy.GetKubeconfigPath(),
		Flavor:               "windows-ingress",
		Namespace:            namespace.Name,
		ClusterName:          clusterName,
		KubernetesVersion:    e2eConfig.MustGetVariable(KubernetesVersion),
		// CAPD doesn't support windows, so we hardcode AWS as infrastructure provider.
		InfrastructureProvider: "aws",
		LogFolder:              filepath.Join(artifactFolder, "clusters", bootstrapClusterProxy.GetName()),
		ClusterctlVariables: map[string]string{
			"CLUSTER_NAME":          clusterName,
			"NAMESPACE":             namespace.Name,
			"SSH_PUBLIC_KEY":        sshPublicKey,
			"SSH_KEY_NAME":          sshKeyName,
			"KUBERNETES_VERSION":    kubernetesVersion,
			"HOST_IP":               hostIP,
			"INGRESS_PORT":          ingressPortValue,
			"AWS_VPC_ID":            vpcID,
			"AWS_SUBNET_ID":         subnetID,
			"AWS_AVAILABILITY_ZONE": az,
		},
	})
	require.NotNil(t, workloadClusterTemplate)

	fmt.Println(string(workloadClusterTemplate))

	require.Eventually(t, func() bool {
		return bootstrapClusterProxy.CreateOrUpdate(ctx, workloadClusterTemplate) == nil
	}, 10*time.Second, 1*time.Second, "Failed to apply the cluster template")

	cluster, err := util.DiscoveryAndWaitForCluster(ctx, capiframework.DiscoveryAndWaitForClusterInput{
		Getter:    bootstrapClusterProxy.GetClient(),
		Namespace: namespace.Name,
		Name:      clusterName,
	}, util.GetInterval(e2eConfig, testName, "wait-cluster"))
	require.NoError(t, err)

	defer func() {
		util.DumpSpecResourcesAndCleanup(
			ctx,
			testName,
			bootstrapClusterProxy,
			artifactFolder,
			namespace,
			cancelWatches,
			cluster,
			util.GetInterval(e2eConfig, testName, "wait-delete-cluster"),
			skipCleanup,
			clusterctlConfigPath,
		)
	}()

	// The control plane is HOSTED (runs as pods in the mgmt cluster), so we
	// wait on it the same way ingress_test.go does, not via
	// DiscoveryAndWaitForControlPlaneInitialized (which is for CAPA-managed,
	// machine-based control planes like the plain "windows" flavor).
	_, err = util.DiscoveryAndWaitForHCPToBeReady(ctx, util.DiscoveryAndWaitForHCPReadyInput{
		Cluster: cluster,
		Lister:  bootstrapClusterProxy.GetClient(),
		Getter:  bootstrapClusterProxy.GetClient(),
	}, util.GetInterval(e2eConfig, testName, "wait-controllers"))
	require.NoError(t, err)
	fmt.Println("Hosted control plane is ready")

	waitMachineInterval := util.GetInterval(e2eConfig, testName, "wait-machines")
	err = util.WaitForWorkerMachine(ctx, util.WaitForWorkersMachineInput{
		Lister:                   bootstrapClusterProxy.GetClient(),
		Namespace:                namespace.Name,
		ExpectedWorkers:          2, // 1 Windows + 1 Linux
		ClusterName:              clusterName,
		WaitForMachinesIntervals: waitMachineInterval,
	})
	require.NoError(t, err)
	fmt.Println("Worker nodes (Windows + Linux) are ready!")

	// From here on we talk to the WORKLOAD cluster (the hosted cluster the
	// Windows/Linux machines just joined), through the same NodePort trick
	// ingress_test.go uses: the K0smotronControlPlane's Service is a NodePort
	// backed by the kind management cluster's ExtraPortMapping for 30443
	// (see e2e/setup.go), so "localhost:30443" from the machine running the
	// test process reaches it directly, bypassing the ingress entirely. The
	// ingress path itself is what the AWS workers use to join/operate; this
	// is just how the *test* observes the resulting workload cluster.
	workloadCluster := bootstrapClusterProxy.GetWorkloadCluster(ctx, namespace.Name, clusterName, capiframework.WithRESTConfigModifier(func(config *rest.Config) {
		config.Host = "https://localhost:30443"
	}))
	wcs, err := kubernetes.NewForConfig(workloadCluster.GetRESTConfig())
	require.NoError(t, err, "Should get workload clientset")

	fmt.Println("Waiting for konnectivity-agent DaemonSet")
	require.NoError(t, common.WaitForDaemonSet(ctx, wcs, "konnectivity-agent"))

	// (b) Both flavors of the node-local Traefik proxy DaemonSet must exist
	// and be fully ready: k0smotron-proxy (Linux) and k0smotron-proxy-win
	// (Windows), both in the "default" namespace. This is the primary
	// Windows-specific assertion of this test.
	fmt.Println("Waiting for k0smotron-proxy (Linux) DaemonSet to be ready")
	waitForNodeLocalProxyDaemonSet(t, wcs, "k0smotron-proxy")
	fmt.Println("Waiting for k0smotron-proxy-win (Windows) DaemonSet to be ready")
	waitForNodeLocalProxyDaemonSet(t, wcs, "k0smotron-proxy-win")

	// (c) The "kubernetes" Service (which the node-local proxy DaemonSets
	// replace the backing of, see internal/controller/k0smotron.io/k0smotroncluster_ingress.go)
	// must have an Endpoints entry that traces back to the Windows node.
	windowsNodes, err := wcs.CoreV1().Nodes().List(ctx, metav1.ListOptions{
		LabelSelector: "kubernetes.io/os=windows",
	})
	require.NoError(t, err, "Should list Windows nodes")
	require.NotEmpty(t, windowsNodes.Items, "Expected at least one Windows node")
	windowsNodeName := windowsNodes.Items[0].Name
	t.Logf("Windows node name: %s", windowsNodeName)

	require.Eventually(t, func() bool {
		endpoints, err := wcs.CoreV1().Endpoints("default").Get(ctx, "kubernetes", metav1.GetOptions{})
		if err != nil {
			t.Logf("waiting for kubernetes Endpoints: %v", err)
			return false
		}
		for _, subset := range endpoints.Subsets {
			for _, addr := range subset.Addresses {
				if addr.NodeName != nil && *addr.NodeName == windowsNodeName {
					return true
				}
			}
		}
		return false
	}, 5*time.Minute, 10*time.Second, "kubernetes Service Endpoints never included the Windows node's address")
	fmt.Println("kubernetes Service Endpoints include the Windows node")

	// (d) In-node API reachability from the Windows node itself.
	//
	// The docker-based `docker exec <machine> curl ...` trick from
	// ingress_test.go does not work here: these are real EC2 instances, not
	// docker containers on the host running the test. Instead we schedule a
	// short-lived verification Pod pinned to the Windows node via
	// nodeSelector and have it curl the "kubernetes" Service's ClusterIP.
	//
	// Tradeoff/why this approach: Windows *process-isolated* containers
	// require the container base image's build to match the host's Windows
	// Server build (unlike Linux containers). We cannot know from here which
	// Windows Server release the AMI (ami-0bc74d0ac37f50b4b) actually is, so
	// the image tag below is a best-effort guess that the human MUST verify.
	// If it turns out to be wrong (pod stuck in ImagePullBackOff/RunContainerError),
	// the fallback described in the task is to instead just assert that a
	// Windows Pod can schedule and reach Running/Ready at all (proving the
	// Windows kubelet + CNI + kube-proxy path works), without asserting on the
	// curl output specifically -- see the comment further down.
	//
	// For the CA verification we deliberately use curl's "--insecure"/-k flag
	// against the ClusterIP rather than mounting the workload cluster's CA:
	// the container-internal CA path for the new Traefik-based proxy is
	// `/etc/traefik/certs/ca.crt` (Linux mount path) -- NOT
	// `/etc/haproxy/certs/ca.crt` as used by the older docker ingress_test.go
	// (that path is specific to the retired HAProxy-based node-local proxy).
	// Wiring up a Secret volume mount with the right CA into an ad-hoc
	// verification Pod adds meaningful complexity for a smoke test whose only
	// goal is "did the request reach the apiserver through k0smotron-proxy-win
	// at all", so -k is preferred here; this does NOT validate the served
	// certificate chain, only reachability.
	kubernetesSvc, err := wcs.CoreV1().Services("default").Get(ctx, "kubernetes", metav1.GetOptions{})
	require.NoError(t, err, "Should get the kubernetes Service")
	clusterIP := kubernetesSvc.Spec.ClusterIP
	require.NotEmpty(t, clusterIP, "kubernetes Service has no ClusterIP")

	const verifyPodName = "verify-windows-node-proxy"
	verifyPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      verifyPodName,
			Namespace: "default",
		},
		Spec: corev1.PodSpec{
			NodeSelector: map[string]string{
				"kubernetes.io/os": "windows",
			},
			// Some Windows node setups taint nodes with os=windows:NoSchedule;
			// tolerate it defensively (a no-op if no such taint exists).
			Tolerations: []corev1.Toleration{
				{
					Key:      "os",
					Operator: corev1.TolerationOpEqual,
					Value:    "windows",
					Effect:   corev1.TaintEffectNoSchedule,
				},
			},
			RestartPolicy: corev1.RestartPolicyNever,
			Containers: []corev1.Container{
				{
					Name: "verify",
					// TODO(e2e-env): this tag MUST match the Windows Server
					// build of ami-0bc74d0ac37f50b4b (e.g. ltsc2019 vs
					// ltsc2022), otherwise the Pod will fail to start
					// (process-isolated Windows containers require a matching
					// kernel build). curl.exe ships built into servercore
					// since Windows Server 2019.
					Image:   "mcr.microsoft.com/windows/servercore:ltsc2022",
					Command: []string{"cmd", "/c"},
					Args: []string{
						fmt.Sprintf("curl.exe -sk -o NUL -w \"%%{http_code}\" https://%s/healthz", clusterIP),
					},
				},
			},
		},
	}
	require.NoError(t, wcs.CoreV1().Pods("default").Delete(ctx, verifyPodName, metav1.DeleteOptions{}), "cleanup of stale verify pod should not error other than NotFound")
	_, err = wcs.CoreV1().Pods("default").Create(ctx, verifyPod, metav1.CreateOptions{})
	require.NoError(t, err, "Should create the Windows verification Pod")
	defer func() {
		_ = wcs.CoreV1().Pods("default").Delete(ctx, verifyPodName, metav1.DeleteOptions{})
	}()

	// Windows images are large (multiple GB); give the pull+run generous time.
	require.Eventually(t, func() bool {
		pod, err := wcs.CoreV1().Pods("default").Get(ctx, verifyPodName, metav1.GetOptions{})
		if err != nil {
			t.Logf("waiting for verify pod: %v", err)
			return false
		}
		return pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed
	}, 15*time.Minute, 15*time.Second, "verify pod never reached a terminal phase")

	logs, err := wcs.CoreV1().Pods("default").GetLogs(verifyPodName, &corev1.PodLogOptions{}).DoRaw(ctx)
	require.NoError(t, err, "Should get logs from the verify pod")
	t.Logf("Windows verify pod output: %s", string(logs))
	require.Contains(t, string(logs), "200", "Expected the Windows node, via k0smotron-proxy-win, to reach the API server's /healthz with a 200 response")

	fmt.Println("All good")
}

// ec2HostInfo holds the EC2 networking facts discovered from IMDS.
type ec2HostInfo struct {
	privateIP        string
	vpcID            string
	subnetID         string
	availabilityZone string
	vpcCIDR          string
	securityGroupIDs []string
}

// detectHostInternalIP returns the host's INTERNAL (private) IPv4 -- the
// address the same-VPC AWS workers use to reach the ingress front door.
// E2E_HOST_IP wins if set (for non-EC2 runs); otherwise it is read from IMDSv2.
// Returns "" if neither is available.
func detectHostInternalIP(t *testing.T) string {
	if ip := os.Getenv(envHostIP); ip != "" {
		return ip
	}
	token, err := imdsToken()
	if err != nil {
		t.Logf("IMDS token unavailable, cannot auto-detect host internal IP: %v", err)
		return ""
	}
	ip, err := imdsGet(token, "/latest/meta-data/local-ipv4")
	if err != nil {
		t.Logf("IMDS local-ipv4 lookup failed: %v", err)
		return ""
	}
	return ip
}

// discoverEC2HostInfo reads the host's networking facts from IMDSv2. The
// second return value is false when IMDS is unavailable (e.g. a non-EC2 run),
// in which case the caller must rely on E2E_* env overrides. E2E_HOST_IP is
// honored for the private IP even when the rest comes from IMDS.
func discoverEC2HostInfo(t *testing.T) (*ec2HostInfo, bool) {
	token, err := imdsToken()
	if err != nil {
		t.Logf("IMDS unavailable: %v", err)
		return nil, false
	}

	info := &ec2HostInfo{}
	info.privateIP = firstNonEmpty(os.Getenv(envHostIP), imdsGetOrEmpty(token, "/latest/meta-data/local-ipv4"))
	info.availabilityZone = imdsGetOrEmpty(token, "/latest/meta-data/placement/availability-zone")

	mac, err := imdsGet(token, "/latest/meta-data/mac")
	if err != nil {
		t.Logf("IMDS mac lookup failed: %v", err)
		return info, true
	}
	base := "/latest/meta-data/network/interfaces/macs/" + mac
	info.vpcID = imdsGetOrEmpty(token, base+"/vpc-id")
	info.subnetID = imdsGetOrEmpty(token, base+"/subnet-id")
	info.vpcCIDR = imdsGetOrEmpty(token, base+"/vpc-ipv4-cidr-block")
	if sgs := imdsGetOrEmpty(token, base+"/security-group-ids"); sgs != "" {
		info.securityGroupIDs = strings.Fields(sgs)
	}

	return info, true
}

// openIngressPortOnHostSG authorizes the ingress port from the VPC CIDR on
// each of the host's security groups using the `aws` CLI, and returns a
// cleanup func that revokes them. It no-ops gracefully when the necessary IMDS
// data is missing or E2E_SKIP_SG_SETUP is set. The `aws` CLI is expected to be
// present and credentialed in the AWS CI environment.
func openIngressPortOnHostSG(t *testing.T, info *ec2HostInfo, port string) func() {
	noop := func() {}

	if os.Getenv(envSkipSGSetup) != "" {
		t.Logf("%s is set; skipping automatic security-group setup", envSkipSGSetup)
		return noop
	}
	if info == nil || info.vpcCIDR == "" || len(info.securityGroupIDs) == 0 {
		t.Log("security group / VPC CIDR not discovered from IMDS; skipping automatic security-group setup")
		return noop
	}

	opened := make([]string, 0, len(info.securityGroupIDs))
	for _, sg := range info.securityGroupIDs {
		out, err := exec.Command("aws", "ec2", "authorize-security-group-ingress",
			"--group-id", sg,
			"--protocol", "tcp",
			"--port", port,
			"--cidr", info.vpcCIDR,
		).CombinedOutput()
		if err != nil {
			// Ignore "rule already exists" (idempotent re-runs), warn otherwise.
			if strings.Contains(string(out), "InvalidPermission.Duplicate") {
				t.Logf("ingress rule already present on %s (tcp/%s from %s)", sg, port, info.vpcCIDR)
				opened = append(opened, sg)
				continue
			}
			t.Logf("WARNING: failed to authorize ingress on %s (tcp/%s from %s): %v\n%s", sg, port, info.vpcCIDR, err, string(out))
			continue
		}
		t.Logf("opened ingress on %s (tcp/%s from %s)", sg, port, info.vpcCIDR)
		opened = append(opened, sg)
	}

	return func() {
		for _, sg := range opened {
			out, err := exec.Command("aws", "ec2", "revoke-security-group-ingress",
				"--group-id", sg,
				"--protocol", "tcp",
				"--port", port,
				"--cidr", info.vpcCIDR,
			).CombinedOutput()
			if err != nil {
				t.Logf("WARNING: failed to revoke ingress on %s (tcp/%s from %s): %v\n%s", sg, port, info.vpcCIDR, err, string(out))
				continue
			}
			t.Logf("revoked ingress on %s (tcp/%s from %s)", sg, port, info.vpcCIDR)
		}
	}
}

// imdsToken fetches an IMDSv2 session token.
func imdsToken() (string, error) {
	req, err := http.NewRequest(http.MethodPut, imdsBase+"/latest/api/token", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("X-aws-ec2-metadata-token-ttl-seconds", "21600")
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("IMDS token request returned status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(body)), nil
}

// imdsGet fetches a single IMDS metadata path using the given IMDSv2 token.
func imdsGet(token, path string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, imdsBase+path, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("X-aws-ec2-metadata-token", token)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("IMDS GET %s returned status %d", path, resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(body)), nil
}

// imdsGetOrEmpty is imdsGet that returns "" instead of an error.
func imdsGetOrEmpty(token, path string) string {
	v, err := imdsGet(token, path)
	if err != nil {
		return ""
	}
	return v
}

// firstNonEmpty returns the first non-empty string from the arguments.
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// waitForNodeLocalProxyDaemonSet waits until the named DaemonSet in the
// workload cluster's "default" namespace (where k0smotron deploys the
// node-local Traefik proxy, see internal/controller/k0smotron.io/k0smotroncluster_ingress.go)
// has all of its desired replicas ready. There is no reusable helper for this
// in e2e/util or github.com/k0sproject/k0s/inttest/common (that package's
// WaitForDaemonSet hardcodes the "kube-system" namespace), so it is
// implemented here directly.
func waitForNodeLocalProxyDaemonSet(t *testing.T, wcs *kubernetes.Clientset, name string) {
	t.Helper()
	require.Eventually(t, func() bool {
		ds, err := wcs.AppsV1().DaemonSets("default").Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			t.Logf("waiting for DaemonSet default/%s: %v", name, err)
			return false
		}
		return ds.Status.DesiredNumberScheduled >= 1 && ds.Status.NumberReady == ds.Status.DesiredNumberScheduled
	}, 15*time.Minute, 10*time.Second, fmt.Sprintf("DaemonSet default/%s never became fully ready", name))
}

// ensureK0sVersionSuffix appends the "-k0s.0" suffix expected by k0smotron if
// the given version doesn't already carry a "-k0s." or "+k0s." suffix.
func ensureK0sVersionSuffix(version string) string {
	if version == "" || strings.Contains(version, "-k0s.") || strings.Contains(version, "+k0s.") {
		return version
	}
	return version + "-k0s.0"
}
