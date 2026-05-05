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

package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8syaml "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/controller-runtime/pkg/conversion"
	sigsyaml "sigs.k8s.io/yaml"

	bootstrapv1beta1 "github.com/k0sproject/k0smotron/api/bootstrap/v1beta1"
	bootstrapv1beta2 "github.com/k0sproject/k0smotron/api/bootstrap/v1beta2"
	cpv1beta1 "github.com/k0sproject/k0smotron/api/controlplane/v1beta1"
	cpv1beta2 "github.com/k0sproject/k0smotron/api/controlplane/v1beta2"
	infrav1beta1 "github.com/k0sproject/k0smotron/api/infrastructure/v1beta1"
	infrav1beta2 "github.com/k0sproject/k0smotron/api/infrastructure/v1beta2"
	k0smotronv1beta1 "github.com/k0sproject/k0smotron/api/k0smotron.io/v1beta1"
	k0smotronv1beta2 "github.com/k0sproject/k0smotron/api/k0smotron.io/v1beta2"
)

var scheme = runtime.NewScheme()

func init() {
	for _, add := range []func(*runtime.Scheme) error{
		k0smotronv1beta1.AddToScheme,
		k0smotronv1beta2.AddToScheme,
		bootstrapv1beta1.AddToScheme,
		bootstrapv1beta2.AddToScheme,
		cpv1beta1.AddToScheme,
		cpv1beta2.AddToScheme,
		infrav1beta1.AddToScheme,
		infrav1beta2.AddToScheme,
	} {
		if err := add(scheme); err != nil {
			panic(err)
		}
	}
}

func main() {
	if err := newRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "convert [file]",
		Short: "Convert k0smotron manifests from v1beta1 to v1beta2",
		Long: `Convert k0smotron manifests from v1beta1 to v1beta2.

Reads from stdin when no file is given. Output always goes to stdout.

WARNING: This is an experimental staging tool. Output should be reviewed before use.`,
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(_ *cobra.Command, args []string) error {
			if len(args) == 0 {
				return processStream(os.Stdin, os.Stdout)
			}
			f, err := os.Open(args[0])
			if err != nil {
				return err
			}
			defer f.Close()
			return processStream(f, os.Stdout)
		},
	}
	return cmd
}

func processStream(r io.Reader, w io.Writer) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}

	decoder := k8syaml.NewDocumentDecoder(io.NopCloser(bytes.NewReader(data)))
	buf := make([]byte, len(data)+1)
	first := true
	for {
		n, err := decoder.Read(buf)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("reading document: %w", err)
		}
		doc := bytes.TrimSpace(buf[:n])
		if len(doc) == 0 {
			continue
		}

		converted, err := convertDocument(doc)
		if err != nil {
			return err
		}

		if !first {
			fmt.Fprint(w, "---\n")
		}
		first = false
		fmt.Fprintf(w, "%s\n", converted)
	}
	return nil
}

func convertDocument(raw []byte) ([]byte, error) {
	var u unstructured.Unstructured
	if err := sigsyaml.Unmarshal(raw, &u.Object); err != nil {
		return raw, nil // not a Kubernetes object; pass through unchanged
	}

	apiVersion := u.GetAPIVersion()
	if !strings.HasSuffix(apiVersion, "/v1beta1") {
		return raw, nil
	}

	group := strings.TrimSuffix(apiVersion, "/v1beta1")
	kind := u.GetKind()

	out, err := convert(group, kind, raw)
	if err != nil {
		ref := kind + " " + u.GetName()
		if ns := u.GetNamespace(); ns != "" {
			ref = kind + " " + ns + "/" + u.GetName()
		}
		return nil, fmt.Errorf("converting %s: %w", ref, err)
	}
	return out, nil
}

// convert uses the scheme to instantiate the v1beta1 object, type-asserts it to
// conversion.Convertible, and calls ConvertTo on a scheme-created v1beta2 hub.
// If the kind is not registered in the scheme or does not implement the conversion
// interfaces, it falls back to a plain version bump.
func convert(group, kind string, raw []byte) ([]byte, error) {
	srcGVK := schema.GroupVersionKind{Group: group, Version: "v1beta1", Kind: kind}
	srcObj, err := scheme.New(srcGVK)
	if err != nil {
		// Kind not registered — not a k0smotron resource, pass through.
		return raw, nil
	}

	src, ok := srcObj.(conversion.Convertible)
	if !ok {
		// Registered but no field-level conversion needed; only bump the version.
		return bumpAPIVersion(group, raw)
	}

	dstGVK := schema.GroupVersionKind{Group: group, Version: "v1beta2", Kind: kind}
	dstObj, err := scheme.New(dstGVK)
	if err != nil {
		return nil, fmt.Errorf("creating v1beta2 object for %s: %w", kind, err)
	}

	dst, ok := dstObj.(conversion.Hub)
	if !ok {
		return nil, fmt.Errorf("%s/v1beta2 %s does not implement conversion.Hub", group, kind)
	}

	if err := sigsyaml.Unmarshal(raw, src); err != nil {
		return nil, err
	}
	if err := src.ConvertTo(dst); err != nil {
		return nil, err
	}
	dst.GetObjectKind().SetGroupVersionKind(dstGVK)
	return sigsyaml.Marshal(dst)
}

// bumpAPIVersion replaces the apiVersion field with group/v1beta2.
func bumpAPIVersion(group string, raw []byte) ([]byte, error) {
	var obj map[string]any
	if err := sigsyaml.Unmarshal(raw, &obj); err != nil {
		return nil, err
	}
	obj["apiVersion"] = group + "/v1beta2"
	return sigsyaml.Marshal(obj)
}
