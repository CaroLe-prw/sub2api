package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGroupModelMappingImagesValidatesTargetAndPreservesRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	group := &Group{ID: 7, Platform: PlatformOpenAI, ModelMapping: map[string]string{"picture-alias": "gpt-image-1", "gpt-image-1": "should-not-recurse"}}
	body := []byte(`{"model":"picture-alias","prompt":"cat","size":"1024x1024"}`)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", strings.NewReader(string(body)))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), ctxkey.Group, group))
	svc := &OpenAIGatewayService{}
	parsed, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)
	require.Equal(t, "picture-alias", parsed.Model)
	mapping, _ := svc.ResolveChannelMappingAndRestrict(c.Request.Context(), &group.ID, parsed.Model)
	require.Equal(t, "gpt-image-1", mapping.MappedModel)
}
