// SPDX-License-Identifier: MIT

package e2e

import (
	"context"
	"errors"
	"testing"

	"github.com/otfabric/go-mms"
)

// TestIdentifier_PeerNamesOutsideCharset: ISO 9506-2 clause 7.5.2
// restricts an Identifier to A-Z a-z 0-9 $ _, but peers serve names
// outside it (libiec61850 serves the iec61850-interop fixture's
// "visible-string"). The server registers and the client reads such a
// name; ValidateIdentifier is the strict check for names you define.
func TestIdentifier_PeerNamesOutsideCharset(t *testing.T) {
	if err := mms.ValidateIdentifier("visible-string"); !errors.Is(err, mms.ErrInvalidArgument) {
		t.Fatalf("ValidateIdentifier(visible-string) = %v, want ErrInvalidArgument", err)
	}
	if err := mms.ValidateIdentifier("LLN0$ST$Mod"); err != nil {
		t.Fatalf("ValidateIdentifier(LLN0$ST$Mod) = %v", err)
	}
	srv := mms.NewServer(mms.ServerOptions{})
	if err := srv.RegisterDomain("interop"); err != nil {
		t.Fatal(err)
	}
	name := mms.ObjectName{Scope: mms.ObjectScopeDomain, Domain: "interop", ItemID: "visible-string"}
	val := mms.NewVisibleString("hello")
	if err := srv.RegisterVariable(mms.Variable{
		Name:     name,
		TypeSpec: mms.TypeSpec{Type: mms.ValueTypeVisibleString, Size: 32},
		Read:     func(context.Context) (*mms.Value, error) { return val, nil },
	}); err != nil {
		t.Fatalf("RegisterVariable(visible-string): %v", err)
	}
	c := dial(t, serve(t, srv))
	got, err := c.Read(timeout(t), mms.ReadRequest{DomainID: "interop", ItemID: "visible-string"})
	if err != nil {
		t.Fatalf("Read(visible-string): %v", err)
	}
	mustEqual(t, "visible-string", got.Value, val)
}
