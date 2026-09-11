// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

package llmloop

import (
	"testing"

	"github.com/alibaba/open-code-review/internal/session"
)

// Only the declared round budget classifies as budget; every other stop is the
// unknown catch-all, and no reason is empty.
func TestMainLoopStopFailureClass(t *testing.T) {
	for _, tc := range []struct {
		stop MainLoopStop
		want session.FailureClass
	}{
		{StopMaxRounds, session.FailureBudget},
		{StopEmptyRounds, session.FailureUnknown},
		{StopCompression, session.FailureUnknown},
		{StopNone, session.FailureUnknown},
	} {
		class, reason := tc.stop.FailureClass()
		if class != tc.want {
			t.Errorf("MainLoopStop(%d).FailureClass() = %q, want %q", tc.stop, class, tc.want)
		}
		if reason == "" {
			t.Errorf("MainLoopStop(%d).FailureClass() returned an empty reason", tc.stop)
		}
	}
}
