package mms

import (
	"github.com/otfabric/go-mms/internal/pdu"
)

// applyAlternateAccessRead traverses a value according to the
// alternate access selector chain and returns the selected sub-value.
// The typeSpec is used to resolve component-by-name selectors.
// Returns nil if the path is invalid (wrong type, out-of-bounds, etc.).
func applyAlternateAccessRead(val *Value, ts *TypeSpec, selectors []pdu.AccessSelectorWire) *Value {
	for _, sel := range selectors {
		if val == nil {
			return nil
		}
		val, ts = applySingleSelector(val, ts, sel)
	}
	return val
}

// applyAlternateAccessWrite performs a read-modify-write: it clones
// the full current value, patches the selected sub-element with the
// incoming write value, and returns the patched full value.
// Returns nil if the selector path is invalid.
func applyAlternateAccessWrite(current *Value, ts *TypeSpec, selectors []pdu.AccessSelectorWire, writeVal *Value) *Value {
	if len(selectors) == 0 {
		return writeVal
	}

	patched := current.Clone()
	if !patchValue(patched, ts, selectors, writeVal) {
		return nil
	}
	return patched
}

// patchValue recursively descends into the cloned value tree and
// replaces the element identified by the selector chain.
func patchValue(val *Value, ts *TypeSpec, selectors []pdu.AccessSelectorWire, writeVal *Value) bool {
	if len(selectors) == 0 || val == nil {
		return false
	}

	sel := selectors[0]
	remaining := selectors[1:]

	switch {
	case sel.Component != "":
		return patchComponent(val, ts, sel.Component, remaining, writeVal)
	case sel.HasIndex:
		return patchIndex(val, ts, sel.Index, remaining, writeVal)
	case sel.IndexRange != nil:
		return patchRange(val, sel.IndexRange.LowIndex, sel.IndexRange.NumberOfElements, remaining, writeVal)
	default:
		return false
	}
}

func patchComponent(val *Value, ts *TypeSpec, name string, remaining []pdu.AccessSelectorWire, writeVal *Value) bool {
	if val.typ != ValueTypeStructure {
		return false
	}
	idx := resolveComponentIndex(ts, name)
	if idx < 0 || idx >= len(val.elementsVal) {
		return false
	}
	var childTs *TypeSpec
	if ts != nil && idx < len(ts.Elements) {
		childTs = &ts.Elements[idx].Type
	}
	if len(remaining) == 0 {
		if childTs != nil && !childTs.ShallowCompatible(writeVal) {
			return false
		}
		val.elementsVal[idx] = writeVal.Clone()
		return true
	}
	return patchValue(val.elementsVal[idx], childTs, remaining, writeVal)
}

func patchIndex(val *Value, ts *TypeSpec, index int, remaining []pdu.AccessSelectorWire, writeVal *Value) bool {
	switch val.typ {
	case ValueTypeArray, ValueTypeStructure:
		if index < 0 || index >= len(val.elementsVal) {
			return false
		}
		var childTs *TypeSpec
		if ts != nil {
			if val.typ == ValueTypeStructure && index < len(ts.Elements) {
				childTs = &ts.Elements[index].Type
			} else if val.typ == ValueTypeArray {
				childTs = ts.Element
			}
		}
		if len(remaining) == 0 {
			if childTs != nil && !childTs.ShallowCompatible(writeVal) {
				return false
			}
			val.elementsVal[index] = writeVal.Clone()
			return true
		}
		return patchValue(val.elementsVal[index], childTs, remaining, writeVal)
	default:
		return false
	}
}

func patchRange(val *Value, start, count int, remaining []pdu.AccessSelectorWire, writeVal *Value) bool {
	if val.typ != ValueTypeArray {
		return false
	}
	end := start + count
	if start < 0 || end > len(val.elementsVal) {
		return false
	}
	if len(remaining) > 0 {
		return false
	}
	newElems, ok := writeVal.ArrayElements()
	if !ok || len(newElems) != count {
		return false
	}
	for i := 0; i < count; i++ {
		val.elementsVal[start+i] = newElems[i].Clone()
	}
	return true
}

func applySingleSelector(val *Value, ts *TypeSpec, sel pdu.AccessSelectorWire) (*Value, *TypeSpec) {
	switch {
	case sel.Component != "":
		return selectComponent(val, ts, sel.Component)
	case sel.HasIndex:
		return selectIndex(val, ts, sel.Index)
	case sel.IndexRange != nil:
		return selectRange(val, sel.IndexRange.LowIndex, sel.IndexRange.NumberOfElements), nil
	default:
		return nil, nil
	}
}

func selectComponent(val *Value, ts *TypeSpec, name string) (*Value, *TypeSpec) {
	if val.typ != ValueTypeStructure {
		return nil, nil
	}
	idx := resolveComponentIndex(ts, name)
	if idx < 0 || idx >= len(val.elementsVal) {
		return nil, nil
	}
	var childTs *TypeSpec
	if ts != nil && idx < len(ts.Elements) {
		childTs = &ts.Elements[idx].Type
	}
	return val.elementsVal[idx], childTs
}

// resolveComponentIndex maps a component name to an element index
// using the TypeSpec. Returns -1 if the name is not found or the
// TypeSpec is nil/not a structure.
func resolveComponentIndex(ts *TypeSpec, name string) int {
	if ts == nil || ts.Type != ValueTypeStructure {
		return -1
	}
	for i, elem := range ts.Elements {
		if elem.Name == name {
			return i
		}
	}
	return -1
}

func selectIndex(val *Value, ts *TypeSpec, index int) (*Value, *TypeSpec) {
	switch val.typ {
	case ValueTypeArray:
		elems := val.elementsVal
		if index < 0 || index >= len(elems) {
			return nil, nil
		}
		var childTs *TypeSpec
		if ts != nil {
			childTs = ts.Element
		}
		return elems[index], childTs
	case ValueTypeStructure:
		elems := val.elementsVal
		if index < 0 || index >= len(elems) {
			return nil, nil
		}
		var childTs *TypeSpec
		if ts != nil && index < len(ts.Elements) {
			childTs = &ts.Elements[index].Type
		}
		return elems[index], childTs
	default:
		return nil, nil
	}
}

func selectRange(val *Value, start, count int) *Value {
	if val.typ != ValueTypeArray {
		return nil
	}
	elems := val.elementsVal
	end := start + count
	if start < 0 || end > len(elems) {
		return nil
	}
	return NewArray(elems[start:end])
}
