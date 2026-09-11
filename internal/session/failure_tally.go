// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

package session

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
)

// failureClassOrder fixes the order classes are reported in, so the all-failed
// message is stable from run to run.
var failureClassOrder = []FailureClass{
	FailureProvider, FailureTimeout, FailureCancelled, FailureConfiguration,
	FailureInput, FailureBudget, FailurePanic, FailureUnknown,
}

// ClassifyItemError maps a per-item error to the class its structure proves,
// recognizing context sentinels through wrapping. Anything that is neither a
// deadline nor a cancellation is attributed to the provider call.
func ClassifyItemError(err error) FailureClass {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return FailureTimeout
	case errors.Is(err, context.Canceled):
		return FailureCancelled
	default:
		return FailureProvider
	}
}

// FailureTally counts failed items by class. The zero value is ready to use and
// it is safe for the concurrent dispatch goroutines of one run.
type FailureTally struct {
	mu     sync.Mutex
	counts map[FailureClass]int64
}

// Add records one failed item of class c.
func (t *FailureTally) Add(c FailureClass) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.counts == nil {
		t.counts = make(map[FailureClass]int64)
	}
	t.counts[c]++
}

// Reset clears every count.
func (t *FailureTally) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.counts = nil
}

// AllFailedError builds the error for a run in which every one of dispatched
// items failed; noun names one item, such as "file review". The message lists
// the classes counted and advises checking LLM configuration only when a
// provider or configuration failure is among them, because a run whose files
// all timed out or panicked had working credentials.
func (t *FailureTally) AllFailedError(noun string, dispatched int64) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	parts := make([]string, 0, len(t.counts))
	for _, c := range failureClassOrder {
		if n := t.counts[c]; n > 0 {
			parts = append(parts, fmt.Sprintf("%s: %d", c, n))
		}
	}
	msg := fmt.Sprintf("all %d %s(s) failed", dispatched, noun)
	if len(parts) > 0 {
		msg += " (" + strings.Join(parts, ", ") + ")"
	}
	// An empty tally carries no evidence either way, so it keeps the legacy advice.
	if len(parts) == 0 || t.counts[FailureProvider] > 0 || t.counts[FailureConfiguration] > 0 {
		msg += " — check your LLM configuration and API key"
	}
	return errors.New(msg)
}
