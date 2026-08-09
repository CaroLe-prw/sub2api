//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type batchGroupModelsListConfigRepoStub struct {
	*groupRepoStubForAdmin
	batchGroupIDs []int64
	batchConfig   GroupModelsListConfig
}

func (s *batchGroupModelsListConfigRepoStub) BatchUpdateModelsListConfig(_ context.Context, groupIDs []int64, config GroupModelsListConfig) (int, error) {
	s.batchGroupIDs = append([]int64(nil), groupIDs...)
	s.batchConfig = config
	return len(groupIDs), nil
}

func TestAdminService_BatchSetGroupModelsListConfig(t *testing.T) {
	repo := &batchGroupModelsListConfigRepoStub{
		groupRepoStubForAdmin: &groupRepoStubForAdmin{
			getByIDByID: map[int64]*Group{
				1: {ID: 1, Name: "first"},
				2: {ID: 2, Name: "second"},
			},
		},
	}
	invalidator := &authCacheInvalidatorStub{}
	svc := &adminServiceImpl{
		groupRepo:            repo,
		authCacheInvalidator: invalidator,
	}

	affected, err := svc.BatchSetGroupModelsListConfig(context.Background(), []int64{2, 1, 2}, GroupModelsListConfig{
		Enabled: true,
		Models:  []string{" gpt-5 ", "", "gpt-5", "claude-sonnet-4-6"},
	})

	require.NoError(t, err)
	require.Equal(t, 2, affected)
	require.Equal(t, []int64{2, 1}, repo.batchGroupIDs)
	require.Equal(t, GroupModelsListConfig{
		Enabled: true,
		Models:  []string{"gpt-5", "claude-sonnet-4-6"},
	}, repo.batchConfig)
	require.Equal(t, []int64{2, 1}, invalidator.groupIDs)
}

func TestAdminService_BatchSetGroupModelsListConfig_ValidatesEveryGroupBeforeUpdate(t *testing.T) {
	repo := &batchGroupModelsListConfigRepoStub{
		groupRepoStubForAdmin: &groupRepoStubForAdmin{
			getByIDByID: map[int64]*Group{
				1: {ID: 1, Name: "first"},
			},
		},
	}
	svc := &adminServiceImpl{groupRepo: repo}

	affected, err := svc.BatchSetGroupModelsListConfig(context.Background(), []int64{1, 99}, GroupModelsListConfig{Enabled: true, Models: []string{"gpt-5"}})

	require.ErrorIs(t, err, ErrGroupNotFound)
	require.Zero(t, affected)
	require.Nil(t, repo.batchGroupIDs)
}
