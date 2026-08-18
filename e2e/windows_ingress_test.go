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
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/k0sproject/k0s/inttest/common"
	"github.com/k0sproject/k0smotron/v2/e2e/util"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/kubernetes"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	capiframework "sigs.k8s.io/cluster-api/test/framework"
	"sigs.k8s.io/cluster-api/test/framework/clusterctl"
	capiutil "sigs.k8s.io/cluster-api/util"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// envHostIP overrides the IP baked into the ingress nip.io hostnames. It is
	// discovered from the hosting cluster's control plane Machine (its public
	// IPv4) and only needs to be set when that address is not the one both the
	// AWS workers and the test process should use to reach the ingress.
	envHostIP = "E2E_HOST_IP"

	// windowsIngressTestName is the e2e spec/interval name for this test, and
	// also the "-flavor" name of the workload cluster template registered in
	// e2e/config/aws.yaml.
	windowsIngressTestName = "windows-ingress"

	// windowsIngressHostFlavor is the template flavor of the AWS *hosting*
	// cluster this spec creates first: a plain machine-based k0s cluster whose
	// single node runs the workload cluster's hosted control plane and the
	// HAProxy ingress front door.
	windowsIngressHostFlavor = "windows-ingress-host"

	// hostClusterK0sVersion is the k0s version of the hosting cluster, passed as
	// ${KUBERNETES_VERSION} to the "windows-ingress-host" template. It has
	// nothing to do with the workload cluster's version below; it only has to
	// be a version that runs HAProxy and the hosted control plane pods, so the
	// same version the plain "windows" flavor already uses is reused here.
	hostClusterK0sVersion = "v1.34.2+k0s.0"

	// The k0smotron ingress feature is only supported starting with this k0s
	// version (see api/k0smotron.io/v1beta2/k0smotroncluster_types.go,
	// ingressCompatibleVersions). We intentionally do NOT reuse the shared
	// KUBERNETES_VERSION e2e config variable (default "v1.31.0" in
	// e2e/config/aws.yaml) because that default predates ingress support and
	// would fail the K0smotronControlPlane's admission validation.
	defaultIngressKubernetesVersion = "v1.36.2"

	// ingressPortValue is the NodePort the HAProxy ingress controller is
	// exposed on (see e2e/data/haproxy-ingress.yaml). The hosting cluster's
	// AWSCluster opens the very same port on its control plane security group.
	ingressPortValue = "32143"

	// The HAProxy ingress controller Deployment installed into the hosting
	// cluster by installHAProxyIngress (e2e/data/haproxy-ingress.yaml).
	haproxyDeploymentName      = "haproxy-kubernetes-ingress"
	haproxyDeploymentNamespace = "haproxy-controller"
)

func TestWindowsIngressProvisioning(t *testing.T) {
	setupAndRun(t, windowsIngressProvisioningSpec)
}

// windowsIngressProvisioningSpec validates that a Windows worker node on AWS
// can reach a hosted K0smotronControlPlane through an ingress front door, and
// that the resulting workload cluster deploys AND uses both flavors of the
// node-local Traefik proxy DaemonSet (k0smotron-proxy for Linux,
// k0smotron-proxy-win for Windows).
//
// Topology. Everything the AWS workers must reach lives in AWS:
//
//	kind (this machine)   CAPI + CAPA + k0smotron controllers only. Talks
//	                      outbound to AWS; nothing dials back into it.
//	hosting cluster       A machine-based k0s cluster on EC2 created by this
//	                      spec ("windows-ingress-host" flavor). Runs the
//	                      HAProxy ingress front door on NodePort 32143 and,
//	                      via remoteHostCluster.kubeconfigRef, the workload
//	                      cluster's hosted control plane pods.
//	workload cluster      K0smotronControlPlane hosted in the cluster above,
//	                      plus one Windows and one Linux worker created in the
//	                      hosting cluster's VPC. They join through
//	                      kube-api.<hosting node public IP>.nip.io:32143.
//
// This is deliberately NOT the earlier topology, where the hosted control plane
// ran in the local kind cluster and the AWS workers connected back to the
// machine running the test. That required the test process to run on an EC2
// instance inside the workers' VPC (host IP, VPC, subnet and security group
// were read from EC2 IMDS), which does not hold for the CI runners: they are
// not EC2 instances, so the IMDSv2 token request was answered by a foreign
// metadata service with HTTP 405 and nothing could be discovered.
func windowsIngressProvisioningSpec(t *testing.T) {
	testName := windowsIngressTestName

	namespace, _ := util.SetupSpecNamespace(ctx, testName, bootstrapClusterProxy, artifactFolder)

	// A SSH key is not strictly needed to reach the clusters over the ingress
	// path, but it is useful for debugging directly on the EC2 instances.
	sshPublicKey := e2eConfig.MustGetVariable(SSHPublicKey)
	if sshPublicKey == "" {
		t.Fatal("SSH public key is not set")
	}
	sshKeyName := e2eConfig.MustGetVariable(SSHKeyName)
	if sshKeyName == "" {
		t.Fatal("SSH key name is not set")
	}

	hostClusterName := fmt.Sprintf("%s-host-%s", testName, capiutil.RandomString(6))
	clusterName := fmt.Sprintf("%s-%s", testName, capiutil.RandomString(6))

	// Both clusters are torn down by a single deferred func because the order
	// matters (see cleanupWindowsIngressClusters); the pointers are filled in
	// as the clusters come up so a failure half-way still cleans up what
	// exists.
	var hostCluster, workloadCluster *clusterv1.Cluster
	var hostClusterProxy capiframework.ClusterProxy
	defer func() {
		cleanupWindowsIngressClusters(t, testName, namespace, &hostCluster, &workloadCluster, &hostClusterProxy)
	}()

	// ---------------------------------------------------------------- hosting
	createWindowsIngressHostCluster(t, testName, namespace.Name, hostClusterName, sshKeyName, &hostCluster)

	// The hosting cluster's kubeconfig is the CAPI-generated
	// "<hostClusterName>-kubeconfig" Secret; its server is the CAPA-created
	// API ELB, reachable from here and from the k0smotron controllers.
	hostClusterProxy = bootstrapClusterProxy.GetWorkloadCluster(ctx, namespace.Name, hostClusterName)

	// The ingress front door. Same controller/manifest ingress_test.go uses for
	// the docker "ingress" flavor, installed into the hosting cluster instead
	// of the management cluster.
	installHAProxyIngress(t, hostClusterProxy)
	require.NoError(t, util.WaitForDeploymentsAvailable(ctx, capiframework.WaitForDeploymentsAvailableInput{
		Getter: hostClusterProxy.GetClient(),
		Deployment: &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{
			Name:      haproxyDeploymentName,
			Namespace: haproxyDeploymentNamespace,
		}},
	}, util.GetInterval(e2eConfig, testName, "wait-controllers")))
	fmt.Println("HAProxy ingress controller is available in the hosting cluster")

	// Everything the workload cluster template needs about the AWS side is read
	// back from the objects that were just created, so this spec has no
	// dependency on where the test process itself runs.
	hostIP := hostClusterIngressIP(t, namespace.Name, hostClusterName)
	hostNet := hostClusterNetwork(t, namespace.Name, hostClusterName)
	t.Logf("hosting cluster: ingressIP=%s vpc=%s subnet=%s az=%s nodeSG=%s",
		hostIP, hostNet.vpcID, hostNet.subnetID, hostNet.availabilityZone, hostNet.nodeSecurityGroupID)

	// --------------------------------------------------------------- workload
	kubernetesVersion := ensureK0sVersionSuffix(defaultIngressKubernetesVersion)
	workerK0sVersion := strings.Replace(kubernetesVersion, "-k0s.", "+k0s.", 1)

	workloadClusterTemplate := clusterctl.ConfigCluster(ctx, clusterctl.ConfigClusterInput{
		ClusterctlConfigPath:     clusterctlConfigPath,
		KubeconfigPath:           bootstrapClusterProxy.GetKubeconfigPath(),
		Flavor:                   windowsIngressTestName,
		Namespace:                namespace.Name,
		ClusterName:              clusterName,
		KubernetesVersion:        kubernetesVersion,
		ControlPlaneMachineCount: new(int64(1)),
		InfrastructureProvider:   "aws",
		LogFolder:                filepath.Join(artifactFolder, "clusters", bootstrapClusterProxy.GetName()),
		ClusterctlVariables: map[string]string{
			"CLUSTER_NAME":                   clusterName,
			"NAMESPACE":                      namespace.Name,
			"SSH_PUBLIC_KEY":                 sshPublicKey,
			"SSH_KEY_NAME":                   sshKeyName,
			"HOST_IP":                        hostIP,
			"INGRESS_PORT":                   ingressPortValue,
			"WORKER_K0S_VERSION":             workerK0sVersion,
			"AWS_VPC_ID":                     hostNet.vpcID,
			"AWS_SUBNET_ID":                  hostNet.subnetID,
			"AWS_AVAILABILITY_ZONE":          hostNet.availabilityZone,
			"AWS_NODE_SECURITY_GROUP_ID":     hostNet.nodeSecurityGroupID,
			"HOST_CLUSTER_KUBECONFIG_SECRET": fmt.Sprintf("%s-kubeconfig", hostClusterName),
		},
	})
	require.NotNil(t, workloadClusterTemplate)

	fmt.Println(string(workloadClusterTemplate))

	// Registered for cleanup before applying, for the same reason as the
	// hosting cluster above.
	workloadCluster = clusterHandle(clusterName, namespace.Name)

	applyClusterTemplate(t, "workload", workloadClusterTemplate)

	var err error
	workloadCluster, err = util.DiscoveryAndWaitForCluster(ctx, capiframework.DiscoveryAndWaitForClusterInput{
		Getter:    bootstrapClusterProxy.GetClient(),
		Namespace: namespace.Name,
		Name:      clusterName,
	}, util.GetInterval(e2eConfig, testName, "wait-cluster"))
	require.NoError(t, err)

	// The control plane is HOSTED (runs as pods in the hosting cluster), so we
	// wait on it the same way ingress_test.go does, not via
	// DiscoveryAndWaitForControlPlaneInitialized (which is for CAPA-managed,
	// machine-based control planes like the plain "windows" flavor).
	_, err = util.DiscoveryAndWaitForHCPToBeReady(ctx, util.DiscoveryAndWaitForHCPReadyInput{
		Cluster: workloadCluster,
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

	// From here on we talk to the WORKLOAD cluster. Its CAPI kubeconfig points
	// at the control plane endpoint k0smotron derived from the ingress
	// (kube-api.<hostIP>.nip.io:32143), which is exactly the path the AWS
	// workers use, so it is usable as-is from here with no port-forward or
	// address rewriting.
	workloadClusterProxy := bootstrapClusterProxy.GetWorkloadCluster(ctx, namespace.Name, clusterName)
	wcs, err := kubernetes.NewForConfig(workloadClusterProxy.GetRESTConfig())
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
	// docker containers on the host running the test. So a short-lived Pod is
	// scheduled onto the Windows node instead, and curls the API through the
	// node-local proxy.
	//
	// It is a HostProcess Pod, mirroring the k0smotron-proxy-win DaemonSet it is
	// verifying (see generateIngressManifestsSecret in
	// internal/controller/k0smotron.io/k0smotroncluster_ingress.go). That choice
	// avoids two Windows-specific traps:
	//
	//   - A process-isolated Windows container must have a base image whose
	//     build matches the host's Windows Server build, and nothing here can
	//     know which release the AMI actually is. A HostProcess container runs
	//     the process on the host and takes only files from the image, so no
	//     build matching applies -- and reusing the image the DaemonSet already
	//     runs means it is guaranteed to be pulled on this node.
	//   - HostProcess Pods are host-networked, and on Windows a Service
	//     ClusterIP is famously not reachable from the node's own network
	//     namespace. So this curls the proxy's own listener on localhost rather
	//     than the ClusterIP. That is the more direct assertion anyway: the
	//     proxy IS what k0smotron-proxy-win provides to the node, and the
	//     ClusterIP wiring is already covered by the Endpoints check above.
	//
	// curl.exe ships with Windows Server 2019 and later, so it is available as a
	// host binary. "-k" is deliberate: this is a reachability smoke test, not a
	// certificate-chain check, and mounting the workload cluster's CA into an
	// ad-hoc Pod would add real complexity for no extra signal here.
	proxyDaemonSet, err := wcs.AppsV1().DaemonSets("default").Get(ctx, "k0smotron-proxy-win", metav1.GetOptions{})
	require.NoError(t, err, "Should get the k0smotron-proxy-win DaemonSet")
	require.NotEmpty(t, proxyDaemonSet.Spec.Template.Spec.Containers, "k0smotron-proxy-win has no containers")
	proxyContainer := proxyDaemonSet.Spec.Template.Spec.Containers[0]
	require.NotEmpty(t, proxyContainer.Ports, "k0smotron-proxy-win exposes no port to probe")
	proxyImage := proxyContainer.Image
	proxyPort := proxyContainer.Ports[0].ContainerPort
	t.Logf("probing the node-local proxy on localhost:%d using image %s", proxyPort, proxyImage)

	const verifyPodName = "verify-windows-node-proxy"
	verifyPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      verifyPodName,
			Namespace: "default",
		},
		Spec: corev1.PodSpec{
			// HostProcess Pods must use the host network.
			HostNetwork: true,
			NodeSelector: map[string]string{
				"kubernetes.io/os": "windows",
			},
			SecurityContext: &corev1.PodSecurityContext{
				WindowsOptions: &corev1.WindowsSecurityContextOptions{
					HostProcess:   new(true),
					RunAsUserName: new(`NT AUTHORITY\Local service`),
				},
			},
			// Same blanket toleration the proxy DaemonSet uses, so a tainted
			// Windows node does not silently leave this Pod Pending.
			Tolerations:   []corev1.Toleration{{Operator: corev1.TolerationOpExists}},
			RestartPolicy: corev1.RestartPolicyNever,
			Containers: []corev1.Container{
				{
					Name:    "verify",
					Image:   proxyImage,
					Command: []string{"cmd.exe", "/c"},
					Args: []string{
						fmt.Sprintf(`curl.exe -skf --retry 36 --retry-delay 5 --retry-all-errors -o NUL -w "%%{http_code}" https://127.0.0.1:%d/healthz`, proxyPort),
					},
				},
			},
		},
	}
	if err := wcs.CoreV1().Pods("default").Delete(ctx, verifyPodName, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
		require.NoError(t, err, "Should clean up a stale verify pod")
	}
	_, err = wcs.CoreV1().Pods("default").Create(ctx, verifyPod, metav1.CreateOptions{})
	require.NoError(t, err, "Should create the Windows verification Pod")
	defer func() {
		_ = wcs.CoreV1().Pods("default").Delete(ctx, verifyPodName, metav1.DeleteOptions{})
	}()

	// The image is already on the node (the DaemonSet runs it), but give the
	// Pod room anyway.
	require.Eventually(t, func() bool {
		pod, err := wcs.CoreV1().Pods("default").Get(ctx, verifyPodName, metav1.GetOptions{})
		if err != nil {
			t.Logf("waiting for verify pod: %v", err)
			return false
		}
		return pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed
	}, 15*time.Minute, 15*time.Second, "verify pod never reached a terminal phase")

	// The verdict comes from the Pod's phase, not from its logs.
	//
	// Reading logs would mean the API server reaching this node's kubelet, and
	// for a Windows node that path does not exist: k0s's own konnectivity-agent
	// DaemonSet carries no kubernetes.io/os selector (k0smotron's own agent
	// manifest does -- see ingress.go), so on a Windows node it sits in
	// ContainerCreating forever with a Linux image, and every logs/exec call
	// against pods there fails with `an error on the server ("unknown")`.
	// Pod *status* travels the other way, kubelet to API server, so it is
	// unaffected -- and curl's -f already encoded the result as an exit code.
	pod, err := wcs.CoreV1().Pods("default").Get(ctx, verifyPodName, metav1.GetOptions{})
	require.NoError(t, err, "Should get the verify pod")

	var terminated string
	for _, cs := range pod.Status.ContainerStatuses {
		if cs.State.Terminated != nil {
			terminated = fmt.Sprintf("exitCode=%d reason=%q message=%q",
				cs.State.Terminated.ExitCode, cs.State.Terminated.Reason, cs.State.Terminated.Message)
		}
	}
	t.Logf("Windows verify pod: phase=%s %s", pod.Status.Phase, terminated)

	// Best effort, purely for diagnostics when the path happens to work.
	if logs, logErr := wcs.CoreV1().Pods("default").GetLogs(verifyPodName, &corev1.PodLogOptions{}).DoRaw(ctx); logErr == nil {
		t.Logf("Windows verify pod output: %s", string(logs))
	} else {
		t.Logf("verify pod logs unavailable (expected on Windows, see above): %v", logErr)
	}

	require.Equal(t, corev1.PodSucceeded, pod.Status.Phase,
		"Expected the Windows node, via k0smotron-proxy-win, to reach the API server's /healthz: %s", terminated)

	fmt.Println("All good")
}

// createWindowsIngressHostCluster creates the AWS hosting cluster and waits
// until its single control plane node is ready to run workloads.
func createWindowsIngressHostCluster(t *testing.T, testName, namespace, clusterName, sshKeyName string, cleanupHandle **clusterv1.Cluster) {
	t.Helper()

	hostClusterTemplate := clusterctl.ConfigCluster(ctx, clusterctl.ConfigClusterInput{
		ClusterctlConfigPath:     clusterctlConfigPath,
		KubeconfigPath:           bootstrapClusterProxy.GetKubeconfigPath(),
		Flavor:                   windowsIngressHostFlavor,
		Namespace:                namespace,
		ClusterName:              clusterName,
		KubernetesVersion:        hostClusterK0sVersion,
		ControlPlaneMachineCount: new(int64(1)),
		InfrastructureProvider:   "aws",
		LogFolder:                filepath.Join(artifactFolder, "clusters", bootstrapClusterProxy.GetName()),
		ClusterctlVariables: map[string]string{
			"CLUSTER_NAME": clusterName,
			"NAMESPACE":    namespace,
			"SSH_KEY_NAME": sshKeyName,
			"INGRESS_PORT": ingressPortValue,
		},
	})
	require.NotNil(t, hostClusterTemplate)

	fmt.Println(string(hostClusterTemplate))

	// Hand the cleanup a handle on the cluster BEFORE applying, not after the
	// waits below succeed. A Cluster that is applied but never finishes
	// provisioning still owns AWS resources, and cleanup can neither dump nor
	// delete what it has no pointer to -- which is how a stalled provision
	// turns into leaked EC2 instances, a VPC and a NAT gateway. Name and
	// namespace are all DeleteClusterAndWait needs.
	*cleanupHandle = clusterHandle(clusterName, namespace)

	applyClusterTemplate(t, "hosting", hostClusterTemplate)

	cluster, err := util.DiscoveryAndWaitForCluster(ctx, capiframework.DiscoveryAndWaitForClusterInput{
		Getter:    bootstrapClusterProxy.GetClient(),
		Namespace: namespace,
		Name:      clusterName,
	}, util.GetInterval(e2eConfig, testName, "wait-cluster"))
	require.NoError(t, err)

	controlPlane, err := util.DiscoveryAndWaitForControlPlaneInitialized(ctx, capiframework.DiscoveryAndWaitForControlPlaneInitializedInput{
		Lister:  bootstrapClusterProxy.GetClient(),
		Cluster: cluster,
	}, util.GetInterval(e2eConfig, testName, "wait-controllers"))
	require.NoError(t, err)

	require.NoError(t, util.WaitForControlPlaneToBeReady(ctx, bootstrapClusterProxy.GetClient(), controlPlane,
		util.GetInterval(e2eConfig, testName, "wait-control-plane")))
	fmt.Println("Hosting cluster is ready")

	*cleanupHandle = cluster
}

// clusterHandle is the minimum a Cluster object needs to be deletable and
// dumpable: cleanup only ever addresses it by name.
func clusterHandle(name, namespace string) *clusterv1.Cluster {
	return &clusterv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace}}
}

// applyClusterTemplate applies a generated cluster template and fails the spec
// with the underlying error when it does not go through.
//
// Deliberately a single attempt. CreateOrUpdate applies every object in the
// template with FieldValidation("Strict") and returns an aggregate, and a
// freshly rendered template going into a fresh namespace either applies or is
// genuinely wrong -- so there is nothing here for a retry to ride out. Retrying
// actively hurts: the first pass creates the objects the controllers then
// mutate, so a second pass re-Updates e.g. an AWSCluster whose spec.network.vpc.id
// CAPA has since filled in, and CAPA's webhook rejects that with "field cannot
// be modified once set" -- an error about the retry, layered on top of whatever
// the real problem was.
//
// The error itself must be reported: require.Eventually(..., err == nil) throws
// it away and leaves only "Condition never satisfied", which for a rejected
// field or a denied admission review is close to useless.
func applyClusterTemplate(t *testing.T, what string, template []byte) {
	t.Helper()

	require.NoError(t, bootstrapClusterProxy.CreateOrUpdate(ctx, template),
		"Failed to apply the %s cluster template", what)
}

// hostClusterIngressIP returns the address the ingress hostnames are built
// from: E2E_HOST_IP if set, otherwise the public IPv4 of the hosting cluster's
// control plane Machine. The public address is used so that a single hostname
// serves both the same-VPC workers (whose traffic hairpins through the internet
// gateway) and the test process, which is not in that VPC.
func hostClusterIngressIP(t *testing.T, namespace, clusterName string) string {
	t.Helper()

	if ip := os.Getenv(envHostIP); ip != "" {
		t.Logf("%s is set, using %s for the ingress hostnames", envHostIP, ip)
		return ip
	}

	var ip string
	require.Eventually(t, func() bool {
		machines := &clusterv1.MachineList{}
		if err := bootstrapClusterProxy.GetClient().List(ctx, machines,
			crclient.InNamespace(namespace),
			crclient.MatchingLabels{clusterv1.ClusterNameLabel: clusterName},
		); err != nil {
			t.Logf("listing hosting cluster machines: %v", err)
			return false
		}
		for _, machine := range machines.Items {
			for _, addr := range machine.Status.Addresses {
				if addr.Type == clusterv1.MachineExternalIP && addr.Address != "" {
					ip = addr.Address
					return true
				}
			}
		}
		return false
	}, 5*time.Minute, 10*time.Second, "hosting cluster control plane Machine never reported an external IP")

	return ip
}

// awsClusterNetwork holds the facts about the hosting cluster's AWS networking
// that the workload cluster template needs. They come from the hosting
// cluster's AWSCluster: CAPA writes the VPC and the subnets it created back
// into spec.network, and the security groups it created into
// status.networkStatus.
type awsClusterNetwork struct {
	vpcID               string
	subnetID            string
	availabilityZone    string
	nodeSecurityGroupID string
}

func hostClusterNetwork(t *testing.T, namespace, clusterName string) awsClusterNetwork {
	t.Helper()

	// CAPA's types are not a Go dependency of this module, so the AWSCluster is
	// read as an unstructured object.
	awsCluster := &unstructured.Unstructured{}
	awsCluster.SetAPIVersion("infrastructure.cluster.x-k8s.io/v1beta2")
	awsCluster.SetKind("AWSCluster")

	var net awsClusterNetwork
	require.Eventually(t, func() bool {
		if err := bootstrapClusterProxy.GetClient().Get(ctx, crclient.ObjectKey{
			Namespace: namespace,
			Name:      clusterName,
		}, awsCluster); err != nil {
			t.Logf("getting hosting AWSCluster: %v", err)
			return false
		}

		vpcID, _, err := unstructured.NestedString(awsCluster.Object, "spec", "network", "vpc", "id")
		if err != nil || vpcID == "" {
			t.Log("waiting for the hosting AWSCluster's VPC id")
			return false
		}

		// The workers need a PUBLIC subnet: they pull container images from the
		// internet and reach the ingress front door over the hosting node's
		// public IP.
		subnets, _, err := unstructured.NestedSlice(awsCluster.Object, "spec", "network", "subnets")
		if err != nil {
			t.Logf("reading the hosting AWSCluster's subnets: %v", err)
			return false
		}
		var subnetID, az string
		for _, raw := range subnets {
			subnet, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			isPublic, _, _ := unstructured.NestedBool(subnet, "isPublic")
			if !isPublic {
				continue
			}
			// Only regular availability-zone subnets can host cluster
			// resources; local-zone / wavelength-zone ones cannot. An unset
			// zoneType means a regular subnet.
			if zoneType, found, _ := unstructured.NestedString(subnet, "zoneType"); found && zoneType != "availability-zone" {
				continue
			}
			// For a subnet CAPA created itself, "id" holds the subnet NAME and
			// the AWS identifier lands in the read-only "resourceID" field;
			// "id" is only the AWS identifier when the subnet was pre-existing
			// and brought in by the user. See SubnetSpec in CAPA's
			// api/v1beta2/network_types.go.
			id, _, _ := unstructured.NestedString(subnet, "resourceID")
			if id == "" {
				id, _, _ = unstructured.NestedString(subnet, "id")
			}
			zone, _, _ := unstructured.NestedString(subnet, "availabilityZone")
			if strings.HasPrefix(id, "subnet-") && zone != "" {
				subnetID, az = id, zone
				break
			}
		}
		if subnetID == "" {
			t.Log("waiting for a public subnet on the hosting AWSCluster")
			return false
		}

		nodeSG, _, err := unstructured.NestedString(awsCluster.Object, "status", "networkStatus", "securityGroups", "node", "id")
		if err != nil || nodeSG == "" {
			t.Log("waiting for the hosting AWSCluster's node security group")
			return false
		}

		net = awsClusterNetwork{
			vpcID:               vpcID,
			subnetID:            subnetID,
			availabilityZone:    az,
			nodeSecurityGroupID: nodeSG,
		}
		return true
	}, 10*time.Minute, 10*time.Second, "hosting cluster networking was never fully reported by CAPA")

	return net
}

// cleanupWindowsIngressClusters dumps the spec's resources and then tears both
// clusters down in dependency order: the workload cluster's EC2 instances live
// in the hosting cluster's VPC, so they have to be gone before CAPA can delete
// that VPC. Doing this in one place (instead of two DumpSpecResourcesAndCleanup
// calls) also keeps the namespace from being deleted while the second cluster
// still needs it.
func cleanupWindowsIngressClusters(t *testing.T, testName string, namespace *corev1.Namespace, hostCluster, workloadCluster **clusterv1.Cluster, hostClusterProxy *capiframework.ClusterProxy) {
	t.Helper()

	// Dump while both clusters still exist. Dumping is namespace-wide, so one
	// call covers the CAPI resources of both; the workload cluster is passed
	// when it exists because its in-cluster resources are the interesting ones.
	dumpFor := *workloadCluster
	if dumpFor == nil {
		dumpFor = *hostCluster
	}
	if dumpFor != nil {
		bestEffort(t, "dumping management cluster resources", func() {
			util.DumpAllResourcesAndLogs(ctx, bootstrapClusterProxy, artifactFolder, namespace, dumpFor, clusterctlConfigPath)
		})
	}
	// The hosted control plane pods live in the hosting cluster, so the
	// management cluster dump above says nothing about them. Without this a
	// control plane that never becomes ready leaves no evidence at all.
	if *hostClusterProxy != nil && *hostCluster != nil {
		bestEffort(t, "dumping hosting cluster state", func() {
			dumpHostClusterState(t, *hostClusterProxy, (*hostCluster).Name, namespace.Name)
		})
	}

	if !skipCleanup {
		interval := util.GetInterval(e2eConfig, testName, "wait-delete-cluster")
		for _, cluster := range []*clusterv1.Cluster{*workloadCluster, *hostCluster} {
			if cluster == nil {
				continue
			}
			if err := util.DeleteClusterAndWait(ctx, capiframework.DeleteClusterAndWaitInput{
				ClusterProxy:         bootstrapClusterProxy,
				Cluster:              cluster,
				ArtifactFolder:       artifactFolder,
				ClusterctlConfigPath: clusterctlConfigPath,
			}, interval); err != nil {
				t.Logf("deleting cluster %s: %v", cluster.Name, err)
			}
		}

		capiframework.DeleteNamespace(ctx, capiframework.DeleteNamespaceInput{
			Deleter: bootstrapClusterProxy.GetClient(),
			Name:    namespace.Name,
		})
	}

	cancelWatches()
}

// bestEffort runs a cleanup step, turning a failure into a log line.
//
// This matters for the dump steps specifically: the CAPI framework asserts with
// gomega, and this suite's fail handler turns a gomega failure into a panic
// (see setup.go). A workload cluster whose control plane never came up has no
// kubeconfig Secret, which is enough to make the framework's dump helpers
// panic -- and without recovering here that panic would skip the cluster
// deletions below and leak EC2 instances, a VPC and an ELB.
func bestEffort(t *testing.T, what string, f func()) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Logf("%s failed, continuing with cleanup: %v", what, r)
		}
	}()
	f()
}

// dumpHostClusterState writes the hosting cluster's pod/event state and the
// logs of the hosted control plane and the ingress controller to the artifact
// folder. kubectl is used rather than typed clients so that whatever is there
// gets captured -- including the container statuses and events that explain a
// pod which never started.
func dumpHostClusterState(t *testing.T, hostClusterProxy capiframework.ClusterProxy, hostClusterName, namespace string) {
	t.Helper()

	dir := filepath.Join(artifactFolder, "clusters", hostClusterName, "hosting-cluster")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Logf("creating %s: %v", dir, err)
		return
	}

	dumps := []struct {
		file string
		args []string
	}{
		{"pods.txt", []string{"get", "pods", "-A", "-o", "wide"}},
		{"pods-describe.txt", []string{"describe", "pods", "-n", namespace}},
		{"events.txt", []string{"get", "events", "-A", "--sort-by=.lastTimestamp"}},
		{"resources.yaml", []string{"get", "statefulsets,deployments,services,ingresses,secrets,configmaps", "-n", namespace, "-o", "yaml"}},
		// The hosted control plane pods carry app=k0smotron (see
		// internal/controller/util/util.go, DefaultK0smotronClusterLabels).
		{"controlplane-logs.txt", []string{"logs", "-n", namespace, "-l", "app=k0smotron", "--all-containers", "--prefix", "--tail=-1"}},
		{"controlplane-logs-previous.txt", []string{"logs", "-n", namespace, "-l", "app=k0smotron", "--all-containers", "--prefix", "--tail=-1", "--previous"}},
		{"haproxy-logs.txt", []string{"logs", "-n", haproxyDeploymentNamespace, "-l", "run=haproxy-ingress", "--all-containers", "--prefix", "--tail=-1"}},
	}
	for _, d := range dumps {
		args := append([]string{"--kubeconfig", hostClusterProxy.GetKubeconfigPath()}, d.args...)
		out, err := exec.Command("kubectl", args...).CombinedOutput()
		if err != nil {
			out = append(out, []byte(fmt.Sprintf("\n\nkubectl %v failed: %v\n", d.args, err))...)
		}
		if writeErr := os.WriteFile(filepath.Join(dir, d.file), out, 0o600); writeErr != nil {
			t.Logf("writing %s: %v", d.file, writeErr)
		}
	}
	t.Logf("hosting cluster state written to %s", dir)
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
