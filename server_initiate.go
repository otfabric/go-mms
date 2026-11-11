// SPDX-License-Identifier: MIT

package mms

import (
	"encoding/asn1"

	"github.com/otfabric/go-mms/internal/acse"
	"github.com/otfabric/go-mms/internal/asn1util"
	"github.com/otfabric/go-mms/internal/serverconn"
)

// servicesSupportedCalled is the server's Initiate-Response
// servicesSupportedCalled (IEC 61850-8-1 8.1.3.4.1, Table 111): the
// services Server.dispatch implements, not the client's offer.
//
//   - status, identify, conclude, cancel: mandatory (m);
//   - getNameList, read, write, getVariableAccessAttributes,
//     defineNamedVariableList, getNamedVariableListAttributes,
//     deleteNamedVariableList: implemented by the server model;
//   - informationReport: sent by ServerConn.SendInformationReport;
//   - fileOpen/Read/Close/Rename/Delete/Directory and obtainFile: only
//     with a FileProvider;
//   - readJournal: only with a JournalProvider.
//
// unsolicitedStatus is not advertised (Table 111 "i"; ServerConn can
// still send it), nor any c10 service of MMS minor version 2+.
func servicesSupportedCalled(files, journals bool) asn1.BitString {
	services := []int{
		serverconn.ServiceStatus,
		asn1util.TagNumGetNameList,
		serverconn.ServiceIdentify,
		asn1util.TagNumRead,
		asn1util.TagNumWrite,
		asn1util.TagNumGetVariableAccessAttributes,
		asn1util.TagNumDefineNamedVariableList,
		asn1util.TagNumGetNamedVariableListAttrs,
		asn1util.TagNumDeleteNamedVariableList,
		serverconn.ServiceInformationReport,
		serverconn.ServiceConclude,
		serverconn.ServiceCancel,
	}
	if files {
		services = append(services,
			asn1util.TagNumObtainFile,
			asn1util.TagNumFileOpen,
			asn1util.TagNumFileRead,
			asn1util.TagNumFileClose,
			asn1util.TagNumFileRename,
			asn1util.TagNumFileDelete,
			asn1util.TagNumFileDirectory,
		)
	}
	if journals {
		services = append(services, asn1util.TagNumReadJournal)
	}
	return serverconn.ServiceBits(services...)
}

// authRejectDiagnostic is the AARE result-source-diagnostic
// (acse-service-user, ISO 8650-1) for an association the Authenticator
// refused: authentication-required (14) when the AARQ carried no
// authentication, authentication-mechanism-name-not-recognized (11)
// for an unknown mechanism, and authentication-failure (13) otherwise.
// TLS-only peers are reported as authentication-failure.
func authRejectDiagnostic(info acse.AuthInfo) int {
	switch info.Mechanism {
	case acse.AuthNone:
		return acse.DiagUserAuthRequired
	case acse.AuthUnknown:
		return acse.DiagUserAuthMechNameNotRecognized
	default:
		return acse.DiagUserAuthFailure
	}
}
