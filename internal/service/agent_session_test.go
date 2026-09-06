package service

import (
	"testing"

	"github/hchw/kianshu/internal/openai"
)

func TestFixMessageHistoryDropsIncompleteToolRound(t *testing.T) {
	content := "ok"
	msgs := []openai.Message{
		{Role: "system", Content: &content},
		{Role: "assistant", ToolCalls: []openai.ToolCall{{ID: "call-1", Function: openai.FunctionCall{Name: "create_node"}}}},
	}
	got := fixMessageHistory(msgs)
	if len(got) != 1 || got[0].Role != "system" {
		t.Fatalf("expected incomplete round removed, got %#v", got)
	}
}

func TestFixMessageHistoryKeepsCompleteToolRound(t *testing.T) {
	content := "ok"
	result := "{}"
	msgs := []openai.Message{
		{Role: "system", Content: &content},
		{Role: "assistant", ToolCalls: []openai.ToolCall{{ID: "call-1", Function: openai.FunctionCall{Name: "create_node"}}}},
		{Role: "tool", ToolCallID: "call-1", Content: &result},
	}
	got := fixMessageHistory(msgs)
	if len(got) != len(msgs) {
		t.Fatalf("complete tool round was truncated: %#v", got)
	}
}

func TestDedupeSliceOnlyRemovesConsecutiveValues(t *testing.T) {
	got := dedupeSlice([]string{"a", "a", "b", "a", "a"})
	want := []string{"a", "b", "a"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}
