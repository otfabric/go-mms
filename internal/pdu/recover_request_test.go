// SPDX-License-Identifier: MIT

package pdu

import "testing"

// RecoverRequestInvokeID reads the invoke ID of a Confirmed-Request from
// its first inner element, whatever follows it, and nothing from another
// PDU kind.
func TestRecoverRequestInvokeID(t *testing.T) {
	for in, want := range map[string]uint32{
		"a003 020107":        7,
		"a0820100 020105":    5, // the outer length says 256 octets; they are not read
		"a080 020103":        3, // indefinite outer length
		"a006 0204 7fffffff": 0x7fffffff,
	} {
		id, ok := RecoverRequestInvokeID(hx(t, in))
		if !ok || id != want {
			t.Errorf("RecoverRequestInvokeID(%s) = %d, %v; want %d", in, id, ok, want)
		}
	}
	for _, in := range []string{
		"",
		"a0",
		"a120 020107",          // Confirmed-Response
		"a003 800107",          // not an INTEGER
		"a085",                 // length octets beyond the PDU
		"a003 0201",            // truncated invoke ID
		"a007 0205 0080000000", // outside Unsigned32
	} {
		if _, ok := RecoverRequestInvokeID(hx(t, in)); ok {
			t.Errorf("RecoverRequestInvokeID(%s) recovered an ID", in)
		}
	}
}
