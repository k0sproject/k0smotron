package util

import (
	"fmt"
	"os/exec"
	"strings"
)

func GetControlPlaneNodesIDs(prefix string) ([]string, error) {
	out, err := exec.Command("/bin/sh", "-c", fmt.Sprintf(`docker ps | grep %s | grep -v "\-lb" | grep -v worker | awk '{print $1}'`, prefix)).Output()
	if err != nil {
		return nil, err
	}

	if string(out) == "" {
		return []string{}, nil
	}
	return strings.Split(strings.Trim(string(out), "\n "), "\n"), nil
}
