//go:build e2e

/*
Copyright 2025.

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
	"fmt"
	"time"

	"sigs.k8s.io/cluster-api/test/framework/clusterctl"
)

const (
	defaultTickDuration    = time.Second * 10
	defaultTimeoutDuration = time.Minute * 10
)

type Interval struct {
	timeout time.Duration
	tick    time.Duration
}

func GetInterval(c *clusterctl.E2EConfig, spec string, key string) Interval {

	specKey := fmt.Sprintf("%s/%s", spec, key)

	intervals, ok := c.Intervals[specKey]
	if !ok {
		specKey = fmt.Sprintf("default/%s", key)
		if intervals, ok = c.Intervals[specKey]; !ok {
			return defaultInterval()
		}
	}

	if len(intervals) != 2 {
		return defaultInterval()
	}

	timeoutDuration, err := time.ParseDuration(intervals[0])
	if err != nil {
		fmt.Printf("Error parsing duration for spec/key '%s': %s", specKey, err.Error())
		return defaultInterval()
	}

	tickDuration, err := time.ParseDuration(intervals[1])
	if err != nil {
		fmt.Printf("Error parsing duration for spec/key '%s': %s", specKey, err.Error())
		return defaultInterval()
	}

	return Interval{
		timeout: timeoutDuration,
		tick:    tickDuration,
	}
}

func defaultInterval() Interval {
	return Interval{
		tick:    defaultTickDuration,
		timeout: defaultTimeoutDuration,
	}
}
