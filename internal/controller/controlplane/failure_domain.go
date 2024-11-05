package controlplane

import (
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sort"
)

type FailureDomainStats struct {
	list  []string
	usage map[string]int
}

func NewFailureDomainsStats(failureDomains clusterv1.FailureDomains) *FailureDomainStats {
	fds := &FailureDomainStats{
		list:  []string{},
		usage: make(map[string]int),
	}

	for fd := range failureDomains {
		fds.list = append(fds.list, fd)
		fds.usage[fd] = 0
	}

	return fds
}

func (f *FailureDomainStats) Add(fd string) {
	if _, ok := f.usage[fd]; !ok {
		f.list = append(f.list, fd)
	}
	f.usage[fd]++
}

func (f *FailureDomainStats) Select() string {
	if len(f.list) == 0 {
		return ""
	}

	// sort by usage
	sort.Sort(ByUsage(*f))

	// select the least used
	return f.list[0]
}

type ByUsage FailureDomainStats

func (u ByUsage) Len() int           { return len(u.list) }
func (u ByUsage) Less(i, j int) bool { return u.usage[u.list[i]] < u.usage[u.list[j]] }
func (u ByUsage) Swap(i, j int)      { u.list[i], u.list[j] = u.list[j], u.list[i] }
