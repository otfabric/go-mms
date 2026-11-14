// SPDX-License-Identifier: MIT

package mms_test

import "testing"

// Disposable pilot branch, never merged: proves a failing test fails the gate.
func TestPilotGateNegative(t *testing.T) {
	t.Fatal("deliberate failure on a disposable pilot branch")
}
