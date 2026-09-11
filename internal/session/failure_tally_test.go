// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

package session

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
)

const credentialAdvice = " — check your LLM configuration and API key"

func TestFailureTallyAllFailedError(t *testing.T) {
	for _, tc := range []struct {
		name string
		add  []FailureClass
		want string
	}{
		{
			name: "all timeouts carry no credential advice",
			add:  []FailureClass{FailureTimeout, FailureTimeout, FailureTimeout},
			want: "all 3 file review(s) failed (timeout: 3)",
		},
		{
			name: "a provider failure keeps the advice and classes keep a fixed order",
			add:  []FailureClass{FailureTimeout, FailureProvider, FailureTimeout},
			want: "all 3 file review(s) failed (provider: 1, timeout: 2)" + credentialAdvice,
		},
		{
			name: "a configuration failure keeps the advice",
			add:  []FailureClass{FailureConfiguration},
			want: "all 3 file review(s) failed (configuration: 1)" + credentialAdvice,
		},
		{
			name: "panics and cancellations carry no advice",
			add:  []FailureClass{FailurePanic, FailureCancelled},
			want: "all 3 file review(s) failed (cancelled: 1, panic: 1)",
		},
		{
			name: "an empty tally keeps the legacy message",
			want: "all 3 file review(s) failed" + credentialAdvice,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var tally FailureTally
			for _, c := range tc.add {
				tally.Add(c)
			}
			if got := tally.AllFailedError("file review", 3).Error(); got != tc.want {
				t.Fatalf("AllFailedError = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestFailureTallyResetClearsCounts(t *testing.T) {
	var tally FailureTally
	tally.Add(FailureTimeout)
	tally.Reset()
	tally.Add(FailureProvider)
	want := "all 1 file scan(s) failed (provider: 1)" + credentialAdvice
	if got := tally.AllFailedError("file scan", 1).Error(); got != want {
		t.Fatalf("AllFailedError after Reset = %q, want %q", got, want)
	}
}

func TestFailureTallyConcurrentAdd(t *testing.T) {
	var tally FailureTally
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tally.Add(FailureTimeout)
		}()
	}
	wg.Wait()
	if got, want := tally.AllFailedError("file review", 64).Error(), "all 64 file review(s) failed (timeout: 64)"; got != want {
		t.Fatalf("AllFailedError = %q, want %q", got, want)
	}
}

func TestClassifyItemError(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want FailureClass
	}{
		{"deadline", context.DeadlineExceeded, FailureTimeout},
		{"deadline_wrapped", fmt.Errorf("provider call timed out: %w", context.DeadlineExceeded), FailureTimeout},
		{"cancelled", context.Canceled, FailureCancelled},
		{"cancelled_wrapped", fmt.Errorf("aborted: %w", context.Canceled), FailureCancelled},
		{"other", errors.New("HTTP 500"), FailureProvider},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClassifyItemError(tc.err); got != tc.want {
				t.Fatalf("ClassifyItemError(%v) = %q, want %q", tc.err, got, tc.want)
			}
		})
	}
}
