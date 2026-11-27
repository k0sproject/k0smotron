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

package infrastructure

import (
	"context"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	kubefake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	api "github.com/k0sproject/k0smotron/v2/api/infrastructure/v1beta2"
	"github.com/k0sproject/k0smotron/v2/internal/provisioner"
)

func newTestJobProvisioner(t *testing.T, objects ...runtime.Object) (*JobProvisioner, *kubefake.Clientset) {
	t.Helper()

	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	clientSet := kubefake.NewClientset(objects...)
	return &JobProvisioner{
		client:    fake.NewClientBuilder().WithScheme(scheme).Build(),
		clientSet: clientSet,
		cloudInit: &provisioner.InputProvisionData{Commands: []string{"k0s start"}},
		machine:   &clusterv1.Machine{Spec: clusterv1.MachineSpec{ClusterName: "cluster"}},
		remoteMachine: &api.RemoteMachine{
			TypeMeta:   metav1.TypeMeta{APIVersion: api.GroupVersion.String(), Kind: "RemoteMachine"},
			ObjectMeta: metav1.ObjectMeta{Name: "rm", Namespace: "default", UID: types.UID("rm-uid")},
			Spec:       api.RemoteMachineSpec{Address: "host", User: "root"},
		},
		provisionJob: &api.ProvisionJob{
			SSHCommand: "ssh",
			SCPCommand: "scp",
			JobTemplate: &batchv1.JobTemplateSpec{
				Spec: batchv1.JobSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "provision", Image: "ssh"}}},
					},
				},
			},
		},
		log: logr.Discard(),
	}, clientSet
}

// finishJobsOnCreate makes the fake API server report every created job as
// finished with the given condition, like the job controller would.
func finishJobsOnCreate(clientSet *kubefake.Clientset, condition batchv1.JobCondition) {
	clientSet.PrependReactor("create", "jobs", func(action k8stesting.Action) (bool, runtime.Object, error) {
		job := action.(k8stesting.CreateAction).GetObject().(*batchv1.Job)
		job.Status.Conditions = append(job.Status.Conditions, condition)
		return false, nil, nil
	})
}

func TestJobProvisionerWaitsForJobCompletion(t *testing.T) {
	p, clientSet := newTestJobProvisioner(t)
	finishJobsOnCreate(clientSet, batchv1.JobCondition{Type: batchv1.JobComplete, Status: corev1.ConditionTrue})

	require.NoError(t, p.Provision(context.Background()))

	job, err := clientSet.BatchV1().Jobs("default").Get(context.Background(), "cluster-rm", metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, types.UID("rm-uid"), job.OwnerReferences[0].UID)
}

func TestJobProvisionerReportsJobFailure(t *testing.T) {
	p, clientSet := newTestJobProvisioner(t)
	finishJobsOnCreate(clientSet, batchv1.JobCondition{
		Type:    batchv1.JobFailed,
		Status:  corev1.ConditionTrue,
		Reason:  batchv1.JobReasonBackoffLimitExceeded,
		Message: "Job has reached the specified backoff limit",
	})

	err := p.Provision(context.Background())
	require.ErrorContains(t, err, "job default/cluster-rm failed: BackoffLimitExceeded: Job has reached the specified backoff limit")
}

func TestJobProvisionerKeepsWaitingForRunningJob(t *testing.T) {
	p, clientSet := newTestJobProvisioner(t)
	// A condition that is not true yet must not be taken as the job's outcome.
	finishJobsOnCreate(clientSet, batchv1.JobCondition{Type: batchv1.JobComplete, Status: corev1.ConditionFalse})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	require.ErrorIs(t, p.Provision(ctx), context.DeadlineExceeded)
}

func TestJobProvisionerResumesExistingJob(t *testing.T) {
	existing := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "cluster-rm",
			Namespace:       "default",
			OwnerReferences: []metav1.OwnerReference{{UID: types.UID("rm-uid")}},
		},
		Status: batchv1.JobStatus{
			Conditions: []batchv1.JobCondition{{Type: batchv1.JobComplete, Status: corev1.ConditionTrue}},
		},
	}
	p, _ := newTestJobProvisioner(t, existing)

	require.NoError(t, p.Provision(context.Background()))
}

func TestJobProvisionerRejectsJobOfAnotherRemoteMachine(t *testing.T) {
	stale := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "cluster-rm",
			Namespace:       "default",
			OwnerReferences: []metav1.OwnerReference{{UID: types.UID("deleted-rm-uid")}},
		},
		Status: batchv1.JobStatus{
			Conditions: []batchv1.JobCondition{{Type: batchv1.JobComplete, Status: corev1.ConditionTrue}},
		},
	}
	p, _ := newTestJobProvisioner(t, stale)

	require.ErrorContains(t, p.Provision(context.Background()), "job default/cluster-rm is not owned by this RemoteMachine")
}
