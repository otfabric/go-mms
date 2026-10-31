// SPDX-License-Identifier: MIT

package iso_test

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain fails the package when a test leaves a goroutine running
// (TEST-006, otfabric/engineering standards/concurrency.md#leaks).
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
