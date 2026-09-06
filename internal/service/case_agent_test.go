package service

import (
	"context"
	"testing"

	"github/hchw/kianshu/internal/caseflow"
	"github/hchw/kianshu/internal/openai"
)

type caseFakeProvider struct {
	calls   int
	payload func(int) openai.Message
}

func (f *caseFakeProvider) ChatCompletion(context.Context, openai.Provider, openai.CompletionRequest) (*openai.CompletionResponse, error) {
	f.calls++
	msg := f.payload(f.calls)
	return &openai.CompletionResponse{Choices: []struct {
		Message openai.Message `json:"message"`
	}{{Message: msg}}}, nil
}
func (f *caseFakeProvider) GetBaseURL() string     { return "" }
func (f *caseFakeProvider) GetAPIKey() string      { return "" }
func (f *caseFakeProvider) GetModel() string       { return "fake" }
func (f *caseFakeProvider) GetStrictContent() bool { return false }

func TestCaseAgentIncrementalApplyThenFinish(t *testing.T) {
	db := setupCaseFlowDB(t)
	cf, err := CreateCaseFlow(db, 1, 1, "agent流", []SourceInput{{Kind: "all"}})
	if err != nil {
		t.Fatal(err)
	}
	view, _ := GetCaseTreeView(db, cf.ID)
	rootID := view.Tree.Root.ID

	provider := &caseFakeProvider{payload: func(n int) openai.Message {
		if n == 1 {
			return openai.Message{Role: "assistant", ToolCalls: []openai.ToolCall{{ID: "t1", Function: openai.FunctionCall{Name: caseToolAddNode, Arguments: `{"parent_id":"` + rootID + `","title":"登录用例"}`}}}}
		}
		content := "完成"
		return openai.Message{Role: "assistant", Content: &content}
	}}
	res, err := RunCaseAgent(context.Background(), db, cf.ID, 1, CaseAgentOptions{Provider: provider, Instruction: "补充登录用例"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Finished {
		t.Fatalf("expected finished, got %#v", res)
	}

	view, _ = GetCaseTreeView(db, cf.ID)
	if len(view.Tree.Root.Children) != 1 || view.Tree.Root.Children[0].Title != "登录用例" {
		t.Fatalf("mutation not persisted: %#v", view.Tree)
	}
	if view.Tree.Root.Children[0].Status != caseflow.StatusUncovered {
		t.Fatal("new node should be uncovered")
	}
}
