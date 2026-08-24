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
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"maps"
	"strings"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/cluster-api/util/conditions"
	"sigs.k8s.io/cluster-api/util/patch"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	k0smotroniov1beta2 "github.com/k0sproject/k0smotron/v2/api/k0smotron.io/v1beta2"
	km "github.com/k0sproject/k0smotron/v2/api/k0smotron.io/v1beta2"
	"github.com/k0sproject/k0smotron/v2/internal/controller/util"
	"github.com/k0sproject/k0smotron/v2/internal/exec"
)

// joinTokenRequestFinalizer is the finalizer used by JoinTokenRequest to clean up resources.
const joinTokenRequestFinalizer = "jointokenrequests.k0smotron.io/finalizer"

// JoinTokenRequestReconciler reconciles a JoinTokenRequest object
type JoinTokenRequestReconciler struct {
	client.Client
	Scheme     *runtime.Scheme
	ClientSet  *kubernetes.Clientset
	RESTConfig *rest.Config
}

//+kubebuilder:rbac:groups=k0smotron.io,resources=jointokenrequests,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=k0smotron.io,resources=jointokenrequests/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=k0smotron.io,resources=jointokenrequests/finalizers,verbs=update
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch;create;update;patch;delete

// Reconcile reconciles a JoinTokenRequest object by creating a join token for the referenced
// cluster and storing it in a secret.
func (r *JoinTokenRequestReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var jtr km.JoinTokenRequest
	if err := r.Get(ctx, req.NamespacedName, &jtr); err != nil {
		logger.Error(err, "unable to fetch JoinTokenRequest")
		// we'll ignore not-found errors, since they can't be fixed by an immediate
		// requeue (we'll need to wait for a new notification), and we can get them
		// on deleted requests.
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if finalizerAdded, err := util.EnsureFinalizer(ctx, r.Client, &jtr, joinTokenRequestFinalizer); err != nil || finalizerAdded {
		return ctrl.Result{RequeueAfter: 5 * time.Second}, err
	}

	patchHelper, err := patch.NewHelper(&jtr, r.Client)
	if err != nil {
		logger.Error(err, "Failed to configure the patch helper")
		return ctrl.Result{Requeue: true}, nil
	}

	isJTRBeingDeleted := !jtr.ObjectMeta.DeletionTimestamp.IsZero()

	reconcileFailureMessage := ""
	clusterUID := types.UID("")
	defer func() {
		if !isJTRBeingDeleted {
			r.updateStatus(&jtr, reconcileFailureMessage, clusterUID)

			err = patchHelper.Patch(ctx, &jtr)
			if err != nil {
				logger.Error(err, "Unable to update JoinTokenRequest status")
			}
		}
	}()

	// In v1beta1, it is allowed to create a JoinTokenRequest in a different namespace than the cluster, so we need to check the annotation
	// for the cluster namespace to keep backward compatibility until v1beta1 is fully removed. If the annotation, which stores the v1beta1
	// namespace used, is not present we assume the JoinTokenRequest is in the same namespace as the cluster.
	clusterNamespace := jtr.Namespace
	isV1Beta1WithPossibleCrossNamespace := false
	if ns, ok := jtr.Annotations[km.V1Beta1ClusterRefNamespaceAnnotation]; ok {
		clusterNamespace = ns
		isV1Beta1WithPossibleCrossNamespace = true
	} else {
		// Do not try to add owner references when deleting because the cluster might be deleted at this point.
		if !isJTRBeingDeleted {
			// JoinTokenRequest created in v1beta2
			var kmc km.Cluster
			err = r.Client.Get(ctx, types.NamespacedName{Name: jtr.Spec.ClusterName, Namespace: clusterNamespace}, &kmc)
			if err != nil {
				reconcileFailureMessage = "Failed to get k0smotron cluster"
				return ctrl.Result{Requeue: true, RequeueAfter: time.Minute}, err
			}
			if ownerRefAdded := ensureOwnerReference(&jtr, kmc); ownerRefAdded {
				// If the owner reference was updated, we need to patch the JoinTokenRequest to persist it before proceeding with the reconciliation,
				// otherwise the controller may try to update the status of the JoinTokenRequest without the owner reference, which will cause a validation error.
				err = patchHelper.Patch(ctx, &jtr)
				if err != nil {
					logger.Error(err, "Failed to patch JoinTokenRequest with owner reference")
				}
				return ctrl.Result{Requeue: true, RequeueAfter: time.Second * 5}, err
			}
		}
	}

	logger.Info("Reconciling")
	pod, err := util.FindStatefulSetPod(ctx, r.ClientSet, km.GetStatefulSetName(jtr.Spec.ClusterName), clusterNamespace)
	if err != nil && !isJTRBeingDeleted {
		reconcileFailureMessage = "Failed finding pods in statefulset"
		return ctrl.Result{Requeue: true, RequeueAfter: time.Minute}, err
	}

	if isJTRBeingDeleted {
		if controllerutil.ContainsFinalizer(&jtr, joinTokenRequestFinalizer) {
			if pod != nil {
				logger.Info("Invalidating token before deletion")
				if err := r.invalidateToken(ctx, &jtr, pod); err != nil {
					return ctrl.Result{}, err
				}
			}
			controllerutil.RemoveFinalizer(&jtr, joinTokenRequestFinalizer)
			if err := r.Update(ctx, &jtr); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	if jtr.Status.TokenID != "" {
		logger.Info("Already reconciled")
		return ctrl.Result{}, nil
	}

	var cluster km.Cluster
	err = r.Client.Get(ctx, types.NamespacedName{Name: jtr.Spec.ClusterName, Namespace: clusterNamespace}, &cluster)
	if err != nil {
		reconcileFailureMessage = "Failed to get cluster"
		return ctrl.Result{Requeue: true, RequeueAfter: time.Minute}, err
	}
	clusterUID = cluster.GetUID()
	if isV1Beta1WithPossibleCrossNamespace {
		// Cross namespace JoinTokenRequest created in v1beta1, we set the cluster UID label for filtering the JoinTokenRequests related to a cluster
		// in the same namespace, which is needed for the cleanup of the JoinTokenRequests when a cluster is deleted.
		jtr.SetLabels(map[string]string{clusterUIDLabel: string(clusterUID)})
	}

	cmd := fmt.Sprintf("k0s token create --role=%s --expiry=%s", jtr.Spec.Role, jtr.Spec.Expiry)
	token, err := exec.PodExecCmdOutput(ctx, r.ClientSet, r.RESTConfig, pod.Name, pod.Namespace, cmd)
	if err != nil {
		reconcileFailureMessage = "Failed getting token"
		return ctrl.Result{Requeue: true, RequeueAfter: time.Minute}, err
	}

	if cluster.Spec.Ingress != nil {
		token, err = updateJoinTokenURL(token, cluster)
		if err != nil {
			reconcileFailureMessage = "Failed updating token URL"
			return ctrl.Result{Requeue: true, RequeueAfter: time.Minute}, err
		}
	}

	if err := r.reconcileSecret(ctx, jtr, token); err != nil {
		reconcileFailureMessage = "Failed creating secret"
		return ctrl.Result{Requeue: true, RequeueAfter: time.Minute}, err
	}

	tokenID, err := getTokenID(token, jtr.Spec.Role)
	if err != nil {
		reconcileFailureMessage = "Failed getting token id"
		return ctrl.Result{Requeue: true, RequeueAfter: time.Minute}, err
	}

	jtr.Status.TokenID = tokenID

	return ctrl.Result{}, nil
}

func (r *JoinTokenRequestReconciler) invalidateToken(ctx context.Context, jtr *km.JoinTokenRequest, pod *v1.Pod) error {
	cmd := fmt.Sprintf("k0s token invalidate %s", jtr.Status.TokenID)
	_, err := exec.PodExecCmdOutput(ctx, r.ClientSet, r.RESTConfig, pod.Name, pod.Namespace, cmd)
	return err
}

func (r *JoinTokenRequestReconciler) reconcileSecret(ctx context.Context, jtr km.JoinTokenRequest, token string) error {
	logger := log.FromContext(ctx)
	logger.Info("Reconciling configmap")

	cm, err := r.generateSecret(&jtr, token)
	if err != nil {
		return err
	}

	return r.Client.Patch(ctx, &cm, client.Apply, patchOpts...)
}

func (r *JoinTokenRequestReconciler) generateSecret(jtr *km.JoinTokenRequest, token string) (v1.Secret, error) {
	labels := map[string]string{
		clusterLabel:                 jtr.Spec.ClusterName,
		"k0smotron.io/role":          jtr.Spec.Role,
		"k0smotron.io/token-request": jtr.Name,
		util.ComponentLabel:          util.ComponentJointoken,
	}
	maps.Copy(labels, jtr.Labels)
	secret := v1.Secret{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Secret",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        jtr.Name,
			Namespace:   jtr.Namespace,
			Labels:      labels,
			Annotations: jtr.Annotations,
		},
		StringData: map[string]string{
			"token": token,
		},
	}

	_ = ctrl.SetControllerReference(jtr, &secret, r.Scheme)
	return secret, nil
}

func (r *JoinTokenRequestReconciler) updateStatus(jtr *km.JoinTokenRequest, failureMessage string, clusterUID types.UID) {
	if failureMessage != "" {
		// For backward compatibility and webhook conversion, we use the deprecated status field to store the failure reason,
		// which will be removed in future versions.
		jtr.SetDeprecatedStatus(failureMessage, clusterUID)

		conditions.Set(jtr, metav1.Condition{
			Type:    k0smotroniov1beta2.JoinTokenRequestSecretCondition,
			Status:  metav1.ConditionFalse,
			Reason:  k0smotroniov1beta2.JoinTokenRequestInternalErrorReason,
			Message: fmt.Sprintf("%s. Check logs for more details", failureMessage),
		})
		return
	}

	// Clean the deprecated status field on successful reconciliation.
	jtr.SetDeprecatedStatus("Reconciliation successful", clusterUID)

	conditions.Set(jtr, metav1.Condition{
		Type:   k0smotroniov1beta2.JoinTokenRequestSecretCondition,
		Status: metav1.ConditionTrue,
		Reason: k0smotroniov1beta2.JoinTokenRequestSecretCreatedReason,
	})
}

func ensureOwnerReference(jtr *km.JoinTokenRequest, kmc km.Cluster) bool {
	refs := jtr.GetOwnerReferences()
	for _, ref := range refs {
		if ref.UID == kmc.GetUID() {
			// Owner reference already exists, no update needed
			return false
		}
	}

	refs = append(refs, metav1.OwnerReference{
		APIVersion: km.GroupVersion.String(),
		Kind:       kmc.GroupVersionKind().Kind,
		Name:       kmc.GetName(),
		UID:        kmc.GetUID(),
		Controller: new(true),
	})

	jtr.SetOwnerReferences(refs)
	return true
}

// SetupWithManager sets up the controller with the Manager.
func (r *JoinTokenRequestReconciler) SetupWithManager(mgr ctrl.Manager, opts controller.Options) error {
	return ctrl.NewControllerManagedBy(mgr).
		WithOptions(opts).
		For(&km.JoinTokenRequest{}).
		Complete(r)
}

func updateJoinTokenURL(token string, kmc km.Cluster) (string, error) {
	b, err := tokenDecode(token)
	if err != nil {
		return "", err
	}

	cfg, err := clientcmd.Load(b)
	if err != nil {
		return "", err
	}

	for _, cluster := range cfg.Clusters {
		cluster.Server = fmt.Sprintf("https://%s:%d", kmc.Spec.Ingress.APIHost, kmc.Spec.Ingress.Port)
	}

	updatedData, err := clientcmd.Write(*cfg)
	if err != nil {
		return "", err
	}

	encodedToken, err := tokenEncode(bytes.NewReader(updatedData))
	if err != nil {
		return "", err
	}
	return encodedToken, nil
}

func getTokenID(token, role string) (string, error) {
	b, err := tokenDecode(token)
	if err != nil {
		return "", err
	}

	cfg, err := clientcmd.Load(b)
	if err != nil {
		return "", err
	}

	var userName string
	switch role {
	case "controller":
		userName = "controller-bootstrap"
	case "worker":
		userName = "kubelet-bootstrap"
	default:
		return "", fmt.Errorf("unknown role: %s", role)
	}

	tokenID, _, _ := strings.Cut(cfg.AuthInfos[userName].Token, ".")
	return tokenID, nil
}

func tokenDecode(token string) ([]byte, error) {
	b, err := base64.StdEncoding.DecodeString(token)
	if err != nil {
		return nil, err
	}
	gz, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}

	output, err := io.ReadAll(gz)
	if err != nil {
		return nil, err
	}

	return output, err
}

func tokenEncode(in io.Reader) (string, error) {
	var outBuf bytes.Buffer
	gz, err := gzip.NewWriterLevel(&outBuf, gzip.BestCompression)
	if err != nil {
		return "", err
	}

	_, err = io.Copy(gz, in)
	gzErr := gz.Close()
	if err != nil {
		return "", err
	}
	if gzErr != nil {
		return "", gzErr
	}

	return base64.StdEncoding.EncodeToString(outBuf.Bytes()), nil
}
