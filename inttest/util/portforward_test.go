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

package util

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_getHost(t *testing.T) {

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "with valid URL",
			input: "http://localhost:8080",
			want:  "localhost:8080",
		},
		{
			name:  "plain host:port",
			input: "localhost:8080",
			want:  "localhost:8080",
		},
		{
			name:  "plain host:port with IP",
			input: "1.2.3.4:8080",
			want:  "1.2.3.4:8080",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, getHost(tt.input))
		})
	}
}
