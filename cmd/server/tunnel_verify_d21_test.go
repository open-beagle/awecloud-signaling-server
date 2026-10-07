package main

import (
	"errors"
	"testing"
)

// D2-1 候选集为空时的判定：只有「未接入治理」才是已知豁免，其余都必须 FAIL。
func TestClassifyEmptyCandidates(t *testing.T) {
	cases := []struct {
		name     string
		eligible bool
		err      error
	}{
		{name: "not onboarded -> exempt", eligible: false},
		{name: "onboarded but empty -> fail", eligible: true},
		{name: "lookup error -> fail", err: errors.New("db down")},
	}
	for _, tc := range cases {
		check := CheckResult{ID: "D2-1"}
		classifyEmptyCandidates(&check, tc.eligible, tc.err)
		wantExempt := !tc.eligible && tc.err == nil
		if check.Exempt != wantExempt || check.Passed || check.Skipped || check.Message == "" {
			t.Fatalf("%s: got %+v", tc.name, check)
		}
	}
}
