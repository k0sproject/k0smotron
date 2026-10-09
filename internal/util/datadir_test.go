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
//nolint:revive
package util

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGetDataDir(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{name: "default", want: DefaultK0sDataDir},
		{name: "equals form", args: []string{"--data-dir=/data/k0s"}, want: "/data/k0s"},
		{name: "separate value", args: []string{"--data-dir"}, want: DefaultK0sDataDir},
		{name: "empty value", args: []string{"--data-dir="}, want: DefaultK0sDataDir},
		{name: "double quoted", args: []string{`--data-dir="/data/k0s"`}, want: "/data/k0s"},
		{name: "double quoted equals with spaces", args: []string{`--data-dir="/new/drive x"`}, want: "/new/drive x"},
		{name: "single element with value", args: []string{"--data-dir /new/drive"}, want: "/new/drive"},
		{name: "mixed quoting", args: []string{`--data-dir='/a'"/b"`}, want: "/a/b"},
		{name: "escaped space", args: []string{`--data-dir=/a\ b`}, want: "/a b"},
		{name: "unbalanced quote", args: []string{`--data-dir="/a`}, want: DefaultK0sDataDir},
		{name: "value in separate argument with quotes, unsupported", args: []string{"--data-dir", "'/data/k0s'"}, want: DefaultK0sDataDir},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, GetDataDir(tc.args))
		})
	}
}
