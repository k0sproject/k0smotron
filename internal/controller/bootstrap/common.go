package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"strings"

	bootstrapv1 "github.com/k0sproject/k0smotron/api/bootstrap/v1beta1"
	"github.com/k0sproject/k0smotron/internal/cloudinit"
	corev1 "k8s.io/api/core/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	bsutil "sigs.k8s.io/cluster-api/bootstrap/util"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	// errExtractingFileContent represents an error when extracting the file content from a source.
	errExtractingFileContent = errors.New("failed to get file content from source")
)

// resolveFiles extracts the content from the given source (ConfigMap or Secret) and returns a list of cloudinit.File containing the extracted data.
func resolveFiles(ctx context.Context, cli client.Client, cluster *clusterv1.Cluster, filesToResolve []bootstrapv1.File) ([]cloudinit.File, error) {
	var files []cloudinit.File
	for _, file := range filesToResolve {
		if file.ContentFrom != nil {
			switch {
			case file.ContentFrom.SecretRef != nil:
				s := &corev1.Secret{}
				err := cli.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: file.ContentFrom.SecretRef.Name}, s)
				if err != nil {
					return nil, fmt.Errorf("%w: %v", errExtractingFileContent, err)
				}

				content, ok := s.Data[file.ContentFrom.SecretRef.Key]
				if !ok {
					return nil, fmt.Errorf("%w: key not found in secret", errExtractingFileContent)
				}
				file.Content = string(content)
			case file.ContentFrom.ConfigMapRef != nil:
				cfg := &corev1.ConfigMap{}
				err := cli.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: file.ContentFrom.ConfigMapRef.Name}, cfg)
				if err != nil {
					return nil, fmt.Errorf("%w: %v", errExtractingFileContent, err)
				}

				content, ok := cfg.Data[file.ContentFrom.ConfigMapRef.Key]
				if !ok {
					return nil, fmt.Errorf("%w: key not found in configmap", errExtractingFileContent)
				}
				file.Content = content
			default:
				return nil, fmt.Errorf("%w: no source specified", errExtractingFileContent)
			}

			file.ContentFrom = nil
		}

		files = append(files, file.File)
	}
	return files, nil
}

func mergeExtraArgs(configArgs []string, configOwner *bsutil.ConfigOwner, isWorker bool, useSystemHostname bool) []string {
	var args []string
	if isWorker {
		args = []string{
			"--labels=" + fmt.Sprintf("%s=%s", machineNameNodeLabel, configOwner.GetName()),
		}
	}

	kubeletExtraArgs := fmt.Sprintf(`--kubelet-extra-args="--hostname-override=%s"`, configOwner.GetName())
	for _, arg := range configArgs {
		if strings.HasPrefix(arg, "--kubelet-extra-args") && !useSystemHostname {
			_, after, ok := strings.Cut(arg, "=")
			if !ok {
				_, after, ok = strings.Cut(arg, " ")
			}
			if !ok {
				kubeletExtraArgs = arg
			} else {
				kubeletExtraArgs = fmt.Sprintf(`--kubelet-extra-args="--hostname-override=%s %s"`, configOwner.GetName(), strings.Trim(after, "\"'"))
			}
		} else {
			args = append(args, arg)
		}
	}
	if isWorker && !useSystemHostname {
		args = append(args, kubeletExtraArgs)
	}

	return args
}
