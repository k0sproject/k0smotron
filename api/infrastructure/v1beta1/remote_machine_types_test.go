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

package v1beta1

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestRemoteMachine_GetPool(t *testing.T) {
	tests := []struct {
		name string
		rm   RemoteMachine
		want string
	}{
		{
			name: "should return pool from annotation",
			rm: RemoteMachine{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						PoolAnnotation: "test-pool",
					},
				},
			},
			want: "test-pool",
		},
		{
			name: "should return empty string if annotation is not set",
			rm:   RemoteMachine{},
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.rm.GetPool(); got != tt.want {
				t.Errorf("RemoteMachine.GetPool() = %v, want %v", got, tt.want)
			}
		})
	}
}
