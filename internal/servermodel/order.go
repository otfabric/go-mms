// SPDX-License-Identifier: MIT

package servermodel

import "slices"

// InsertSorted inserts name into the ascending slice order at its sorted
// position and returns the result. The caller guarantees that name is not
// already present. A registration costs O(log n) comparisons and one
// copy, where appending and re-sorting cost O(n log n) per call and made
// registering a model of n variables quadratic.
func InsertSorted(order []string, name string) []string {
	i, _ := slices.BinarySearch(order, name)
	return slices.Insert(order, i, name)
}
