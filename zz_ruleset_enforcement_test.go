package mms

import "testing"

// Disposable: a failing test that ruleset 27169441 must stop from merging. Never merged.
func TestRulesetEnforcementDisposable(t *testing.T) {
	t.Fatal("disposable failing test: the required check must block this pull request")
}
