// SPDX-License-Identifier: MIT

package mms

import (
	"bytes"
	"encoding/asn1"
	"errors"
	"fmt"

	"github.com/otfabric/go-mms/internal/acse"
	"github.com/otfabric/go-mms/internal/codec"
	"github.com/otfabric/go-mms/internal/pdu"
	"github.com/otfabric/go-mms/internal/presentation"
	"github.com/otfabric/go-mms/internal/session"
)

// This file decodes captured MMS traffic passively, for trace analysis:
// a transport service data unit (the user data of the COTP data TPDUs of
// one TSDU, reassembled) through the session, presentation and ACSE layers
// to the MMS PDU. Nothing here sends or keeps state; the caller pairs
// requests and responses by invoke ID.

// ErrNotMMS is returned by [DecodeTSDU] for a unit that does not decode as
// the session layer of an MMS association.
var ErrNotMMS = errors.New("mms: not an MMS session unit")

// Captured is one decoded transport service data unit of an MMS
// association.
type Captured struct {
	// Session is the session SPDU: "connect", "accept", "refuse",
	// "finish", "disconnect", "abort" or "data".
	Session string
	// Presentation is the presentation PPDU: "CP", "CPA", "CPR",
	// "user-data", "ARU" or "ARP" (ISO 8823-1), or "" when the SPDU
	// carries none or one that does not decode.
	Presentation string
	// ACSE is set for an association control APDU (AARQ, AARE, RLRQ,
	// RLRE, ABRT).
	ACSE *CapturedACSE
	// PDU is the MMS PDU, when the unit carries one.
	PDU *CapturedPDU
}

// CapturedACSE is an association control APDU. Credentials are never
// decoded: only whether and how the association request authenticates.
type CapturedACSE struct {
	// Kind is "AARQ", "AARE", "RLRQ", "RLRE" or "ABRT".
	Kind string
	// Result of an AARE: 0 accepted, 1 rejected (permanent), 2 rejected
	// (transient); nil for other APDUs.
	Result *int
	// Authentication of an AARQ: "none", "password" or "other".
	Authentication string
}

// CapturedPDU is an MMS PDU. Kind and the invoke ID identify it; the
// service fields are set for the services this decoder models, and Raw
// holds the service content of the others.
type CapturedPDU struct {
	// Kind is one of the PDU kinds: "confirmed-request",
	// "confirmed-response", "confirmed-error", "unconfirmed", "reject",
	// "initiate-request", "initiate-response", "initiate-error",
	// "conclude-request", "conclude-response", "conclude-error",
	// "cancel-request", "cancel-response", "cancel-error".
	Kind        string
	InvokeID    uint32
	HasInvokeID bool
	// Service names the confirmed service as ISO 9506-2 does ("Read",
	// "Write", "GetNameList", "Kill", ...; "Service<n>" for a tag it does
	// not define) or "InformationReport" for an unconfirmed PDU.
	Service string

	// Variables are the variables a Read or Write request names, or an
	// InformationReport carries; ListName is the named variable list
	// they name instead.
	Variables []CapturedVariable
	ListName  *ObjectName
	// Values are a Read response's results, a Write request's data or an
	// InformationReport's access results, in order. A failed result is a
	// value of type ValueTypeDataAccessError.
	Values []*Value
	// WriteResults are a Write response's results, in order: nil for
	// success, the data access error otherwise.
	WriteResults []*DataAccessError

	NameListRequest  *CapturedNameListRequest
	NameList         []string
	NameListMore     bool
	Identify         *CapturedIdentify
	VariableName     *ObjectName // GetVariableAccessAttributes request
	TypeSpec         *TypeSpec   // GetVariableAccessAttributes response
	Initiate         *CapturedInitiate
	Error            *CapturedServiceError
	Reject           *CapturedReject
	NamedListMembers []CapturedVariable // GetNamedVariableListAttributes response

	// Raw is the service content of a service this decoder does not
	// model, and DecodeError why a modelled service did not decode. Raw
	// is a copy: it does not change when the decoded buffer does.
	Raw         []byte
	DecodeError string
}

// CapturedVariable is a variable a PDU names, with its alternate access.
type CapturedVariable struct {
	Name   ObjectName
	Access []AccessSelector
}

// CapturedNameListRequest is a GetNameList request.
type CapturedNameListRequest struct {
	ObjectClass   int
	Scope         ObjectScope
	Domain        string
	ContinueAfter string
}

// CapturedIdentify is an Identify response.
type CapturedIdentify struct {
	Vendor, Model, Revision string
}

// CapturedInitiate holds the parameters of an Initiate request or
// response.
type CapturedInitiate struct {
	LocalDetail                  int
	MaxServicesOutstandingCall   int
	MaxServicesOutstandingCalled int
	DataStructureNesting         int
	Version                      int
}

// CapturedServiceError is a confirmed (or initiate, conclude, cancel)
// error: the error class and code of ISO 9506-2.
type CapturedServiceError struct {
	Class, Code int
}

// CapturedReject is a Reject PDU: the rejected PDU type and the reason.
type CapturedReject struct {
	Type, Reason int
}

// confirmedServices names the ConfirmedServiceRequest and
// ConfirmedServiceResponse alternatives of ISO 9506-2 by their tag number,
// as the MMS ASN.1 module names them (with an upper-case first letter).
var confirmedServices = [...]string{
	"Status", "GetNameList", "Identify", "Rename", "Read", "Write", "GetVariableAccessAttributes",
	"DefineNamedVariable", "DefineScatteredAccess", "GetScatteredAccessAttributes", "DeleteVariableAccess",
	"DefineNamedVariableList", "GetNamedVariableListAttributes", "DeleteNamedVariableList", "DefineNamedType",
	"GetNamedTypeAttributes", "DeleteNamedType", "Input", "Output", "TakeControl", "RelinquishControl",
	"DefineSemaphore", "DeleteSemaphore", "ReportSemaphoreStatus", "ReportPoolSemaphoreStatus",
	"ReportSemaphoreEntryStatus", "InitiateDownloadSequence", "DownloadSegment", "TerminateDownloadSequence",
	"InitiateUploadSequence", "UploadSegment", "TerminateUploadSequence", "RequestDomainDownload",
	"RequestDomainUpload", "LoadDomainContent", "StoreDomainContent", "DeleteDomain", "GetDomainAttributes",
	"CreateProgramInvocation", "DeleteProgramInvocation", "Start", "Stop", "Resume", "Reset", "Kill",
	"GetProgramInvocationAttributes", "ObtainFile", "DefineEventCondition", "DeleteEventCondition",
	"GetEventConditionAttributes", "ReportEventConditionStatus", "AlterEventConditionMonitoring", "TriggerEvent",
	"DefineEventAction", "DeleteEventAction", "GetEventActionAttributes", "ReportEventActionStatus",
	"DefineEventEnrollment", "DeleteEventEnrollment", "AlterEventEnrollment", "ReportEventEnrollmentStatus",
	"GetEventEnrollmentAttributes", "AcknowledgeEventNotification", "GetAlarmSummary", "GetAlarmEnrollmentSummary",
	"ReadJournal", "WriteJournal", "InitializeJournal", "ReportJournalStatus", "CreateJournal", "DeleteJournal",
	"GetCapabilityList", "FileOpen", "FileRead", "FileClose", "FileRename", "FileDelete", "FileDirectory",
}

// serviceName is the name of a confirmed service tag, or "Service<n>" for
// a tag ISO 9506-2 does not define (companion standards, extensions).
func serviceName(tag int) string {
	if tag >= 0 && tag < len(confirmedServices) {
		return confirmedServices[tag]
	}
	return fmt.Sprintf("Service%d", tag)
}

var sessionKinds = map[session.SpduType]string{
	session.SpduConnect: "connect", session.SpduAccept: "accept", session.SpduRefuse: "refuse",
	session.SpduFinish: "finish", session.SpduDisconnect: "disconnect", session.SpduAbort: "abort",
	session.SpduData: "data",
}

// DecodeTSDU decodes a transport service data unit of an MMS association.
// It fails with an error wrapping [ErrNotMMS] when the unit is not a
// session SPDU, and returns what it decoded with the error of the first
// layer above that does not decode.
func DecodeTSDU(b []byte) (*Captured, error) {
	spdu, err := session.Parse(b)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNotMMS, err)
	}
	out := &Captured{Session: sessionKinds[spdu.Type]}
	if len(spdu.UserData) == 0 {
		return out, nil
	}
	parse := presentation.Parse
	if spdu.Type == session.SpduAbort {
		// ARU-PPDU or ARP-PPDU (ISO 8823-1).
		parse = presentation.ParseAbort
	}
	ppdu, err := parse(spdu.UserData)
	if err != nil {
		if spdu.Type == session.SpduAbort {
			return out, nil // an abort PPDU that does not decode is not modelled
		}
		return out, fmt.Errorf("mms: presentation: %w", err)
	}
	out.Presentation = map[presentation.PpduKind]string{
		presentation.PpduCP: "CP", presentation.PpduCPA: "CPA", presentation.PpduCPR: "CPR",
		presentation.PpduUserData: "user-data", presentation.PpduARU: "ARU", presentation.PpduARP: "ARP",
	}[ppdu.Kind]
	if len(ppdu.UserData) == 0 {
		return out, nil
	}
	if ppdu.Kind == presentation.PpduUserData && ppdu.ContextID == presentation.ContextIDMMS {
		out.PDU, err = DecodePDU(ppdu.UserData)
		return out, err
	}
	apdu, err := acse.Parse(ppdu.UserData)
	if err != nil {
		return out, fmt.Errorf("mms: acse: %w", err)
	}
	out.ACSE = capturedACSE(apdu)
	// The MMS Initiate request of an AARQ, or response of an AARE.
	if len(apdu.UserData) > 0 {
		out.PDU, err = DecodePDU(apdu.UserData)
	}
	return out, err
}

func capturedACSE(a *acse.ParsedAPDU) *CapturedACSE {
	out := &CapturedACSE{Kind: map[acse.ApduType]string{
		acse.ApduAARQ: "AARQ", acse.ApduAARE: "AARE", acse.ApduRLRQ: "RLRQ", acse.ApduRLRE: "RLRE", acse.ApduABRT: "ABRT",
	}[a.Type]}
	switch a.Type {
	case acse.ApduAARE:
		if a.AARE != nil {
			r := a.AARE.Result
			out.Result = &r
		}
	case acse.ApduAARQ:
		out.Authentication = map[acse.AuthMechanism]string{
			acse.AuthNone: "none", acse.AuthPassword: "password", acse.AuthUnknown: "other",
		}[a.Auth.Mechanism]
	case acse.ApduRLRQ, acse.ApduRLRE, acse.ApduABRT:
		// The kind is what they say.
	}
	return out
}

var pduKinds = map[pdu.PduKind]string{
	pdu.PduConfirmedRequest: "confirmed-request", pdu.PduConfirmedResponse: "confirmed-response",
	pdu.PduConfirmedError: "confirmed-error", pdu.PduUnconfirmed: "unconfirmed", pdu.PduReject: "reject",
	pdu.PduInitiateRequest: "initiate-request", pdu.PduInitiateResponse: "initiate-response",
	pdu.PduInitiateError: "initiate-error", pdu.PduConcludeRequest: "conclude-request",
	pdu.PduConcludeResponse: "conclude-response", pdu.PduConcludeError: "conclude-error",
	pdu.PduCancelRequest: "cancel-request", pdu.PduCancelResponse: "cancel-response",
	pdu.PduCancelError: "cancel-error",
}

// DecodePDU decodes an MMS PDU. A service it models but cannot decode
// sets DecodeError and keeps the content in Raw; only a PDU whose outer
// structure does not decode is an error.
func DecodePDU(b []byte) (*CapturedPDU, error) {
	kind, content, err := pdu.DecodePdu(b)
	if err != nil {
		return nil, fmt.Errorf("mms: %w", err)
	}
	out := &CapturedPDU{Kind: pduKinds[kind]}
	switch kind {
	case pdu.PduConfirmedRequest:
		id, svc, err := codec.UnmarshalConfirmedRequest(content)
		if err != nil {
			return out.envelopeFailed(content, "confirmed request", err)
		}
		out.InvokeID, out.HasInvokeID = id, true
		out.Service = serviceName(svc.Tag)
		out.decodeRequest(svc)
	case pdu.PduConfirmedResponse:
		resp, err := pdu.DecodeConfirmedResponse(content)
		if err != nil {
			return out.envelopeFailed(content, "confirmed response", err)
		}
		out.InvokeID, out.HasInvokeID = resp.InvokeID, true
		out.Service = serviceName(resp.ServiceData.Tag)
		out.decodeResponse(resp.ServiceKind, resp.ServiceData)
	case pdu.PduConfirmedError:
		e, err := pdu.DecodeConfirmedError(content)
		if err != nil {
			return out.envelopeFailed(content, "confirmed error", err)
		}
		out.InvokeID, out.HasInvokeID = e.InvokeID, true
		out.Error = &CapturedServiceError{Class: e.ErrorClass, Code: e.ErrorCode}
	case pdu.PduReject:
		r, err := pdu.DecodeRejectPDU(content)
		if err != nil {
			return nil, fmt.Errorf("mms: reject: %w", err)
		}
		out.InvokeID, out.HasInvokeID = r.InvokeID, r.HasInvokeID
		out.Reject = &CapturedReject{Type: r.RejectType, Reason: r.RejectReason}
	case pdu.PduConcludeError:
		// Conclude-ErrorPDU ::= ServiceError (ISO 9506-2 clause 8.4.3).
		se, err := pdu.DecodeConcludeError(content)
		if err != nil {
			out.fail(content, err)
			break
		}
		out.Error = &CapturedServiceError{Class: se.ErrorClass, Code: se.ErrorCode}
	case pdu.PduUnconfirmed:
		out.Service = "InformationReport"
		out.decodeReport(content)
	case pdu.PduInitiateRequest:
		r, err := pdu.UnmarshalInitiateRequest(content)
		if err != nil {
			out.fail(content, err)
			break
		}
		out.Initiate = &CapturedInitiate{LocalDetail: r.LocalDetailCalling, MaxServicesOutstandingCall: r.ProposedMaxServOutstandingCall,
			MaxServicesOutstandingCalled: r.ProposedMaxServOutstandingCalled, DataStructureNesting: r.ProposedDataStructureNesting,
			Version: r.InitRequestDetail.ProposedVersion}
	case pdu.PduInitiateResponse:
		r, err := pdu.UnmarshalInitiateResponse(content)
		if err != nil {
			out.fail(content, err)
			break
		}
		out.Initiate = &CapturedInitiate{LocalDetail: r.LocalDetailCalled, MaxServicesOutstandingCall: r.NegotiatedMaxServOutstandingCall,
			MaxServicesOutstandingCalled: r.NegotiatedMaxServOutstandingCalled, DataStructureNesting: r.NegotiatedDataStructureNesting,
			Version: r.InitResponseDetail.NegotiatedVersion}
	default:
		// Initiate and conclude errors, conclude and cancel PDUs: the kind
		// (and a cancel's invoke ID, its whole content) is what they say.
		out.Raw = bytes.Clone(content)
	}
	return out, nil
}

// envelopeFailed handles a confirmed PDU whose envelope does not decode:
// when its invoke ID still does (a response without a service, as some
// stacks send), the PDU is returned with the invoke ID, its content in Raw
// and DecodeError, so that a trace can still pair it; otherwise it is an
// error.
func (p *CapturedPDU) envelopeFailed(content []byte, what string, err error) (*CapturedPDU, error) {
	id, idErr := pdu.ExtractInvokeID(content)
	if idErr != nil {
		return nil, fmt.Errorf("mms: %s: %w", what, err)
	}
	p.InvokeID, p.HasInvokeID = id, true
	p.fail(content, err)
	return p, nil
}

func (p *CapturedPDU) fail(content []byte, err error) {
	p.Raw, p.DecodeError = bytes.Clone(content), err.Error()
}

func (p *CapturedPDU) decodeRequest(svc asn1.RawValue) {
	var err error
	switch pdu.ClassifyServiceTag(svc.Tag) {
	case pdu.ServiceRead:
		var r *pdu.ReadRequestWire
		if r, err = pdu.UnmarshalReadRequestParsed(svc.Bytes); err == nil {
			err = p.setVariables(r.Variables, r.ListName)
		}
	case pdu.ServiceWrite:
		var w *pdu.WriteRequestWire
		if w, err = pdu.UnmarshalWriteRequestParsed(svc.Bytes); err == nil {
			if err = p.setVariables(w.Variables, w.ListName); err == nil {
				p.Values, err = dataValuesToValues(w.Values)
			}
		}
	case pdu.ServiceGetNameList:
		var r *pdu.GetNameListRequest
		if r, err = pdu.UnmarshalGetNameListRequest(svc.Bytes); err == nil {
			var scope ObjectScope
			if scope, err = objectScopeFromWire(r.Scope); err == nil {
				p.NameListRequest = &CapturedNameListRequest{ObjectClass: r.ObjectClass, Scope: scope, Domain: r.DomainID, ContinueAfter: r.ContinueAfter}
			}
		}
	case pdu.ServiceGetVariableAccessAttrs:
		var n pdu.ObjectNameWire
		if n, err = pdu.UnmarshalGetVarAccessRequest(svc.Bytes); err == nil {
			var name ObjectName
			if name, err = objectNameFromWire(n); err == nil {
				p.VariableName = &name
			}
		}
	case pdu.ServiceGetNamedVariableListAttrs:
		var r *pdu.GetNVLAttrsRequest
		if r, err = pdu.UnmarshalGetNVLAttrsRequest(svc.Bytes); err == nil {
			var name ObjectName
			if name, err = objectNameFromWire(r.ListName); err == nil {
				p.ListName = &name
			}
		}
	case pdu.ServiceIdentify:
	default:
		p.Raw = bytes.Clone(svc.Bytes)
	}
	if err != nil {
		p.fail(svc.Bytes, err)
	}
}

func (p *CapturedPDU) decodeResponse(kind pdu.ServiceKind, svc asn1.RawValue) {
	var err error
	switch kind {
	case pdu.ServiceRead:
		var dvs []*pdu.DataValue
		if dvs, err = pdu.UnmarshalReadResponse(svc); err == nil {
			p.Values, err = dataValuesToValues(dvs)
		}
	case pdu.ServiceWrite:
		var items []pdu.WriteResultItem
		if items, err = pdu.UnmarshalWriteResponse(svc); err == nil {
			p.WriteResults = make([]*DataAccessError, len(items))
			for i, it := range items {
				if !it.Success {
					code, cerr := decodeDataAccessError(it.ErrCode)
					if cerr != nil {
						err = cerr
						break
					}
					p.WriteResults[i] = &DataAccessError{Code: code}
				}
			}
		}
	case pdu.ServiceGetNameList:
		var r *pdu.GetNameListResult
		if r, err = pdu.UnmarshalGetNameListResponse(svc); err == nil {
			p.NameList, p.NameListMore = r.Names, r.MoreFollows
		}
	case pdu.ServiceIdentify:
		var r *pdu.IdentifyResponse
		if r, err = pdu.UnmarshalIdentifyResponse(svc); err == nil {
			p.Identify = &CapturedIdentify{Vendor: r.VendorName, Model: r.ModelName, Revision: r.Revision}
		}
	case pdu.ServiceGetVariableAccessAttrs:
		var r *pdu.GetVarAccessResult
		if r, err = pdu.UnmarshalGetVarAccessResponse(svc); err == nil {
			var ts TypeSpec
			if ts, err = typeSpecFromWire(r.TypeSpec); err == nil {
				p.TypeSpec = &ts
			}
		}
	case pdu.ServiceGetNamedVariableListAttrs:
		var r *pdu.NamedVarListAttrsResult
		if r, err = pdu.UnmarshalGetNamedVarListAttrsResponse(svc); err == nil {
			p.NamedListMembers, err = capturedVariables(r.Variables)
		}
	default:
		p.Raw = bytes.Clone(svc.Bytes)
	}
	if err != nil {
		p.fail(svc.Bytes, err)
	}
}

func (p *CapturedPDU) decodeReport(content []byte) {
	r, err := pdu.UnmarshalInformationReport(content)
	if err == nil {
		var vars []pdu.VariableSpecWire
		for _, v := range r.Variables {
			vars = append(vars, pdu.VariableSpecWire{Name: v})
		}
		if err = p.setVariables(vars, r.ListName); err == nil {
			p.Values, err = dataValuesToValues(r.Values)
		}
	}
	if err != nil {
		p.fail(content, err)
	}
}

func (p *CapturedPDU) setVariables(vars []pdu.VariableSpecWire, list *pdu.ObjectNameWire) error {
	if list != nil {
		name, err := objectNameFromWire(*list)
		if err != nil {
			return err
		}
		p.ListName = &name
		return nil
	}
	var err error
	p.Variables, err = capturedVariables(vars)
	return err
}

func capturedVariables(vars []pdu.VariableSpecWire) ([]CapturedVariable, error) {
	out := make([]CapturedVariable, 0, len(vars))
	for _, v := range vars {
		name, err := objectNameFromWire(v.Name)
		if err != nil {
			return nil, err
		}
		cv := CapturedVariable{Name: name}
		for _, s := range v.AlternateAccess {
			cv.Access = append(cv.Access, accessSelectorFromWire(s))
		}
		out = append(out, cv)
	}
	return out, nil
}

func accessSelectorFromWire(s pdu.AccessSelectorWire) AccessSelector {
	switch {
	case s.Component != "":
		return AccessSelector{Component: s.Component}
	case s.IndexRange != nil:
		return AccessSelector{IndexRange: &IndexRange{Start: s.IndexRange.LowIndex, Count: s.IndexRange.NumberOfElements}}
	default:
		i := s.Index
		return AccessSelector{Index: &i}
	}
}
