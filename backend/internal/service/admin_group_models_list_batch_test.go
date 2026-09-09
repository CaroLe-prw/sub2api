//go:build unit

package service

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

type batchGroupModelAllowlistRepoStub struct {
	*groupRepoStubForAdmin
	batchGroupIDs []int64
	batchConfig   GroupModelAllowlist
}

func (s *batchGroupModelAllowlistRepoStub) BatchUpdateModelAllowlist(_ context.Context, groupIDs []int64, config GroupModelAllowlist) (int, error) {
	s.batchGroupIDs = append([]int64(nil), groupIDs...)
	s.batchConfig = config
	return len(groupIDs), nil
}

func TestAdminService_BatchSetGroupModelAllowlist(t *testing.T) {
	repo := &batchGroupModelAllowlistRepoStub{
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

	affected, err := svc.BatchSetGroupModelAllowlist(context.Background(), []int64{2, 1, 2}, GroupModelAllowlist{
		Enabled: true,
		Models:  []string{" gpt-5 ", "", "gpt-5", "claude-sonnet-4-6"},
	})

	require.NoError(t, err)
	require.Equal(t, 2, affected)
	require.Equal(t, []int64{2, 1}, repo.batchGroupIDs)
	require.Equal(t, GroupModelAllowlist{
		Enabled: true,
		Models:  []string{"gpt-5", "claude-sonnet-4-6"},
	}, repo.batchConfig)
	require.Equal(t, []int64{2, 1}, invalidator.groupIDs)
}

func TestAdminService_BatchSetGroupModelAllowlist_ValidatesEveryGroupBeforeUpdate(t *testing.T) {
	repo := &batchGroupModelAllowlistRepoStub{
		groupRepoStubForAdmin: &groupRepoStubForAdmin{
			getByIDByID: map[int64]*Group{
				1: {ID: 1, Name: "first"},
			},
		},
	}
	svc := &adminServiceImpl{groupRepo: repo}

	affected, err := svc.BatchSetGroupModelAllowlist(context.Background(), []int64{1, 99}, GroupModelAllowlist{Enabled: true, Models: []string{"gpt-5"}})

	require.ErrorIs(t, err, ErrGroupNotFound)
	require.Zero(t, affected)
	require.Nil(t, repo.batchGroupIDs)
}

func TestAdminService_BatchSetGroupModelAllowlist_RejectsInvalidAllowlistBeforeUpdate(t *testing.T) {
	for _, models := range [][]string{nil, {"gpt-*-codex"}} {
		t.Run(fmt.Sprint(models), func(t *testing.T) {
			repo := &batchGroupModelAllowlistRepoStub{groupRepoStubForAdmin: &groupRepoStubForAdmin{
				getByIDByID: map[int64]*Group{1: {ID: 1}},
			}}
			invalidator := &authCacheInvalidatorStub{}
			svc := &adminServiceImpl{groupRepo: repo, authCacheInvalidator: invalidator}
			affected, err := svc.BatchSetGroupModelAllowlist(context.Background(), []int64{1}, GroupModelAllowlist{Enabled: true, Models: models})
			require.Error(t, err)
			require.Zero(t, affected)
			require.Nil(t, repo.batchGroupIDs)
			require.Empty(t, invalidator.groupIDs)
		})
	}
}
