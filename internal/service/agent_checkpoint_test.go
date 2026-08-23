package service

import (
	"testing"

	"github/hchw/kianshu/internal/flow"
)

func TestShouldCheckpoint(t *testing.T) {
	for _, tc := range []struct {
		rounds         int
		highRisk, want bool
	}{
		{1, false, false}, {2, false, false}, {3, false, true}, {1, true, true},
	} {
		if got := shouldCheckpoint(tc.rounds, tc.highRisk); got != tc.want {
			t.Errorf("shouldCheckpoint(%d, %v) = %v, want %v", tc.rounds, tc.highRisk, got, tc.want)
		}
	}
}

func TestValidationDeltaOnlyReturnsNewFindings(t *testing.T) {
	repeated := flow.ValidationError{NodeID: "n1", Code: "x", Level: flow.LevelError, Message: "same"}
	newFinding := flow.ValidationError{NodeID: "n2", Code: "y", Level: flow.LevelWarning, Message: "new"}
	got := validationDelta(flow.Result{Errors: []flow.ValidationError{repeated}}, flow.Result{Errors: []flow.ValidationError{repeated}, Warnings: []flow.ValidationError{newFinding}})
	if len(got.Errors) != 0 || len(got.Warnings) != 1 || got.Warnings[0].Code != "y" {
		t.Fatalf("增量诊断不正确: %#v", got)
	}
}
