package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type preferredAccountHandlerRepo struct {
	service.UserPreferredAccountRepository
	args []int64
	err  error
}

func (r *preferredAccountHandlerRepo) Set(_ context.Context, userID, groupID, accountID int64) error {
	r.args = []int64{userID, groupID, accountID}
	return r.err
}

func (r *preferredAccountHandlerRepo) Delete(_ context.Context, userID, groupID int64) error {
	r.args = []int64{userID, groupID}
	return r.err
}

func TestGroupPreferredAccountHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name, method, path, body string
		repoErr                  error
		status                   int
		args                     []int64
	}{
		{"save", http.MethodPut, "/groups/10/preferred-accounts/7", `{"account_id":1}`, nil, 200, []int64{7, 10, 1}},
		{"cross_group", http.MethodPut, "/groups/10/preferred-accounts/7", `{"account_id":1}`, service.ErrInvalidPreferredAccount, 400, []int64{7, 10, 1}},
		{"invalid_account", http.MethodPut, "/groups/10/preferred-accounts/7", `{"account_id":0}`, nil, 400, nil},
		{"invalid_user", http.MethodPut, "/groups/10/preferred-accounts/0", `{"account_id":1}`, nil, 400, nil},
		{"delete", http.MethodDelete, "/groups/10/preferred-accounts/7", "", nil, 200, []int64{7, 10}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &preferredAccountHandlerRepo{err: tc.repoErr}
			h := &GroupHandler{preferredAccounts: service.NewUserPreferredAccountService(repo)}
			router := gin.New()
			router.PUT("/groups/:id/preferred-accounts/:user_id", h.SetPreferredAccount)
			router.DELETE("/groups/:id/preferred-accounts/:user_id", h.DeletePreferredAccount)
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			result := httptest.NewRecorder()
			router.ServeHTTP(result, req)
			require.Equal(t, tc.status, result.Code, result.Body.String())
			require.Equal(t, tc.args, repo.args)
		})
	}
}
