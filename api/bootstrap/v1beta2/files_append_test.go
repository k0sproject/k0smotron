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

package v1beta2

import (
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/util/validation/field"

	"github.com/k0sproject/k0smotron/v2/internal/provisioner"
)

// file is a minimal entry, since only the path and the append flag decide whether a
// later entry conflicts with an earlier one.
func file(path string, appends bool) File {
	return File{File: provisioner.File{Path: path, Content: "x", Append: appends}}
}

// TestValidateFilesPathClaims covers which entries take a path over and which add to
// one. Ignition emits one entry per file, so there an append is a second writer of the
// same path rather than an addition to it.
func TestValidateFilesPathClaims(t *testing.T) {
	const (
		overwrite = false
		appends   = true
	)

	const (
		cloudInit = provisioner.CloudInitProvisioningFormat
		ignition  = provisioner.IgnitionProvisioningFormat
	)

	// want is the error expected on files[index], so a case pins which entry is
	// blamed and not merely how many errors came back.
	type want struct {
		index  int
		detail string
	}

	for _, tc := range []struct {
		name   string
		format provisioner.ProvisioningFormat
		files  []File
		want   []want
	}{
		{name: "cloud-init, one file", format: cloudInit, files: []File{file("/a", overwrite)}},
		{name: "ignition, one file", format: ignition, files: []File{file("/a", overwrite)}},

		{name: "cloud-init, one file appending to nothing", format: cloudInit, files: []File{file("/a", appends)}},
		{name: "ignition, one file appending to nothing", format: ignition, files: []File{file("/a", appends)}},

		{name: "cloud-init, two paths", format: cloudInit, files: []File{file("/a", overwrite), file("/b", overwrite)}},
		{name: "ignition, two paths", format: ignition, files: []File{file("/a", overwrite), file("/b", overwrite)}},

		{
			name: "cloud-init, two overwrites on one path", format: cloudInit,
			files: []File{file("/a", overwrite), file("/a", overwrite)},
			want:  []want{{1, pathConflictMsg}},
		},
		{
			name: "ignition, two overwrites on one path", format: ignition,
			files: []File{file("/a", overwrite), file("/a", overwrite)},
			want:  []want{{1, pathConflictMsg}},
		},

		// The case the change is for. Appending is fine where the format can do it.
		{
			name: "cloud-init, an append after an overwrite", format: cloudInit,
			files: []File{file("/a", overwrite), file("/a", appends)},
		},
		{
			name: "ignition, an append after an overwrite", format: ignition,
			files: []File{file("/a", overwrite), file("/a", appends)},
			want:  []want{{1, appendUnsupportedMsg}},
		},

		// The append claims nothing, so the overwrite after it is the first writer.
		{
			name: "cloud-init, an overwrite after an append", format: cloudInit,
			files: []File{file("/a", appends), file("/a", overwrite)},
		},
		{
			name: "ignition, an overwrite after an append", format: ignition,
			files: []File{file("/a", appends), file("/a", overwrite)},
			want:  []want{{1, pathConflictMsg}},
		},

		{
			name: "cloud-init, two appends after an overwrite", format: cloudInit,
			files: []File{file("/a", overwrite), file("/a", appends), file("/a", appends)},
		},
		{
			name: "ignition, two appends after an overwrite", format: ignition,
			files: []File{file("/a", overwrite), file("/a", appends), file("/a", appends)},
			want:  []want{{1, appendUnsupportedMsg}, {2, appendUnsupportedMsg}},
		},

		{
			name: "cloud-init, three overwrites blame the second and third", format: cloudInit,
			files: []File{file("/a", overwrite), file("/a", overwrite), file("/a", overwrite)},
			want:  []want{{1, pathConflictMsg}, {2, pathConflictMsg}},
		},
		{
			name: "ignition, three overwrites blame the second and third", format: ignition,
			files: []File{file("/a", overwrite), file("/a", overwrite), file("/a", overwrite)},
			want:  []want{{1, pathConflictMsg}, {2, pathConflictMsg}},
		},

		{
			name: "cloud-init, appends only", format: cloudInit,
			files: []File{file("/a", appends), file("/a", appends)},
		},
		{
			name: "ignition, appends only", format: ignition,
			files: []File{file("/a", appends), file("/a", appends)},
			want:  []want{{1, appendUnsupportedMsg}},
		},

		{
			name: "cloud-init, a conflict on one path does not mask another", format: cloudInit,
			files: []File{file("/a", overwrite), file("/a", overwrite), file("/b", overwrite), file("/b", overwrite)},
			want:  []want{{1, pathConflictMsg}, {3, pathConflictMsg}},
		},
		{
			name: "ignition, a conflict on one path does not mask another", format: ignition,
			files: []File{file("/a", overwrite), file("/a", overwrite), file("/b", overwrite), file("/b", overwrite)},
			want:  []want{{1, pathConflictMsg}, {3, pathConflictMsg}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			errs := ValidateFiles(tc.files, ProvisionerSpec{Type: tc.format}, field.NewPath("spec"))

			// Only the path rules are under test here, and matching on the message
			// keeps an unrelated failure from being read as one of them.
			var got []want
			for _, e := range errs {
				if e.Detail != pathConflictMsg && e.Detail != appendUnsupportedMsg {
					continue
				}

				for i := range tc.files {
					if e.Field == field.NewPath("spec").Child("files").Index(i).Child("path").String() {
						got = append(got, want{i, e.Detail})
					}
				}
			}

			require.Equal(t, tc.want, got, "all errors were %v", errs)
		})
	}
}

// TestAppliesAppend pins which formats can add to a path another entry already writes,
// since every path rule above hangs off it.
func TestAppliesAppend(t *testing.T) {
	for _, tc := range []struct {
		format provisioner.ProvisioningFormat
		want   bool
	}{
		{provisioner.CloudInitProvisioningFormat, true},
		{provisioner.PowershellProvisioningFormat, true},
		{provisioner.PowershellXMLProvisioningFormat, true},
		{provisioner.IgnitionProvisioningFormat, false},
	} {
		t.Run(string(tc.format), func(t *testing.T) {
			require.Equal(t, tc.want, ProvisionerSpec{Type: tc.format}.appliesAppend())
		})
	}
}
