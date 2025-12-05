package v1beta1

import (
	"testing"

	"k8s.io/apimachinery/pkg/util/validation/field"
)

func TestK0sWorkerConfigSpec_validateVersion(t *testing.T) {
	tests := []struct {
		name string // description of this test case
		// Named input parameters for target function.
		pathPrefix *field.Path
		want       field.ErrorList
	}{
		{
			name:       "test case 1",
			pathPrefix: field.NewPath("spec").Child("version"),
			want:       field.ErrorList{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// TODO: construct the receiver type.
			var cs K0sWorkerConfigSpec
			cs.Version = "v1.31.5"
			got := cs.validateVersion(tt.pathPrefix)
			// TODO: update the condition below to compare got with tt.want.
			if true {
				t.Errorf("validateVersion() = %v, want %v", got, tt.want)
			}
		})
	}
}
