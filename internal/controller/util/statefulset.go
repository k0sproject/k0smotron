package util

import (
	"context"
	"fmt"
	"k8s.io/client-go/kubernetes"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// FindStatefulSetPod returns a first running pod from a StatefulSet
func FindStatefulSetPod(ctx context.Context, clientSet *kubernetes.Clientset, statefulSet string, namespace string) (*v1.Pod, error) {
	dep, err := clientSet.AppsV1().StatefulSets(namespace).Get(ctx, statefulSet, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	selector := metav1.FormatLabelSelector(dep.Spec.Selector)
	pods, err := clientSet.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: selector,
	})
	if err != nil {
		return nil, err
	}
	if len(pods.Items) < 1 {
		return nil, fmt.Errorf("did not find matching pods for statefulSet %s", statefulSet)
	}
	// Find a running pod
	var runningPod *v1.Pod
	for _, p := range pods.Items {
		if p.Status.Phase == v1.PodRunning {
			runningPod = &p
			break
		}
	}
	if runningPod == nil {
		return nil, fmt.Errorf("did not find running pods for statefulSet %s", statefulSet)
	}
	return runningPod, nil
}
