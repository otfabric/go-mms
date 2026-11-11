// SPDX-License-Identifier: MIT

package session

import "testing"

func FuzzSessionParse(f *testing.F) {
	// CONNECT SPDU
	f.Add(mustEncode(f)(EncodeConnect(ConnectParams{
		CallingSelector: []byte{0x00, 0x01},
		CalledSelector:  []byte{0x00, 0x01},
	}, []byte{0x31, 0x00})))

	// ACCEPT SPDU
	f.Add(mustEncode(f)(EncodeAccept(ConnectParams{
		CallingSelector: []byte{0x00, 0x01},
	}, []byte{0x31, 0x00})))

	// DATA SPDU
	f.Add(EncodeData([]byte{0x61, 0x00}))

	// FINISH SPDU
	f.Add(mustEncode(f)(EncodeFinish([]byte{0x61, 0x00})))

	// DISCONNECT SPDU
	f.Add(mustEncode(f)(EncodeDisconnect([]byte{0x61, 0x00})))

	// ABORT SPDU
	f.Add(mustEncode(f)(EncodeAbort([]byte{0x64, 0x00})))

	// REFUSE with user data, ABORT ACCEPT, extended user data CONNECT
	f.Add([]byte{0x0c, 0x08, 0x11, 0x01, 0x01, 0x32, 0x03, 0x02, 0x30, 0x00})
	f.Add([]byte{0x1a, 0x00})
	f.Add(mustEncode(f)(EncodeConnect(ConnectParams{}, make([]byte, 600))))

	f.Add([]byte{})
	f.Add([]byte{0xff})
	f.Add([]byte{0x01, 0x00, 0x01, 0x00}) // minimal DATA

	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = Parse(data)
	})
}
