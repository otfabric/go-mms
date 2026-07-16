//go:build interop

// SPDX-License-Identifier: MIT

package interop

import (
	"context"
	"os"
	"testing"
	"time"

	mms "github.com/otfabric/go-mms"
	"github.com/otfabric/go-mms/transport/iso"
)

func serverAddr() string {
	if addr := os.Getenv("MMS_INTEROP_ADDR"); addr != "" {
		return addr
	}
	return "localhost:102"
}

func dialOrSkip(t *testing.T, ctx context.Context) *mms.Client {
	t.Helper()
	client, err := iso.Dial(ctx, serverAddr())
	if err != nil {
		t.Skipf("cannot reach C server at %s: %v", serverAddr(), err)
	}
	return client
}

func TestInteropIdentify(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client := dialOrSkip(t, ctx)
	defer client.Close(ctx)

	id, err := client.Identify(ctx)
	if err != nil {
		t.Fatalf("Identify: %v", err)
	}
	t.Logf("Identify: vendor=%q, model=%q, revision=%q", id.Vendor, id.Model, id.Revision)

	if id.Vendor == "" {
		t.Error("expected non-empty vendor name")
	}
}

func TestInteropStatus(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client := dialOrSkip(t, ctx)
	defer client.Close(ctx)

	status, err := client.Status(ctx)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	t.Logf("Status: logical=%d, physical=%d", status.Logical, status.Physical)
}

func TestInteropGetNameList(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client := dialOrSkip(t, ctx)
	defer client.Close(ctx)

	result, err := client.GetNameList(ctx, mms.NameListRequest{
		ObjectClass: mms.ObjectClassDomain,
		Scope:       mms.ObjectScopeVMD,
	})
	if err != nil {
		t.Fatalf("GetNameList (domains): %v", err)
	}
	t.Logf("Domains: %d names", len(result.Names))
	for _, n := range result.Names {
		t.Logf("  %s", n)
	}

	if len(result.Names) == 0 {
		t.Skip("no domains reported; cannot enumerate domain variables")
	}

	domain := mms.DomainID(result.Names[0])

	varResult, err := client.GetNameList(ctx, mms.NameListRequest{
		ObjectClass: mms.ObjectClassNamedVariable,
		Scope:       mms.ObjectScopeDomain,
		DomainID:    domain,
	})
	if err != nil {
		t.Fatalf("GetNameList (variables in %s): %v", domain, err)
	}
	t.Logf("Variables in %s: %d names", domain, len(varResult.Names))
	for _, n := range varResult.Names {
		t.Logf("  %s/%s", domain, n)
	}
}

func TestInteropReadVariable(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client := dialOrSkip(t, ctx)
	defer client.Close(ctx)

	domainResult, err := client.GetNameList(ctx, mms.NameListRequest{
		ObjectClass: mms.ObjectClassDomain,
		Scope:       mms.ObjectScopeVMD,
	})
	if err != nil || len(domainResult.Names) == 0 {
		t.Skip("no domains available to read variables from")
	}

	domain := mms.DomainID(domainResult.Names[0])

	varResult, err := client.GetNameList(ctx, mms.NameListRequest{
		ObjectClass: mms.ObjectClassNamedVariable,
		Scope:       mms.ObjectScopeDomain,
		DomainID:    domain,
	})
	if err != nil || len(varResult.Names) == 0 {
		t.Skip("no variables available to read")
	}

	name := varResult.Names[0]

	rr, err := client.Read(ctx, mms.ReadRequest{
		DomainID: domain,
		ItemID:   mms.ItemID(name),
	})
	if err != nil {
		t.Fatalf("Read %s/%s: %v", domain, name, err)
	}
	t.Logf("Read %s/%s: type=%s value=%s", domain, name, rr.Value.Type(), rr.Value)
}

func TestInteropGetVariableAccessAttributes(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client := dialOrSkip(t, ctx)
	defer client.Close(ctx)

	domainResult, err := client.GetNameList(ctx, mms.NameListRequest{
		ObjectClass: mms.ObjectClassDomain,
		Scope:       mms.ObjectScopeVMD,
	})
	if err != nil || len(domainResult.Names) == 0 {
		t.Skip("no domains available")
	}

	domain := mms.DomainID(domainResult.Names[0])

	varResult, err := client.GetNameList(ctx, mms.NameListRequest{
		ObjectClass: mms.ObjectClassNamedVariable,
		Scope:       mms.ObjectScopeDomain,
		DomainID:    domain,
	})
	if err != nil || len(varResult.Names) == 0 {
		t.Skip("no variables available")
	}

	name := varResult.Names[0]

	attrs, err := client.GetVariableAccessAttributes(ctx, mms.ObjectName{
		Scope:  mms.ObjectScopeDomain,
		Domain: domain,
		ItemID: mms.ItemID(name),
	})
	if err != nil {
		t.Fatalf("GetVariableAccessAttributes %s/%s: %v", domain, name, err)
	}
	t.Logf("GetVariableAccessAttributes %s/%s: deletable=%v type=%s",
		domain, name, attrs.Deletable, attrs.TypeSpec.Type)
}
