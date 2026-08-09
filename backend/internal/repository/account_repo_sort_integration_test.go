//go:build integration

package repository

import (
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func (s *AccountRepoSuite) TestList_DefaultSortByNameAsc() {
	mustCreateAccount(s.T(), s.client, &service.Account{Name: "z-account"})
	mustCreateAccount(s.T(), s.client, &service.Account{Name: "a-account"})

	accounts, _, err := s.repo.List(s.ctx, pagination.PaginationParams{Page: 1, PageSize: 10})
	s.Require().NoError(err)
	s.Require().Len(accounts, 2)
	s.Require().Equal("a-account", accounts[0].Name)
	s.Require().Equal("z-account", accounts[1].Name)
}

func (s *AccountRepoSuite) TestListWithFilters_SortByPriorityDesc() {
	mustCreateAccount(s.T(), s.client, &service.Account{Name: "low-priority", Priority: 10})
	mustCreateAccount(s.T(), s.client, &service.Account{Name: "high-priority", Priority: 90})

	accounts, _, err := s.repo.ListWithFilters(s.ctx, pagination.PaginationParams{
		Page:      1,
		PageSize:  10,
		SortBy:    "priority",
		SortOrder: "desc",
	}, "", "", "", "", 0, "")
	s.Require().NoError(err)
	s.Require().Len(accounts, 2)
	s.Require().Equal("high-priority", accounts[0].Name)
	s.Require().Equal("low-priority", accounts[1].Name)
}

func (s *AccountRepoSuite) TestListWithFilters_SortByUpstreamBillingRateWithMissingLast() {
	makeAccount := func(name, status string, rate any) {
		extra := map[string]any{}
		if rate != nil {
			extra[service.UpstreamBillingProbeExtraKey] = map[string]any{
				"status": status,
				"data":   map[string]any{"effective_rate_multiplier": rate},
			}
		}
		mustCreateAccount(s.T(), s.client, &service.Account{Name: name, Extra: extra})
	}
	makeAccount("high-rate", service.UpstreamBillingProbeStatusOK, 0.8)
	makeAccount("low-rate", service.UpstreamBillingProbeStatusOK, 0.03)
	makeAccount("missing-rate", "", nil)
	makeAccount("unsupported-with-retained-rate", service.UpstreamBillingProbeStatusUnsupported, 0.01)

	for _, tc := range []struct {
		order string
		want  []string
	}{
		{order: "asc", want: []string{"low-rate", "high-rate", "missing-rate", "unsupported-with-retained-rate"}},
		{order: "desc", want: []string{"high-rate", "low-rate", "unsupported-with-retained-rate", "missing-rate"}},
	} {
		accounts, _, err := s.repo.ListWithFilters(s.ctx, pagination.PaginationParams{
			Page: 1, PageSize: 10, SortBy: "upstream_billing_rate", SortOrder: tc.order,
		}, "", "", "", "", 0, "")
		s.Require().NoError(err)
		s.Require().Len(accounts, 4)
		for i, name := range tc.want {
			s.Require().Equal(name, accounts[i].Name)
		}
	}
}

func (s *AccountRepoSuite) TestListWithFilters_SortByUpstreamBalanceWithMissingLast() {
	makeAccount := func(name string, balance any) {
		extra := map[string]any{}
		if balance != nil {
			extra[service.UpstreamBillingProbeExtraKey] = map[string]any{
				"status": service.UpstreamBillingProbeStatusOK,
				"data":   map[string]any{"balance": balance},
			}
		}
		mustCreateAccount(s.T(), s.client, &service.Account{Name: name, Extra: extra})
	}
	makeAccount("high-balance", 80.5)
	makeAccount("low-balance", 3.25)
	makeAccount("missing-balance", nil)
	makeAccount("invalid-balance", "unknown")

	for _, tc := range []struct {
		order string
		want  []string
	}{
		{order: "asc", want: []string{"low-balance", "high-balance", "missing-balance", "invalid-balance"}},
		{order: "desc", want: []string{"high-balance", "low-balance", "invalid-balance", "missing-balance"}},
	} {
		accounts, _, err := s.repo.ListWithFilters(s.ctx, pagination.PaginationParams{
			Page: 1, PageSize: 10, SortBy: "upstream_balance", SortOrder: tc.order,
		}, "", "", "", "", 0, "")
		s.Require().NoError(err)
		s.Require().Len(accounts, 4)
		for i, name := range tc.want {
			s.Require().Equal(name, accounts[i].Name)
		}
	}
}

func (s *AccountRepoSuite) TestListWithFilters_SortByCurrentUpstreamBillingRateDuringPeak() {
	now := time.Now()
	locations := []string{"UTC", "Asia/Shanghai", "America/New_York", "Europe/London"}
	var timezone string
	var minute int
	for _, name := range locations {
		location, err := time.LoadLocation(name)
		s.Require().NoError(err)
		local := now.In(location)
		candidate := local.Hour()*60 + local.Minute()
		if candidate >= 2 && candidate <= 1436 {
			timezone = name
			minute = candidate
			break
		}
	}
	s.Require().NotEmpty(timezone)

	peakStart := fmt.Sprintf("%02d:%02d", (minute-2)/60, (minute-2)%60)
	peakEnd := fmt.Sprintf("%02d:%02d", (minute+3)/60, (minute+3)%60)
	mustCreateAccount(s.T(), s.client, &service.Account{
		Name: "current-peak-rate",
		Extra: map[string]any{
			service.UpstreamBillingProbeExtraKey: map[string]any{
				"status": service.UpstreamBillingProbeStatusOK,
				"data": map[string]any{
					"billing_scope":             "token",
					"resolved_rate_multiplier":  1.0,
					"effective_rate_multiplier": 1.0,
					"peak_rate_enabled":         true,
					"peak_start":                peakStart,
					"peak_end":                  peakEnd,
					"peak_rate_multiplier":      10.0,
					"timezone":                  timezone,
				},
			},
		},
	})
	mustCreateAccount(s.T(), s.client, &service.Account{
		Name: "current-off-peak-rate",
		Extra: map[string]any{
			service.UpstreamBillingProbeExtraKey: map[string]any{
				"status": service.UpstreamBillingProbeStatusOK,
				"data": map[string]any{
					"effective_rate_multiplier": 5.0,
				},
			},
		},
	})

	for _, tc := range []struct {
		order string
		want  []string
	}{
		{order: "asc", want: []string{"current-off-peak-rate", "current-peak-rate"}},
		{order: "desc", want: []string{"current-peak-rate", "current-off-peak-rate"}},
	} {
		accounts, _, err := s.repo.ListWithFilters(s.ctx, pagination.PaginationParams{
			Page: 1, PageSize: 10, SortBy: "upstream_billing_rate", SortOrder: tc.order,
		}, "", "", "", "", 0, "")
		s.Require().NoError(err)
		s.Require().Len(accounts, 2)
		for i, name := range tc.want {
			s.Require().Equal(name, accounts[i].Name)
		}
	}
}

func (s *AccountRepoSuite) TestListWithFilters_SortByCalibratedUpstreamBillingRate() {
	makeAccount := func(name string, rate, calibration float64) {
		mustCreateAccount(s.T(), s.client, &service.Account{
			Name: name,
			Extra: map[string]any{
				service.OpenAIUpstreamRateCalibrationExtraKey: calibration,
				service.UpstreamBillingProbeExtraKey: map[string]any{
					"status": service.UpstreamBillingProbeStatusOK,
					"data": map[string]any{
						"billing_scope":             "token",
						"resolved_rate_multiplier":  rate,
						"effective_rate_multiplier": rate,
						"peak_rate_enabled":         false,
					},
				},
			},
		})
	}
	makeAccount("raw-cheap-calibrated-expensive", 0.03, 3)
	makeAccount("raw-expensive-calibrated-cheap", 0.8, 0.1)

	accounts, _, err := s.repo.ListWithFilters(s.ctx, pagination.PaginationParams{
		Page: 1, PageSize: 10, SortBy: "upstream_billing_rate", SortOrder: "asc",
	}, "", "", "", "", 0, "")
	s.Require().NoError(err)
	s.Require().Len(accounts, 2)
	s.Require().Equal("raw-expensive-calibrated-cheap", accounts[0].Name)
	s.Require().Equal("raw-cheap-calibrated-expensive", accounts[1].Name)
}
