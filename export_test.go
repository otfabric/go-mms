package mms

import "github.com/otfabric/go-mms/internal/acse"

// BuildAuthContextForTest exposes buildAuthContext for white-box testing
// of mechanism classification logic.
func (s *Server) BuildAuthContextForTest(conn Transport, ai acse.AuthInfo) *AuthContext {
	return s.buildAuthContext(conn, ai)
}
