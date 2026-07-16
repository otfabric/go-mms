// SPDX-License-Identifier: MIT

package mms

import (
	"context"
	"crypto/x509"
	"encoding/asn1"
	"net"
)

// AuthMechanism identifies the authentication mechanism used during
// association establishment.
//
// The zero value is [AuthMechanismUnknown]. It indicates that an
// authentication mechanism was presented but could not be classified.
// Absence of authentication is represented by [AuthMechanismNone].
type AuthMechanism int

const (
	// AuthMechanismUnknown indicates the peer presented an ACSE
	// authentication mechanism that this library does not recognize.
	// The raw OID is available in [AuthContext].MechanismOID.
	AuthMechanismUnknown AuthMechanism = iota

	// AuthMechanismNone indicates no authentication was provided —
	// no ACSE auth fields and no TLS peer certificate.
	AuthMechanismNone

	// AuthMechanismACSEPassword indicates ACSE password authentication
	// (mechanism OID 2.2.3.1). The password is available in
	// [AuthContext].Password.
	//
	// SECURITY: ACSE password auth transmits credentials inside the
	// AARQ. Without TLS, the password travels in the clear. Password
	// auth should normally be combined with TLS transport security.
	AuthMechanismACSEPassword

	// AuthMechanismTLSCertificate indicates TLS-level peer certificate
	// authentication. This mechanism is inferred when no ACSE
	// authentication is present but the TLS transport provides peer
	// certificates. The leaf certificate is available in
	// [AuthContext].PeerCertificate.
	AuthMechanismTLSCertificate
)

func (m AuthMechanism) String() string {
	switch m {
	case AuthMechanismUnknown:
		return "Unknown"
	case AuthMechanismNone:
		return "None"
	case AuthMechanismACSEPassword:
		return "ACSEPassword"
	case AuthMechanismTLSCertificate:
		return "TLSCertificate"
	default:
		return "AuthMechanism(?)"
	}
}

// ApplicationReference identifies a calling or called application by
// its AP-title (Application Process title) and optional AE-qualifier
// (Application Entity qualifier).
//
// In MMS/IEC 61850, these are carried in the ACSE AARQ and are the
// primary identity for association-level access control decisions.
type ApplicationReference struct {
	// APTitle is the application process title as an OID.
	APTitle asn1.ObjectIdentifier

	// AEQualifier is the application entity qualifier. Nil when absent.
	AEQualifier *int
}

// AuthContext holds all authentication-relevant information for an
// incoming association. It is passed to the [Authenticator] callback
// during association establishment.
//
// Fields are populated from the ACSE AARQ and, where available, from
// TLS transport state. Absent optional fields are nil/zero.
type AuthContext struct {
	// Mechanism is the normalized authentication mechanism.
	//
	// [AuthMechanismTLSCertificate] is transport-derived: the server
	// infers it when no ACSE mechanism is present but the TLS transport
	// provides a peer certificate. It does not correspond to an ACSE
	// mechanism-name OID; [MechanismOID] will be nil in that case.
	Mechanism AuthMechanism

	// MechanismOID is the ACSE mechanism OID from the AARQ, decoded
	// from BER into a structured OID. Nil when no mechanism was
	// presented. For password auth this is {2, 2, 3, 1}.
	MechanismOID asn1.ObjectIdentifier

	// Password is the ACSE password bytes, populated when Mechanism
	// is [AuthMechanismACSEPassword]. This is a defensive copy; the
	// caller's original slice is not shared.
	Password []byte

	// CallingApplication is the calling AP-title and AE-qualifier
	// from the AARQ. Nil when not present. This is the primary
	// association identity for access control alongside the mechanism.
	CallingApplication *ApplicationReference

	// PeerCertificate is the leaf TLS peer certificate, populated when
	// the transport is TLS-secured and the peer presented a certificate.
	// Nil when TLS is not active or no peer certificate was provided.
	PeerCertificate *x509.Certificate

	// RemoteAddr is the network address of the remote peer. Nil when
	// the transport does not expose address information.
	RemoteAddr net.Addr
}

// HasTLSCertificate reports whether a TLS peer certificate is available.
func (a *AuthContext) HasTLSCertificate() bool {
	return a.PeerCertificate != nil
}

// HasCallingApplication reports whether the AARQ contained a calling
// AP-title / AE-qualifier.
func (a *AuthContext) HasCallingApplication() bool {
	return a.CallingApplication != nil
}

// AuthResult is the outcome of an [Authenticator] callback.
type AuthResult struct {
	// Accept indicates whether the association should be accepted.
	Accept bool

	// Token is an opaque application security token or principal that
	// the authenticator attaches to the association. It is stored on
	// the [ServerConn] and can be retrieved by upper layers (e.g.
	// go-iec61850) via [ServerConn.AuthToken].
	Token any
}

// Authenticator is a server callback invoked during association
// establishment to decide whether to accept or reject the peer.
//
// It receives a [context.Context] (with the Serve deadline if any) and
// the [AuthContext] describing the association's authentication material.
//
// Return an [AuthResult] with Accept=true to accept, or Accept=false to
// reject. The error return is reserved for internal failures (e.g.
// backend lookup errors); returning a non-nil error also rejects the
// association.
//
// If not set on [ServerOptions], all associations are accepted with a
// nil token.
//
// Example authenticator for ACSE password policy:
//
//	func myAuth(_ context.Context, auth *mms.AuthContext) (mms.AuthResult, error) {
//	    if auth.Mechanism != mms.AuthMechanismACSEPassword {
//	        return mms.AuthResult{}, nil // reject: wrong mechanism
//	    }
//	    if !bytes.Equal(auth.Password, []byte("secret")) {
//	        return mms.AuthResult{}, nil // reject: wrong password
//	    }
//	    return mms.AuthResult{Accept: true, Token: "operator"}, nil
//	}
//
// Example authenticator for TLS client certificate policy:
//
//	func tlsAuth(_ context.Context, auth *mms.AuthContext) (mms.AuthResult, error) {
//	    if auth.PeerCertificate == nil {
//	        return mms.AuthResult{}, nil // reject: no peer cert
//	    }
//	    cn := auth.PeerCertificate.Subject.CommonName
//	    if !isAllowedOperator(cn) {
//	        return mms.AuthResult{}, nil
//	    }
//	    return mms.AuthResult{Accept: true, Token: cn}, nil
//	}
//
// Example authenticator for AP-title policy:
//
//	func apTitleAuth(_ context.Context, auth *mms.AuthContext) (mms.AuthResult, error) {
//	    if auth.CallingApplication == nil {
//	        return mms.AuthResult{}, nil
//	    }
//	    if !auth.CallingApplication.APTitle.Equal(allowedTitle) {
//	        return mms.AuthResult{}, nil
//	    }
//	    return mms.AuthResult{Accept: true}, nil
//	}
type Authenticator func(ctx context.Context, auth *AuthContext) (AuthResult, error)
