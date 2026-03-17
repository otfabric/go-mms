package mms

import "testing"

func TestObjectClassString(t *testing.T) {
	tests := []struct {
		c    ObjectClass
		want string
	}{
		{ObjectClassNamedVariable, "NamedVariable"},
		{ObjectClassNamedVariableList, "NamedVariableList"},
		{ObjectClassDomain, "Domain"},
		{ObjectClass(99), "ObjectClass(99)"},
	}
	for _, tt := range tests {
		if got := tt.c.String(); got != tt.want {
			t.Errorf("ObjectClass(%d).String() = %q, want %q", int(tt.c), got, tt.want)
		}
	}
}

func TestValueTypeString(t *testing.T) {
	tests := []struct {
		vt   ValueType
		want string
	}{
		{ValueTypeBoolean, "Boolean"},
		{ValueTypeInteger, "Integer"},
		{ValueTypeFloat, "Float"},
		{ValueTypeStructure, "Structure"},
		{ValueTypeDataAccessError, "DataAccessError"},
		{ValueType(99), "ValueType(99)"},
	}
	for _, tt := range tests {
		if got := tt.vt.String(); got != tt.want {
			t.Errorf("ValueType(%d).String() = %q, want %q", int(tt.vt), got, tt.want)
		}
	}
}

func TestDataAccessErrorCodeString(t *testing.T) {
	tests := []struct {
		c    DataAccessErrorCode
		want string
	}{
		{DataAccessErrorNone, "None"},
		{DataAccessErrorObjectUndefined, "ObjectUndefined"},
		{DataAccessErrorCode(99), "DataAccessErrorCode(99)"},
	}
	for _, tt := range tests {
		if got := tt.c.String(); got != tt.want {
			t.Errorf("DataAccessErrorCode(%d).String() = %q, want %q", int(tt.c), got, tt.want)
		}
	}
}

func TestErrorClassString(t *testing.T) {
	tests := []struct {
		c    ErrorClass
		want string
	}{
		{ErrorClassVMDState, "VMDState"},
		{ErrorClassAccess, "Access"},
		{ErrorClass(99), "ErrorClass(99)"},
	}
	for _, tt := range tests {
		if got := tt.c.String(); got != tt.want {
			t.Errorf("ErrorClass(%d).String() = %q, want %q", int(tt.c), got, tt.want)
		}
	}
}

func TestVMDLogicalStatusString(t *testing.T) {
	if got := VMDLogicalStatusStateChangesAllowed.String(); got != "StateChangesAllowed" {
		t.Errorf("got %q, want StateChangesAllowed", got)
	}
}

func TestVMDPhysicalStatusString(t *testing.T) {
	if got := VMDPhysicalStatusOperational.String(); got != "Operational" {
		t.Errorf("got %q, want Operational", got)
	}
}
