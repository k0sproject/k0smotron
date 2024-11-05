package controlplane

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFailureDomainStats_Add(t *testing.T) {
	fds := &FailureDomainStats{
		list:  []string{},
		usage: map[string]int{},
	}

	fds.Add("abc")

	require.Equal(t, []string{"abc"}, fds.list)
}

func TestFailureDomainStats_Select(t *testing.T) {
	testCases := []struct {
		stats         *FailureDomainStats
		correctValues []string
	}{
		{
			stats: &FailureDomainStats{
				list:  []string{},
				usage: map[string]int{},
			},
			correctValues: []string{""},
		},
		{
			stats: &FailureDomainStats{
				list:  []string{"a", "b", "c"},
				usage: map[string]int{"a": 1, "b": 2, "c": 3},
			},
			correctValues: []string{"a"},
		},
		{
			stats: &FailureDomainStats{
				list:  []string{"a", "b", "c"},
				usage: map[string]int{"a": 2, "b": 2, "c": 1},
			},
			correctValues: []string{"c"},
		},
		{
			stats: &FailureDomainStats{
				list:  []string{"a", "b", "c"},
				usage: map[string]int{"a": 2, "b": 2, "c": 3},
			},
			correctValues: []string{"a", "b"},
		},
		{
			stats: &FailureDomainStats{
				list:  []string{"a", "b", "c"},
				usage: map[string]int{"a": 0, "b": 0, "c": 0},
			},
			correctValues: []string{"a", "b", "c"},
		},
	}

	for _, tc := range testCases {
		t.Run("", func(t *testing.T) {
			got := tc.stats.Select()
			require.Contains(t, tc.correctValues, got)
		})
	}
}
