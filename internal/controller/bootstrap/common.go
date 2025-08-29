package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"strings"

	bootstrapv1 "github.com/k0sproject/k0smotron/api/bootstrap/v1beta1"
	"github.com/k0sproject/k0smotron/internal/provisioner"
	corev1 "k8s.io/api/core/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	bsutil "sigs.k8s.io/cluster-api/bootstrap/util"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	// errExtractingFileContent represents an error when extracting the file content from a source.
	errExtractingFileContent = errors.New("failed to get file content from source")
)

func resolveContentFromFile(ctx context.Context, cli client.Client, cluster *clusterv1.Cluster, contentFrom *bootstrapv1.ContentSource) (string, error) {
	switch {
	case contentFrom.SecretRef != nil:
		s := &corev1.Secret{}
		err := cli.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: contentFrom.SecretRef.Name}, s)
		if err != nil {
			return "", fmt.Errorf("%w: %v", errExtractingFileContent, err)
		}

		content, ok := s.Data[contentFrom.SecretRef.Key]
		if !ok {
			return "", fmt.Errorf("%w: key not found in secret", errExtractingFileContent)
		}
		return string(content), nil
	case contentFrom.ConfigMapRef != nil:
		cfg := &corev1.ConfigMap{}
		err := cli.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: contentFrom.ConfigMapRef.Name}, cfg)
		if err != nil {
			return "", fmt.Errorf("%w: %v", errExtractingFileContent, err)
		}

		content, ok := cfg.Data[contentFrom.ConfigMapRef.Key]
		if !ok {
			return "", fmt.Errorf("%w: key not found in configmap", errExtractingFileContent)
		}
		return content, nil
	default:
		return "", fmt.Errorf("%w: no source specified", errExtractingFileContent)
	}
}

// resolveFiles extracts the content from the given source (ConfigMap or Secret) and returns a list of cloudinit.File containing the extracted data.
func resolveFiles(ctx context.Context, cli client.Client, cluster *clusterv1.Cluster, filesToResolve []bootstrapv1.File) ([]provisioner.File, error) {
	var files []provisioner.File
	for _, file := range filesToResolve {
		if file.ContentFrom != nil {
			content, err := resolveContentFromFile(ctx, cli, cluster, file.ContentFrom)
			if err != nil {
				return nil, fmt.Errorf("failed to resolve file content: %w", err)
			}
			file.Content = content
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
