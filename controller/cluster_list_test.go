package controller

import (
	"context"
	"errors"
	"regexp"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zalando-incubator/cluster-lifecycle-manager/api"
	"github.com/zalando-incubator/cluster-lifecycle-manager/channel"
	"github.com/zalando-incubator/cluster-lifecycle-manager/config"
)

var mockStatus = &api.ClusterStatus{
	NextVersion:    "",
	CurrentVersion: "abc#123",
}

var devRevision = channel.ConfigVersion("<dev-channel>")
var defaultChannels = channel.NewGitVersions(map[string]channel.ConfigVersion{
	"dev": devRevision,
})
var dummyCancelFunc = func() {}

func TestUpdateIgnoresClusters(t *testing.T) {
	filter := config.IncludeExcludeFilter{
		Exclude: regexp.MustCompile("^aws:123456789222.*"),
		Include: regexp.MustCompile("^aws.*"),
	}

	for _, ti := range []struct {
		cluster *api.Cluster
		ignored bool
	}{
		{
			cluster: &api.Cluster{
				ID: "aws:123456789011:eu-central-1:decommissioned",
				InfrastructureAccount: "aws:123456789011",
				LifecycleStatus:       "decommissioned",
				Channel:               "dev",
				Status:                mockStatus,
			},
			ignored: true,
		},
		{
			cluster: &api.Cluster{
				ID: "aws:123456789011:eu-central-1:ready",
				InfrastructureAccount: "aws:123456789011",
				LifecycleStatus:       "ready",
				Channel:               "dev",
				Status:                mockStatus,
			},
			ignored: false,
		},
		{
			cluster: &api.Cluster{
				ID: "aws:123456789011:eu-central-1:requested",
				InfrastructureAccount: "aws:123456789011",
				LifecycleStatus:       "ready",
				Channel:               "dev",
				Status:                mockStatus,
			},
			ignored: false,
		},
		{
			cluster: &api.Cluster{
				ID: "aws:123456789011:eu-central-1:decommission-requested",
				InfrastructureAccount: "aws:123456789011",
				LifecycleStatus:       "decommission-requested",
				Channel:               "dev",
				Status:                mockStatus,
			},
			ignored: false,
		},
		{
			cluster: &api.Cluster{
				ID: "aws:123456789222:eu-central-1:excluded",
				InfrastructureAccount: "aws:123456789222",
				LifecycleStatus:       "ready",
				Channel:               "dev",
				Status:                mockStatus,
			},
			ignored: true,
		},
		{
			cluster: &api.Cluster{
				ID: "aws:123456789011:eu-central-1:update-blocked",
				InfrastructureAccount: "aws:123456789011",
				LifecycleStatus:       "ready",
				Channel:               "dev",
				Status:                mockStatus,
				ConfigItems:           map[string]string{updateBlockedConfigItem: "please don't"},
			},
			ignored: true,
		},
		{
			cluster: &api.Cluster{
				ID: "foobar:123456789011:eu-central-1:not-included",
				InfrastructureAccount: "foobar:123456789011",
				LifecycleStatus:       "ready",
				Channel:               "dev",
				Status:                mockStatus,
			},
			ignored: true,
		},
	} {
		clusterList := NewClusterList(filter, []string{})
		clusterList.UpdateAvailable(defaultChannels, []*api.Cluster{ti.cluster})
		nextCluster := clusterList.SelectNext(dummyCancelFunc)
		if ti.ignored {
			assert.Nil(t, nextCluster, "cluster wasn't ignored: %s", ti.cluster.ID)
		} else {
			assert.NotNil(t, nextCluster, "cluster ignored: %s", ti.cluster.ID)
		}
	}
}

func allClusterIds(clusterList *ClusterList) []string {
	var clusters []*ClusterInfo
	var result []string
	for {
		clusterInfo := clusterList.SelectNext(dummyCancelFunc)
		if clusterInfo == nil {
			for _, info := range clusters {
				clusterList.ClusterProcessed(info)
			}
			return result
		} else {
			clusters = append(clusters, clusterInfo)
			result = append(result, clusterInfo.Cluster.ID)
		}
	}
}

func TestUpdateAddsNewClusters(t *testing.T) {
	cluster1 := &api.Cluster{
		ID: "aws:123456789011:eu-central-1:cluster1",
		InfrastructureAccount: "aws:123456789011",
		LifecycleStatus:       "ready",
		Channel:               "dev",
		Status:                mockStatus,
	}
	cluster2 := &api.Cluster{
		ID: "aws:123456789012:eu-central-1:cluster2",
		InfrastructureAccount: "aws:123456789012",
		LifecycleStatus:       "ready",
		Channel:               "dev",
		Status:                mockStatus,
	}

	clusterList := NewClusterList(config.DefaultFilter, []string{})

	// No clusters yet
	require.Nil(t, clusterList.SelectNext(dummyCancelFunc))

	// One new cluster
	clusterList.UpdateAvailable(defaultChannels, []*api.Cluster{cluster1})
	require.Equal(t, []string{cluster1.ID}, allClusterIds(clusterList))

	// Another new cluster
	clusterList.UpdateAvailable(defaultChannels, []*api.Cluster{cluster1, cluster2})
	require.Equal(t, []string{cluster2.ID, cluster1.ID}, allClusterIds(clusterList))
}

func TestUpdateUpdatesExistingClusters(t *testing.T) {
	cluster := &api.Cluster{
		ID: "aws:123456789011:eu-central-1:cluster1",
		InfrastructureAccount: "aws:123456789011",
		LifecycleStatus:       "requested",
		Channel:               "dev",
		Status:                mockStatus,
	}

	clusterList := NewClusterList(config.DefaultFilter, []string{})

	clusterList.UpdateAvailable(defaultChannels, []*api.Cluster{cluster})

	next := clusterList.SelectNext(dummyCancelFunc)
	require.NotNil(t, next)
	require.Equal(t, cluster.LifecycleStatus, next.Cluster.LifecycleStatus)
	clusterList.ClusterProcessed(next)

	updated := &api.Cluster{
		ID: "aws:123456789011:eu-central-1:cluster1",
		InfrastructureAccount: "aws:123456789011",
		LifecycleStatus:       "ready",
		Channel:               "dev",
		Status:                mockStatus,
	}
	clusterList.UpdateAvailable(defaultChannels, []*api.Cluster{updated})
	next = clusterList.SelectNext(dummyCancelFunc)
	require.NotNil(t, next)
	require.Equal(t, updated.LifecycleStatus, next.Cluster.LifecycleStatus)

	clusterList.ClusterProcessed(next)
	require.Nil(t, clusterList.SelectNext(dummyCancelFunc))
	clusterList.UpdateAvailable(defaultChannels, []*api.Cluster{updated})

	assert.Equal(t, []string{cluster.ID}, allClusterIds(clusterList))
}

func sortedStrings(s []string) []string {
	sort.Strings(s)
	return s
}

func TestUpdateAbortsProcessingIfBlocked(t *testing.T) {
	cluster := &api.Cluster{
		ID: "aws:123456789011:eu-central-1:cluster",
		InfrastructureAccount: "aws:123456789011",
		LifecycleStatus:       "ready",
		Channel:               "dev",
		Status:                mockStatus,
	}

	clusterList := NewClusterList(config.DefaultFilter, []string{})
	clusterList.UpdateAvailable(defaultChannels, []*api.Cluster{cluster})

	ctx, cancelFunc := context.WithCancel(context.Background())
	next := clusterList.SelectNext(cancelFunc)
	require.NotNil(t, next)
	require.NoError(t, ctx.Err())

	updated := &api.Cluster{
		ID: "aws:123456789011:eu-central-1:cluster",
		InfrastructureAccount: "aws:123456789011",
		LifecycleStatus:       "ready",
		Channel:               "dev",
		Status:                mockStatus,
		ConfigItems:           map[string]string{updateBlockedConfigItem: "please don't"},
	}
	clusterList.UpdateAvailable(defaultChannels, []*api.Cluster{updated})
	require.Equal(t, context.Canceled, ctx.Err())
}

func TestUpdateDeletesUnusedClusters(t *testing.T) {
	cluster1 := &api.Cluster{
		ID: "aws:123456789011:eu-central-1:cluster1",
		InfrastructureAccount: "aws:123456789011",
		LifecycleStatus:       "ready",
		Channel:               "dev",
		Status:                mockStatus,
	}
	cluster2 := &api.Cluster{
		ID: "aws:123456789012:eu-central-1:cluster2",
		InfrastructureAccount: "aws:123456789012",
		LifecycleStatus:       "ready",
		Channel:               "dev",
		Status:                mockStatus,
	}

	clusterList := NewClusterList(config.DefaultFilter, []string{})

	clusterList.UpdateAvailable(defaultChannels, []*api.Cluster{cluster1, cluster2})
	require.Equal(t, []string{cluster1.ID, cluster2.ID}, sortedStrings(allClusterIds(clusterList)))

	clusterList.UpdateAvailable(defaultChannels, []*api.Cluster{cluster2})
	require.Equal(t, []string{cluster2.ID}, allClusterIds(clusterList))
}

func TestClusterPriority(t *testing.T) {
	normal := &api.Cluster{
		ID: "aws:123456789011:eu-central-1:normal",
		InfrastructureAccount: "aws:123456789011",
		LifecycleStatus:       "ready",
		Channel:               "dev",
		Status:                mockStatus,
	}
	decommissionRequested := &api.Cluster{
		ID: "aws:123456789012:eu-central-1:decommission-requested",
		InfrastructureAccount: "aws:123456789012",
		LifecycleStatus:       "decommission-requested",
		Channel:               "dev",
		Status:                mockStatus,
	}
	pendingUpdate := &api.Cluster{
		ID: "aws:123456789013:eu-central-1:pendingUpdate",
		InfrastructureAccount: "aws:123456789013",
		LifecycleStatus:       "ready",
		Channel:               "dev",
		Status: &api.ClusterStatus{
			NextVersion:    "abc123",
			CurrentVersion: "def#456",
		},
	}
	normal2 := &api.Cluster{
		ID: "aws:123456789014:eu-central-1:normal-2",
		InfrastructureAccount: "aws:123456789014",
		LifecycleStatus:       "ready",
		Channel:               "dev",
		Status:                mockStatus,
	}

	for _, clusters := range [][]*api.Cluster{
		{normal, decommissionRequested, pendingUpdate},
		{normal, pendingUpdate, decommissionRequested},
		{decommissionRequested, normal, pendingUpdate},
		{decommissionRequested, pendingUpdate, normal},
		{pendingUpdate, normal, decommissionRequested},
		{pendingUpdate, decommissionRequested, normal},
	} {
		clusterList := NewClusterList(config.DefaultFilter, []string{})

		clusterList.UpdateAvailable(defaultChannels, clusters)
		assert.Equal(t, []string{pendingUpdate.ID, decommissionRequested.ID, normal.ID}, allClusterIds(clusterList))

		// add normal2, it should now be updated before normal1
		clusterList.UpdateAvailable(defaultChannels, append(clusters, normal2))
		assert.Equal(t, []string{pendingUpdate.ID, decommissionRequested.ID, normal2.ID, normal.ID}, allClusterIds(clusterList))
	}
}

func TestClusterEnvOrder(t *testing.T) {
	status := &api.ClusterStatus{
		CurrentVersion: "abc123#test",
	}
	channels := channel.NewGitVersions(map[string]channel.ConfigVersion{"dev": "def456"})

	test1 := &api.Cluster{
		ID: "aws:123456789011:eu-central-1:test1",
		InfrastructureAccount: "aws:123456789011",
		LifecycleStatus:       "ready",
		Channel:               "dev",
		Environment:           "test",
		Status:                status,
	}
	test2 := &api.Cluster{
		ID: "aws:123456789012:eu-central-1:test2",
		InfrastructureAccount: "aws:123456789011",
		LifecycleStatus:       "ready",
		Channel:               "dev",
		Environment:           "test",
		Status:                status,
	}
	test3 := &api.Cluster{
		ID: "aws:123456789012:eu-central-1:test3",
		InfrastructureAccount: "aws:123456789011",
		LifecycleStatus:       "ready",
		Channel:               "dev",
		Environment:           "test",
		Status: &api.ClusterStatus{
			CurrentVersion: "",
		},
	}
	prod := &api.Cluster{
		ID: "aws:123456789013:eu-central-1:prod",
		InfrastructureAccount: "aws:123456789011",
		LifecycleStatus:       "ready",
		Channel:               "dev",
		Environment:           "prod",
		Status:                status,
	}
	staging := &api.Cluster{
		ID: "aws:123456789014:eu-central-1:staging",
		InfrastructureAccount: "aws:123456789011",
		LifecycleStatus:       "ready",
		Channel:               "dev",
		Environment:           "staging",
		Status:                status,
	}

	pendingUpdates := func(clusters ...*api.Cluster) []string {
		clusterList := NewClusterList(config.DefaultFilter, []string{"test", "prod"})
		clusterList.UpdateAvailable(channels, clusters)
		return allClusterIds(clusterList)
	}

	// ignore prod: not all test clusters have been updated
	assert.NotContains(t, pendingUpdates(test1, test2, prod), prod.ID)

	// ignore prod: some test clusters have invalid statuses
	assert.NotContains(t, pendingUpdates(test1, test3, prod), prod.ID)

	// allow prod: no clusters in test environment
	assert.Contains(t, pendingUpdates(staging, prod), prod.ID)

	// other environments should work fine
	assert.Contains(t, pendingUpdates(test1, test3, staging), staging.ID)
}

func TestClusterLastUpdated(t *testing.T) {
	clusterList := NewClusterList(config.DefaultFilter, []string{})

	clusters := []*api.Cluster{
		{
			ID: "aws:123456789011:eu-central-1:cluster1",
			InfrastructureAccount: "aws:123456789011",
			LifecycleStatus:       "ready",
			Channel:               "dev",
			Status:                mockStatus,
		},
		{
			ID: "aws:123456789012:eu-central-1:cluster2",
			InfrastructureAccount: "aws:123456789012",
			LifecycleStatus:       "ready",
			Channel:               "dev",
			Status:                mockStatus,
		},
		{
			ID: "aws:123456789013:eu-central-1:cluster3",
			InfrastructureAccount: "aws:123456789013",
			LifecycleStatus:       "ready",
			Channel:               "dev",
			Status:                mockStatus,
		},
	}

	clusterList.UpdateAvailable(defaultChannels, clusters)

	// get the next clusters to process
	next1 := clusterList.SelectNext(dummyCancelFunc)
	require.NotNil(t, next1)

	next2 := clusterList.SelectNext(dummyCancelFunc)
	require.NotNil(t, next2)

	next3 := clusterList.SelectNext(dummyCancelFunc)
	require.NotNil(t, next3)

	require.Nil(t, clusterList.SelectNext(dummyCancelFunc))

	// finish processing in a different order (2->1->3)
	clusterList.ClusterProcessed(next2)
	clusterList.ClusterProcessed(next1)
	clusterList.ClusterProcessed(next3)

	require.Nil(t, clusterList.SelectNext(dummyCancelFunc))

	// the same order should be preserved for next update attempts
	clusterList.UpdateAvailable(defaultChannels, clusters)
	require.Equal(t, []string{next2.Cluster.ID, next1.Cluster.ID, next3.Cluster.ID}, allClusterIds(clusterList))
}

func TestProcessingClusterNotDeleted(t *testing.T) {
	cluster := &api.Cluster{
		ID: "aws:123456789011:eu-central-1:cluster1",
		InfrastructureAccount: "aws:123456789011",
		LifecycleStatus:       "ready",
		Channel:               "dev",
		Status:                mockStatus,
	}

	clusterList := NewClusterList(config.DefaultFilter, []string{})
	clusterList.UpdateAvailable(defaultChannels, []*api.Cluster{cluster})
	next := clusterList.SelectNext(dummyCancelFunc)
	require.NotNil(t, next)
	require.Equal(t, cluster.ID, next.Cluster.ID)

	newError := errors.New("<updated>")
	next.NextError = newError

	// remove the cluster
	clusterList.UpdateAvailable(defaultChannels, []*api.Cluster{})

	// add it back, but it still should be processing
	clusterList.UpdateAvailable(defaultChannels, []*api.Cluster{cluster})
	require.Nil(t, clusterList.SelectNext(dummyCancelFunc))
	require.EqualValues(t, newError, next.NextError)

	// finish processing
	clusterList.ClusterProcessed(next)
	clusterList.UpdateAvailable(defaultChannels, []*api.Cluster{cluster})

	next = clusterList.SelectNext(dummyCancelFunc)
	require.NotNil(t, next)
	require.Equal(t, cluster.ID, next.Cluster.ID)
	require.EqualValues(t, next.NextError, nil)
}

func TestProcessingClusterNotUpdated(t *testing.T) {
	cluster := &api.Cluster{
		ID: "aws:123456789011:eu-central-1:cluster1",
		InfrastructureAccount: "aws:123456789011",
		LifecycleStatus:       "ready",
		Channel:               "dev",
		Status:                mockStatus,
	}

	clusterList := NewClusterList(config.DefaultFilter, []string{})
	clusterList.UpdateAvailable(defaultChannels, []*api.Cluster{cluster})
	next := clusterList.SelectNext(dummyCancelFunc)
	require.NotNil(t, next)
	require.Equal(t, cluster.ID, next.Cluster.ID)

	updated := &api.Cluster{
		ID: "aws:123456789011:eu-central-1:cluster1",
		InfrastructureAccount: "aws:123456789011",
		LifecycleStatus:       "decommission-pending",
		Channel:               "dev",
		Status:                mockStatus,
	}

	clusterList.UpdateAvailable(defaultChannels, []*api.Cluster{updated})
	clusterList.ClusterProcessed(next)

	// cluster should not be overwritten
	require.Equal(t, cluster.LifecycleStatus, next.Cluster.LifecycleStatus)

	// now it should get updated
	clusterList.UpdateAvailable(defaultChannels, []*api.Cluster{updated})
	next2 := clusterList.SelectNext(dummyCancelFunc)
	require.NotNil(t, next2)
	require.Equal(t, updated.LifecycleStatus, next2.Cluster.LifecycleStatus)
}
