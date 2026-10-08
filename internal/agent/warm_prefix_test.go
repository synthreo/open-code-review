// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

package agent

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/alibaba/open-code-review/internal/llm"
	"github.com/alibaba/open-code-review/internal/model"
)

// orderingClient records when each request starts and ends. The first request is held
// briefly, so a request started while it is in flight shows up as a start before its end.
type orderingClient struct {
	mu     sync.Mutex
	events []string
	calls  int
}

func (c *orderingClient) CompletionsWithCtx(ctx context.Context, _ llm.ChatRequest) (*llm.ChatResponse, error) {
	c.mu.Lock()
	c.calls++
	n := c.calls
	c.events = append(c.events, "start")
	c.mu.Unlock()
	if n == 1 {
		select {
		case <-time.After(200 * time.Millisecond):
		case <-ctx.Done():
		}
	}
	c.mu.Lock()
	c.events = append(c.events, "end")
	c.mu.Unlock()
	return agentTaskDoneResponse(), nil
}

// DEV-12640: a provider caches a prompt prefix only once some request has computed it, so files
// whose first requests all start together each pay full price for the run's shared background
// and checklist. Measured in prod on 2026-10-08: a job's first-round calls started within ~100ms
// of each other and each read only the ~1k-token system prefix from cache, while calls started
// after one request had finished read ~16k. The run's first request must therefore complete
// before any other file's request starts; later requests stay parallel.
func TestFirstRequestCompletesBeforeTheFanOut(t *testing.T) {
	client := &orderingClient{}
	a := prefixTestAgent(t, client)
	a.diffs = []model.Diff{
		{NewPath: "pkg/a.go", OldPath: "pkg/a.go", Diff: "+a", Insertions: 1},
		{NewPath: "pkg/b.go", OldPath: "pkg/b.go", Diff: "+b", Insertions: 1},
		{NewPath: "pkg/c.go", OldPath: "pkg/c.go", Diff: "+c", Insertions: 1},
	}
	if _, err := a.dispatchSubtasks(context.Background()); err != nil {
		t.Fatalf("dispatchSubtasks: %v", err)
	}

	client.mu.Lock()
	defer client.mu.Unlock()
	if client.calls != 3 {
		t.Fatalf("requests = %d, want 3", client.calls)
	}
	if client.events[0] != "start" || client.events[1] != "end" {
		t.Fatalf("a second request started while the first was in flight: %v", client.events)
	}
}

// The gate holds only the fan-out of the first round. A run with one file has nothing to wait for.
func TestSingleFileRunDoesNotWait(t *testing.T) {
	client := &orderingClient{}
	a := prefixTestAgent(t, client)
	a.diffs = a.diffs[:1]
	if _, err := a.dispatchSubtasks(context.Background()); err != nil {
		t.Fatalf("dispatchSubtasks: %v", err)
	}
	if client.calls != 1 {
		t.Fatalf("requests = %d, want 1", client.calls)
	}
}
