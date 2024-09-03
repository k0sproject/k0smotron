package bootstrap

import (
	"fmt"
	"strings"

	bsutil "sigs.k8s.io/cluster-api/bootstrap/util"
)

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
