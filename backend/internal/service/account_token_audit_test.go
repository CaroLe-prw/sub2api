package service

import (
	"testing"

	"github.com/tiktoken-go/tokenizer"
)

func TestBuildTokenAuditSamplesAreStable(t *testing.T) {
	codec, err := tokenizer.Get(tokenAuditEncoding)
	if err != nil {
		t.Fatalf("load tokenizer: %v", err)
	}
	samples, err := buildTokenAuditSamples(codec)
	if err != nil {
		t.Fatalf("build samples: %v", err)
	}
	want := []int{64, 256, 1024, 4096, 256, 256}
	if len(samples) != len(want) {
		t.Fatalf("sample count = %d, want %d", len(samples), len(want))
	}
	for i, sample := range samples {
		got, err := codec.Count(sample.text)
		if err != nil {
			t.Fatalf("count %s: %v", sample.name, err)
		}
		if got != want[i] {
			t.Errorf("count %s = %d, want %d", sample.name, got, want[i])
		}
		if tokenAuditHash(sample.text) == "" {
			t.Errorf("hash %s is empty", sample.name)
		}
	}
}

func TestFitTokenAudit(t *testing.T) {
	samples := []TokenAuditSample{
		{Kind: "english", LocalTokens: 64, InputTokens: tokenAuditInt(4450)},
		{Kind: "english", LocalTokens: 256, InputTokens: tokenAuditInt(4642)},
		{Kind: "english", LocalTokens: 1024, InputTokens: tokenAuditInt(5410)},
		{Kind: "english", LocalTokens: 4096, InputTokens: tokenAuditInt(8482)},
	}
	fit := fitTokenAudit(samples, true)
	if fit == nil || fit.Intercept != 4386 || fit.Slope != 1 || fit.R2 != 1 {
		t.Fatalf("unexpected fit: %#v", fit)
	}
}
