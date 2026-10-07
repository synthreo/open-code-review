// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

package agent

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/alibaba/open-code-review/internal/config/template"
	"github.com/alibaba/open-code-review/internal/llm"
	"github.com/alibaba/open-code-review/internal/model"
	"github.com/alibaba/open-code-review/internal/session"
	"github.com/alibaba/open-code-review/internal/tool"
)

const (
	prefixTestBackground = "BACKGROUND-SENTINEL repository review guidance shared by every file in the run"
	prefixTestChecklist  = "CHECKLIST-SENTINEL language rules shared by every file of this language"
)

type fixedRule string

func (r fixedRule) Resolve(string) string { return string(r) }

// recordingClient answers every request with `reply` and keeps each request's user text.
type recordingClient struct {
	mu    sync.Mutex
	users []string
	reply func() *llm.ChatResponse
}

func (c *recordingClient) CompletionsWithCtx(_ context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, m := range req.Messages {
		if m.Role == "user" {
			c.users = append(c.users, m.ExtractText())
			break
		}
	}
	return c.reply(), nil
}

func commonPrefix(a, b string) string {
	n := 0
	for n < len(a) && n < len(b) && a[n] == b[n] {
		n++
	}
	return a[:n]
}

func prefixTestAgent(t *testing.T, client llm.LLMClient) *Agent {
	t.Helper()
	tpl, err := template.LoadDefault()
	if err != nil {
		t.Fatalf("LoadDefault: %v", err)
	}
	a := New(Args{
		LLMClient:  client,
		Model:      "test",
		Session:    session.New(t.TempDir(), "main", "test", session.SessionOptions{ReviewMode: "diff"}),
		Tools:      tool.NewRegistry(),
		Template:   *tpl,
		Background: prefixTestBackground,
		SystemRule: fixedRule(prefixTestChecklist),
		MainToolDefs: []llm.ToolDef{
			{Type: "function", Function: llm.FunctionDef{Name: "task_done", Description: "done"}},
		},
	})
	a.currentDate = "2026-10-07 10:00"
	a.diffs = []model.Diff{
		{NewPath: "pkg/alpha.go", OldPath: "pkg/alpha.go", Diff: "+alpha change", Insertions: 1},
		{NewPath: "pkg/beta.go", OldPath: "pkg/beta.go", Diff: "+beta change", Insertions: 1},
	}
	return a
}

// Two files of one run must send the run's background and checklist as a SHARED prefix of the user
// turn: providers cache by prefix, and a background placed after the file's own diff is re-billed
// for every file (DEV-12640: ~14k tokens of REVIEW.md per file on synth_auth and synth_front).
func TestMainTaskSharesBackgroundAndChecklistAcrossFiles(t *testing.T) {
	client := &recordingClient{reply: agentTaskDoneResponse}
	a := prefixTestAgent(t, client)
	for _, d := range a.diffs {
		if _, _, err := a.executeSubtask(context.Background(), d); err != nil {
			t.Fatalf("executeSubtask(%s): %v", d.NewPath, err)
		}
	}
	if len(client.users) != 2 {
		t.Fatalf("main-task requests = %d, want 2 (plan phase is below threshold)", len(client.users))
	}
	shared := commonPrefix(client.users[0], client.users[1])
	for _, want := range []string{prefixTestBackground, prefixTestChecklist} {
		if !strings.Contains(shared, want) {
			t.Errorf("shared user prefix of two files lacks %q; prefix ends %q", want, tail(shared))
		}
	}
}

func TestPlanTaskSharesBackgroundAndChecklistAcrossFiles(t *testing.T) {
	plan := "plan"
	client := &recordingClient{reply: func() *llm.ChatResponse {
		return &llm.ChatResponse{Choices: []llm.Choice{{Message: llm.ResponseMessage{Content: &plan}}}}
	}}
	a := prefixTestAgent(t, client)
	for _, d := range a.diffs {
		if _, err := a.executePlanPhase(context.Background(), d.NewPath, d.Diff, a.buildChangeFilesExcept(d.NewPath), prefixTestChecklist); err != nil {
			t.Fatalf("executePlanPhase(%s): %v", d.NewPath, err)
		}
	}
	if len(client.users) != 2 {
		t.Fatalf("plan requests = %d, want 2", len(client.users))
	}
	shared := commonPrefix(client.users[0], client.users[1])
	for _, want := range []string{prefixTestBackground, prefixTestChecklist} {
		if !strings.Contains(shared, want) {
			t.Errorf("shared user prefix of two files lacks %q; prefix ends %q", want, tail(shared))
		}
	}
}

func tail(s string) string {
	if len(s) > 80 {
		return s[len(s)-80:]
	}
	return s
}
