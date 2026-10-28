//go:build interop

// SPDX-License-Identifier: MIT

package interop

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/otfabric/go-mms/internal/apisurface"
)

// peerStack is an MMS implementation packaged by mms-interop. Every stack
// implements the same adapter contract: commands <name>-mms-server and
// <name>-mms-client in image mms-interop-<name>, the same flags, fixture
// and JSON Lines, and a ready event whose "adapter" is <name>. The suite
// runs one test body per scenario against the stack selected by
// MMS_INTEROP_STACK.
type peerStack struct {
	name     string
	imageEnv string // per-stack image variable, e.g. LIBIEC61850_IMAGE

	// quirks are peer behaviours a test adapts its expectations to
	// instead of skipping, so the rest of the scenario still runs.
	quirks quirks

	// skips maps a test name to the reason the peer cannot run it. Only
	// peer limitations belong here (mirrored in mms-interop
	// fixtures/capabilities.json knownLimitations and INTEROP.md); a
	// go-mms bug is never skipped.
	skips map[string]string

	// iedOnly: the stack packages only IEC 61850 IED adapters
	// (<name>-ied-server and friends), not the MMS contract adapters. The
	// MMS contract suite skips; the IED server suite
	// (ied_server_test.go) runs against <name>-ied-server instead.
	iedOnly bool
}

// quirks records documented peer behaviours (capabilities.json
// knownLimitations) that change an expected value.
type quirks struct {
	// namedListReportsOnly: the server adapter sends only the fixture's
	// named-list InformationReports (libiec61850 mis-sizes listOfVariable
	// reports with domain-specific names).
	namedListReportsOnly bool
	// unseekedFileOpen: FileOpen honours initialPosition in the transfer
	// length but returns the bytes from offset 0.
	unseekedFileOpen bool
	// fileOpenNoLastModified: the client drops FileOpen's lastModified
	// and reports 0.
	fileOpenNoLastModified bool
	// noWriteNVL: the client cannot write a named variable list, so the
	// values it would set (true, 77) stay at those of write-multiple
	// (false, 0) for later reads and reports.
	noWriteNVL bool
	// noIdentify: the server cannot decode an Identify request and sends
	// no response; the association stays usable.
	noIdentify bool
	// noConclude: the server cannot decode a Conclude request and sends
	// no response, so Close ends at its context deadline.
	noConclude bool
	// disconnectOnUnknownName: a Read of a name the server does not have
	// ends the connection (COTP DR) instead of returning a data access
	// error.
	disconnectOnUnknownName bool
}

const defaultStack = "libiec61850"

var peerStacks = map[string]peerStack{
	"libiec61850": {
		name:     "libiec61850",
		imageEnv: "LIBIEC61850_IMAGE",
		quirks:   quirks{namedListReportsOnly: true, unseekedFileOpen: true},
	},
	"dscsystems": {
		name:     "dscsystems",
		imageEnv: "DSCSYSTEMS_IMAGE",
		quirks:   quirks{fileOpenNoLastModified: true, noWriteNVL: true},
		skips:    dscsystemsSkips,
	},
	"iec61850-rust": {
		name:     "iec61850-rust",
		imageEnv: "IEC61850RUST_IMAGE",
		skips:    iec61850RustSkips,
	},
	"iec61850bean": {
		name:     "iec61850bean",
		imageEnv: "IEC61850BEAN_IMAGE",
		quirks:   quirks{noIdentify: true, noConclude: true, disconnectOnUnknownName: true},
		iedOnly:  true,
	},
}

// stack returns the stack selected by MMS_INTEROP_STACK (default
// libiec61850).
func stack(t testing.TB) peerStack {
	t.Helper()
	name := getEnv("MMS_INTEROP_STACK", defaultStack)
	s, ok := peerStacks[name]
	if !ok {
		names := make([]string, 0, len(peerStacks))
		for n := range peerStacks {
			names = append(names, n)
		}
		sort.Strings(names)
		t.Fatalf("MMS_INTEROP_STACK=%q: unknown stack (one of %s)", name, strings.Join(names, ", "))
	}
	return s
}

// image returns the adapter image: MMS_INTEROP_IMAGE, else the stack's
// own variable, else the locally built mms-interop-<name>:local.
func (s peerStack) image() string {
	if v := os.Getenv("MMS_INTEROP_IMAGE"); v != "" {
		return v
	}
	return getEnv(s.imageEnv, "mms-interop-"+s.name+":local")
}

func (s peerStack) serverCommand() string    { return s.name + "-mms-server" }
func (s peerStack) iedServerCommand() string { return s.name + "-ied-server" }
func (s peerStack) clientCommand() string    { return s.name + "-mms-client" }

// skipForStack skips t when the selected stack cannot run it. Subtests
// inherit their parent's entry.
func skipForStack(t *testing.T) {
	t.Helper()
	s := stack(t)
	if s.iedOnly {
		t.Skipf("%s: no MMS contract adapter (IEC 61850 IED adapters only; see TestIEDServer_*)", s.name)
	}
	name := t.Name()
	for {
		if reason, ok := s.skips[name]; ok {
			t.Skipf("%s: %s", s.name, reason)
		}
		i := strings.LastIndexByte(name, '/')
		if i < 0 {
			return
		}
		name = name[:i]
	}
}

// skipUnsupported skips t when the client adapter reported that its stack
// has no call for the operation (contract: "error":"unsupported").
func skipUnsupported(t *testing.T, r clientResult) {
	t.Helper()
	if !r.OK && r.Error == "unsupported" {
		t.Skipf("%s client has no call for %s %q", stack(t).name, r.Operation, r.Target)
	}
}

// stackSkipTable renders the skip tables for INTEROP.md.
func stackSkipTable() string {
	var b strings.Builder
	names := make([]string, 0, len(peerStacks))
	for n := range peerStacks {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		tests := make([]string, 0, len(peerStacks[n].skips))
		for test := range peerStacks[n].skips {
			tests = append(tests, test)
		}
		sort.Strings(tests)
		for _, test := range tests {
			fmt.Fprintf(&b, "| %s | `%s` | %s |\n", n, test, peerStacks[n].skips[test])
		}
	}
	return b.String()
}

// dscsystemsSkips lists dscsystems/go-iec61850 limitations.
var dscsystemsSkips = map[string]string{
	"TestClient_ReadArrayRange":                               "server refuses index-range alternate access of more than one element (object-access-unsupported)",
	"TestClient_ReadVariables_WriteVariables_AlternateAccess": "server refuses index-range alternate access of more than one element (object-access-unsupported)",
}

// iec61850RustSkips lists csp0924/iec61850-rust limitations.
var iec61850RustSkips = map[string]string{}

func TestStackSkipsNameRealTests(t *testing.T) {
	tests, err := apisurface.TestFuncs(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range peerStacks {
		for name := range s.skips {
			base, _, _ := strings.Cut(name, "/")
			if !tests[base] {
				t.Errorf("%s skip names unknown test %s", s.name, name)
			}
		}
	}
	if testing.Verbose() {
		t.Logf("skip table:\n%s", stackSkipTable())
	}
}
