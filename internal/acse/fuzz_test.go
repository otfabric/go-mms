// SPDX-License-Identifier: MIT

package acse

import "testing"

func FuzzACSEParse(f *testing.F) {
	// AARQ with MMS payload
	aarq, _ := EncodeAARQ(AARQParams{}, []byte{0xa8, 0x00})
	f.Add(aarq)

	// AARQ with password auth
	aarqAuth, _ := EncodeAARQ(AARQParams{
		Password: []byte("secret"),
	}, []byte{0xa8, 0x00})
	f.Add(aarqAuth)

	// AARE accepted
	f.Add(EncodeAARE(ResultAccepted, []byte{0xa9, 0x00}))

	// AARE rejected
	f.Add(EncodeAARE(ResultRejectedPerm, nil))

	// RLRQ
	f.Add(EncodeRLRQ())

	// RLRE
	f.Add(EncodeRLRE())

	// ABRT user
	f.Add(EncodeABRT(0))

	// ABRT provider
	f.Add(EncodeABRT(1))

	f.Add([]byte{})
	f.Add([]byte{0xff})
	f.Add([]byte{0x60, 0x00}) // minimal AARQ

	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = Parse(data)
	})
}
