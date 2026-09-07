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

package util

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func TestEnsureNamespaceExists(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	gr := corev1.Resource("namespaces")

	tests := []struct {
		name         string
		existing     []client.Object
		interceptor  interceptor.Funcs
		wantErr      string
		skipGetCheck bool
	}{
		{
			name: "namespace already exists",
			existing: []client.Object{
				&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "foo"}},
			},
		},
		{
			name: "namespace missing, created",
		},
		{
			name: "namespace missing, created by another process in the meantime",
			interceptor: interceptor.Funcs{
				Create: func(_ context.Context, _ client.WithWatch, _ client.Object, _ ...client.CreateOption) error {
					return apierrors.NewAlreadyExists(gr, "foo")
				},
			},
			skipGetCheck: true,
		},
		{
			name: "namespace missing, create fails",
			interceptor: interceptor.Funcs{
				Create: func(_ context.Context, _ client.WithWatch, _ client.Object, _ ...client.CreateOption) error {
					return apierrors.NewInternalError(context.DeadlineExceeded)
				},
			},
			wantErr: "failed to create namespace foo",
		},
		{
			name: "get forbidden",
			interceptor: interceptor.Funcs{
				Get: func(_ context.Context, _ client.WithWatch, _ client.ObjectKey, _ client.Object, _ ...client.GetOption) error {
					return apierrors.NewForbidden(gr, "foo", context.DeadlineExceeded)
				},
			},
			wantErr: "insufficient permissions",
		},
		{
			name: "get fails with other error",
			interceptor: interceptor.Funcs{
				Get: func(_ context.Context, _ client.WithWatch, _ client.ObjectKey, _ client.Object, _ ...client.GetOption) error {
					return apierrors.NewInternalError(context.DeadlineExceeded)
				},
			},
			wantErr: "failed to get namespace foo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tt.existing...).
				WithInterceptorFuncs(tt.interceptor).
				Build()

			err := EnsureNamespaceExists(context.Background(), c, "foo")

			if tt.wantErr == "" {
				require.NoError(t, err)
				if !tt.skipGetCheck {
					var ns corev1.Namespace
					require.NoError(t, c.Get(context.Background(), client.ObjectKey{Name: "foo"}, &ns))
				}
			} else {
				require.ErrorContains(t, err, tt.wantErr)
			}
		})
	}
}
