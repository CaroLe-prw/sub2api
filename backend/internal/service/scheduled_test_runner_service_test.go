//go:build unit

package service

import "testing"

func TestChannelMonitorPoolAccountEligibleKeepsUnhealthyInventory(t *testing.T) {
	platforms := []string{PlatformOpenAI, PlatformAnthropic}
	tests := []struct {
		name    string
		account *Account
		want    bool
	}{
		{name: "active schedulable", account: &Account{Platform: PlatformOpenAI, Status: StatusActive, Schedulable: true}, want: true},
		{name: "active manually unschedulable", account: &Account{Platform: PlatformOpenAI, Status: StatusActive, Schedulable: false}, want: true},
		{name: "error remains monitored", account: &Account{Platform: PlatformOpenAI, Status: StatusError, Schedulable: false}, want: true},
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
