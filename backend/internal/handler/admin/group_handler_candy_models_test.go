package admin

import (
	"strings"
	"testing"

	"github.com/gin-gonic/gin/binding"
	"github.com/stretchr/testify/require"
)

func TestGroupRequestAcceptsOrderedCandyModels(t *testing.T) {
	models := strings.Repeat("a", 80) + "\n" + strings.Repeat("b", 80)
	require.NoError(t, binding.Validator.ValidateStruct(&CreateGroupRequest{
		Name: "Ordered candy", Platform: "openai", CandyTestModel: models,
	}))
	require.NoError(t, binding.Validator.ValidateStruct(&UpdateGroupRequest{CandyTestModel: &models}))
	tooLong := strings.Repeat("a", 2049)
	require.Error(t, binding.Validator.ValidateStruct(&CreateGroupRequest{Name: "Bad", CandyTestModel: tooLong}))
	require.Error(t, binding.Validator.ValidateStruct(&UpdateGroupRequest{CandyTestModel: &tooLong}))
}
