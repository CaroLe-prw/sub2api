//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestChannelMonitorPoolAccountEligibleRequiresEnabledNonOAuthAccount(t *testing.T) {
	platforms := []string{PlatformOpenAI, PlatformAnthropic}
	tests := []struct {
		name    string
		account *Account
		want    bool
	}{
		{name: "active api key", account: &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true}, want: true},
		{name: "oauth excluded", account: &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true}, want: false},
		{name: "unschedulable excluded", account: &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: false}, want: false},
		{name: "error excluded", account: &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusError, Schedulable: true}, want: false},
		{name: "disabled is excluded", account: &Account{Platform: PlatformOpenAI, Status: StatusDisabled}, want: false},
		{name: "expired is excluded", account: &Account{Platform: PlatformOpenAI, Status: StatusExpired}, want: false},
		{name: "unsupported platform", account: &Account{Platform: "custom", Status: StatusActive}, want: false},
		{name: "nil account", account: nil, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := channelMonitorPoolAccountEligible(tt.account, platforms); got != tt.want {
				t.Fatalf("channelMonitorPoolAccountEligible() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestChannelMonitorAccountWhitelistNarrowsGlobalPolicy(t *testing.T) {
	account := &Account{Extra: map[string]any{
		ChannelMonitorAccountModelWhitelistExtraKey: []any{"gpt-5.6-sol", "gpt-5.6-sol"},
	}}

	channelWhitelist := channelMonitorAccountModelWhitelist(account)
	models := filterAutoMonitorModels([]string{"gpt-5.6-sol", "gpt-5.6-terra", "gpt-4.1"}, []string{"gpt-5.*"})
	models = filterAutoMonitorModels(models, channelWhitelist)

	require.Equal(t, []string{"gpt-5.6-sol"}, channelWhitelist)
	require.Equal(t, []string{"gpt-5.6-sol"}, models)
}
