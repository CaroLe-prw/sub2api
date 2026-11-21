//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestAdminGroupCandyModelCreateAndUpdate(t *testing.T) {
	repo := &groupRepoStubForAdmin{}
	svc := &adminServiceImpl{groupRepo: repo}
	_, err := svc.CreateGroup(context.Background(), &CreateGroupInput{
		Name: "Candy", Platform: PlatformOpenAI, RateMultiplier: 1, CandyTestModel: " gpt-6.1-sol ",
	})
	require.NoError(t, err)
	require.Equal(t, "gpt-6.1-sol", repo.created.CandyTestModel)
	repo.getByID = repo.created
	repo.getByID.ID = 1
	_, err = svc.UpdateGroup(context.Background(), 1, &UpdateGroupInput{Name: "Renamed"})
	require.NoError(t, err)
	require.Equal(t, "gpt-6.1-sol", repo.updated.CandyTestModel)
	model := "gpt-6.2-sol"
	_, err = svc.UpdateGroup(context.Background(), 1, &UpdateGroupInput{CandyTestModel: &model})
	require.NoError(t, err)
	require.Equal(t, "gpt-6.2-sol", repo.updated.CandyTestModel)
	model = ""
	_, err = svc.UpdateGroup(context.Background(), 1, &UpdateGroupInput{CandyTestModel: &model})
	require.NoError(t, err)
	require.Empty(t, repo.updated.CandyTestModel)
}

func TestAdminGroupCandyModelInvalidAndOtherPlatforms(t *testing.T) {
	repo := &groupRepoStubForAdmin{}
	svc := &adminServiceImpl{groupRepo: repo}
	_, err := svc.CreateGroup(context.Background(), &CreateGroupInput{
		Name: "Bad model", Platform: PlatformOpenAI, RateMultiplier: 1, CandyTestModel: "gpt-6.2 sol",
	})
	require.Error(t, err)
	require.Nil(t, repo.created)
	_, err = svc.CreateGroup(context.Background(), &CreateGroupInput{
		Name: "Other platform", Platform: PlatformAnthropic, RateMultiplier: 1, CandyTestModel: "gpt-6.2-sol",
	})
	require.NoError(t, err)
	require.Empty(t, repo.created.CandyTestModel)
}

func TestAdminGroupCandyModelWorksInSimpleMode(t *testing.T) {
	repo := &groupRepoStubForAdmin{}
	svc := &adminServiceImpl{groupRepo: repo, cfg: &config.Config{RunMode: config.RunModeSimple}}
	_, err := svc.CreateGroup(context.Background(), &CreateGroupInput{Name: "Simple", Platform: PlatformOpenAI, RateMultiplier: 10, CandyTestModel: "gpt-6.1-sol"})
	require.NoError(t, err)
	require.Equal(t, "gpt-6.1-sol", repo.created.CandyTestModel)
	require.Equal(t, float64(1), repo.created.RateMultiplier)
	repo.getByID = repo.created
	model := "gpt-6.2-sol"
	_, err = svc.UpdateGroup(context.Background(), 1, &UpdateGroupInput{CandyTestModel: &model})
	require.NoError(t, err)
	require.Equal(t, model, repo.updated.CandyTestModel)
}

func TestAdminGroupCandyIntervalCreateUpdateAndSimpleMode(t *testing.T) {
	for _, simple := range []bool{false, true} {
		repo := &groupRepoStubForAdmin{}
		svc := &adminServiceImpl{groupRepo: repo}
		if simple {
			svc.cfg = &config.Config{RunMode: config.RunModeSimple}
		}
		_, err := svc.CreateGroup(context.Background(), &CreateGroupInput{Name: "Timed", Platform: PlatformOpenAI, RateMultiplier: 1, CandyTestIntervalSeconds: 60})
		require.NoError(t, err)
		require.Equal(t, 60, repo.created.CandyTestIntervalSeconds)
		repo.getByID = repo.created
		_, err = svc.UpdateGroup(context.Background(), 1, &UpdateGroupInput{Name: "Renamed"})
		require.NoError(t, err)
		require.Equal(t, 60, repo.updated.CandyTestIntervalSeconds)
		for _, seconds := range []int{1, 86400, 0} {
			_, err = svc.UpdateGroup(context.Background(), 1, &UpdateGroupInput{CandyTestIntervalSeconds: &seconds})
			require.NoError(t, err)
			require.Equal(t, seconds, repo.updated.CandyTestIntervalSeconds)
		}
	}
}

func TestAdminGroupCandyIntervalValidation(t *testing.T) {
	for _, seconds := range []int{-1, 86401} {
		repo := &groupRepoStubForAdmin{}
		svc := &adminServiceImpl{groupRepo: repo}
		_, err := svc.CreateGroup(context.Background(), &CreateGroupInput{Name: "Invalid", Platform: PlatformOpenAI, RateMultiplier: 1, CandyTestIntervalSeconds: seconds})
		require.Error(t, err)
		require.Nil(t, repo.created)
		repo.getByID = &Group{ID: 1, Platform: PlatformOpenAI, RateMultiplier: 1}
		_, err = svc.UpdateGroup(context.Background(), 1, &UpdateGroupInput{CandyTestIntervalSeconds: &seconds})
		require.Error(t, err)
		require.Nil(t, repo.updated)
	}
	repo := &groupRepoStubForAdmin{}
	svc := &adminServiceImpl{groupRepo: repo}
	_, err := svc.CreateGroup(context.Background(), &CreateGroupInput{Name: "Disabled", Platform: PlatformOpenAI, RateMultiplier: 1})
	require.NoError(t, err)
	require.Zero(t, repo.created.CandyTestIntervalSeconds)
	_, err = svc.CreateGroup(context.Background(), &CreateGroupInput{Name: "Other", Platform: PlatformAnthropic, RateMultiplier: 1, CandyTestIntervalSeconds: 60})
	require.NoError(t, err)
	require.Zero(t, repo.created.CandyTestIntervalSeconds)
}
