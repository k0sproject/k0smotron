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
	"strings"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	km "github.com/k0sproject/k0smotron/api/k0smotron.io/v1beta1"
	"github.com/k0sproject/k0smotron/internal/controller/util"
	"github.com/k0sproject/k0smotron/internal/exec"
)

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

	var cluster km.Cluster
	err := r.Client.Get(ctx, types.NamespacedName{Name: jtr.Spec.ClusterRef.Name, Namespace: jtr.Spec.ClusterRef.Namespace}, &cluster)
	if err != nil {
		r.updateStatus(ctx, jtr, "Failed getting cluster")
		return ctrl.Result{Requeue: true, RequeueAfter: time.Minute}, err
	}
	clusterUID := cluster.GetUID()
	jtr.Status.ClusterUID = clusterUID

	if !controllerutil.ContainsFinalizer(&cluster, clusterFinalizer) {
		cluster.Finalizers = append(cluster.Finalizers, clusterFinalizer)
	}
	err = r.Update(ctx, &cluster)
	if err != nil {
		logger.Error(err, "unable to add finalizer to cluster for JoinTokenRequest resource removal")
	}

	jtr.SetLabels(map[string]string{clusterUIDLabel: string(clusterUID)})
	err = r.Update(ctx, &jtr)
	if err != nil {
		r.updateStatus(ctx, jtr, "Failed update JoinTokenRequest with cluster UID label")
		return ctrl.Result{Requeue: true, RequeueAfter: time.Minute}, err
	}

	logger.Info("Reconciling")
	pod, err := util.FindStatefulSetPod(ctx, r.ClientSet, km.GetStatefulSetName(jtr.Spec.ClusterRef.Name), jtr.Spec.ClusterRef.Namespace)
	if err != nil {
		r.updateStatus(ctx, jtr, "Failed finding pods in statefulset")
		return ctrl.Result{Requeue: true, RequeueAfter: time.Minute}, err
	}

	finalizerName := "jointokenrequests.k0smotron.io/finalizer"
	if !jtr.ObjectMeta.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(&jtr, finalizerName) {
			if err := r.invalidateToken(ctx, &jtr, pod); err != nil {
				return ctrl.Result{}, err
			}
			controllerutil.RemoveFinalizer(&jtr, finalizerName)
			if err := r.Update(ctx, &jtr); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	if !controllerutil.ContainsFinalizer(&jtr, finalizerName) {
		controllerutil.AddFinalizer(&jtr, finalizerName)
	}

	if jtr.Status.TokenID != "" {
		logger.Info("Already reconciled")
		return ctrl.Result{}, nil
	}

	cmd := fmt.Sprintf("k0s token create --role=%s --expiry=%s", jtr.Spec.Role, jtr.Spec.Expiry)
	token, err := exec.PodExecCmdOutput(ctx, r.ClientSet, r.RESTConfig, pod.Name, pod.Namespace, cmd)
	if err != nil {
		r.updateStatus(ctx, jtr, "Failed getting token")
		return ctrl.Result{Requeue: true, RequeueAfter: time.Minute}, err
	}

	if err := r.reconcileSecret(ctx, jtr, token); err != nil {
		r.updateStatus(ctx, jtr, "Failed creating secret")
		return ctrl.Result{Requeue: true, RequeueAfter: time.Minute}, err
	}

	tokenID, err := getTokenID(token, jtr.Spec.Role)
	if err != nil {
		r.updateStatus(ctx, jtr, "Failed getting token id")
		return ctrl.Result{Requeue: true, RequeueAfter: time.Minute}, err
	}
	jtr.Status.TokenID = tokenID
	r.updateStatus(ctx, jtr, "Reconciliation successful")
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
		clusterLabel:                 jtr.Spec.ClusterRef.Name,
		"k0smotron.io/cluster-uid":   string(jtr.Status.ClusterUID),
		"k0smotron.io/role":          jtr.Spec.Role,
		"k0smotron.io/token-request": jtr.Name,
	}
	for k, v := range jtr.Labels {
		labels[k] = v
	}
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

func (r *JoinTokenRequestReconciler) updateStatus(ctx context.Context, jtr km.JoinTokenRequest, status string) {
	logger := log.FromContext(ctx)
	jtr.Status.ReconciliationStatus = status
	if err := r.Status().Update(ctx, &jtr); err != nil {
		logger.Error(err, fmt.Sprintf("Unable to update status: %s", status))
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *JoinTokenRequestReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&km.JoinTokenRequest{}).
		Complete(r)
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
