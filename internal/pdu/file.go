// SPDX-License-Identifier: MIT

package pdu

import (
	"fmt"
	"math"
	"time"

	"github.com/otfabric/go-mms/internal/asn1util"
	"github.com/otfabric/go-mms/internal/berutil"
	"github.com/otfabric/go-mms/internal/codec"
)

// Defensive limits for file-related decoders.
const (
	maxFileNameLength   = 1024
	maxDirectoryEntries = 10000
)

// FileDirectoryEntry is a single entry in a FileDirectory response.
type FileDirectoryEntry struct {
	FileName     string
	Size         int64
	LastModified time.Time
}

// FileOpenRequest holds the parsed FileOpen request fields.
type FileOpenRequest struct {
	FileName        string
	InitialPosition uint32
}

// FileDirectoryRequest holds the parsed FileDirectory request fields.
type FileDirectoryRequest struct {
	FileSpec      string
	ContinueAfter string
}

// --- Client-side request marshalers ---

// MarshalFileOpenRequest builds a ConfirmedRequestPdu for FileOpen.
// The current public client API always opens at position 0;
// seek/open-at-offset support may be added in a future phase.
//
//	FileOpenRequest ::= [72] IMPLICIT SEQUENCE {
//	    fileName   [0] IMPLICIT FileName,
//	    initialPosition [1] IMPLICIT Unsigned32
//	}
func MarshalFileOpenRequest(invokeID codec.InvokeID, fileName string, initialPosition uint32) ([]byte, error) {
	nameBytes := encodeFileName(fileName)
	posBytes := berutil.EncodeTLV(0x81, berutil.EncodeUint32(initialPosition))

	payload := make([]byte, 0, len(nameBytes)+len(posBytes))
	payload = append(payload, nameBytes...)
	payload = append(payload, posBytes...)

	return MarshalConfirmedRequest(invokeID, asn1util.TagNumFileOpen, true, payload)
}

// MarshalFileReadRequest builds a ConfirmedRequestPdu for FileRead.
// The request body is just the frsmId as an integer.
func MarshalFileReadRequest(invokeID codec.InvokeID, frsmID int32) ([]byte, error) {
	payload := berutil.EncodeInt(int(frsmID))
	return MarshalConfirmedRequest(invokeID, asn1util.TagNumFileRead, false, payload)
}

// MarshalFileCloseRequest builds a ConfirmedRequestPdu for FileClose.
func MarshalFileCloseRequest(invokeID codec.InvokeID, frsmID int32) ([]byte, error) {
	payload := berutil.EncodeInt(int(frsmID))
	return MarshalConfirmedRequest(invokeID, asn1util.TagNumFileClose, false, payload)
}

// MarshalFileDeleteRequest builds a ConfirmedRequestPdu for FileDelete.
func MarshalFileDeleteRequest(invokeID codec.InvokeID, fileName string) ([]byte, error) {
	payload := encodeFileName(fileName)
	return MarshalConfirmedRequest(invokeID, asn1util.TagNumFileDelete, true, payload)
}

// MarshalFileDirectoryRequest builds a ConfirmedRequestPdu for FileDirectory.
//
//	FileDirectoryRequest ::= [77] IMPLICIT SEQUENCE {
//	    fileSpecification [0] IMPLICIT FileName OPTIONAL
//	    continueAfter     [1] IMPLICIT FileName OPTIONAL
//	}
func MarshalFileDirectoryRequest(invokeID codec.InvokeID, fileSpec string, continueAfter string) ([]byte, error) {
	var payload []byte
	if fileSpec != "" {
		payload = append(payload, encodeFileNameTag(0xa0, fileSpec)...)
	}
	if continueAfter != "" {
		payload = append(payload, encodeFileNameTag(0xa1, continueAfter)...)
	}
	return MarshalConfirmedRequest(invokeID, asn1util.TagNumFileDirectory, true, payload)
}

// MarshalFileRenameRequest builds a ConfirmedRequestPdu for FileRename.
//
//	FileRenameRequest ::= [75] IMPLICIT SEQUENCE {
//	    currentFileName [0] IMPLICIT FileName,
//	    newFileName     [1] IMPLICIT FileName
//	}
func MarshalFileRenameRequest(invokeID codec.InvokeID, currentName, newName string) ([]byte, error) {
	cur := encodeFileNameTag(0xa0, currentName)
	new_ := encodeFileNameTag(0xa1, newName)

	payload := make([]byte, 0, len(cur)+len(new_))
	payload = append(payload, cur...)
	payload = append(payload, new_...)

	return MarshalConfirmedRequest(invokeID, asn1util.TagNumFileRename, true, payload)
}

// MarshalObtainFileRequest builds a ConfirmedRequestPdu for ObtainFile.
//
//	ObtainFileRequest ::= [46] IMPLICIT SEQUENCE {
//	    sourceFileName      [1] IMPLICIT FileName,
//	    destinationFileName [2] IMPLICIT FileName
//	}
func MarshalObtainFileRequest(invokeID codec.InvokeID, sourceFile, destFile string) ([]byte, error) {
	src := encodeFileNameTag(0xa1, sourceFile)
	dst := encodeFileNameTag(0xa2, destFile)

	payload := make([]byte, 0, len(src)+len(dst))
	payload = append(payload, src...)
	payload = append(payload, dst...)

	return MarshalConfirmedRequest(invokeID, asn1util.TagNumObtainFile, true, payload)
}

// FileRenameRequest holds the parsed FileRename request fields.
type FileRenameRequest struct {
	CurrentFileName string
	NewFileName     string
}

// ObtainFileRequest holds the parsed ObtainFile request fields.
type ObtainFileRequest struct {
	SourceFile      string
	DestinationFile string
}

// --- Server-side request unmarshalers ---

// UnmarshalFileOpenRequest decodes a FileOpen request body.
func UnmarshalFileOpenRequest(data []byte) (*FileOpenRequest, error) {
	req := &FileOpenRequest{}
	offset := 0
	for offset < len(data) {
		tag, inner, n, err := berutil.DecodeTLVAt(data, offset)
		if err != nil {
			return nil, fmt.Errorf("pdu: file-open request: %w", err)
		}
		offset += n
		switch tag {
		case 0xa0: // fileName
			name, nameErr := decodeFileName(inner)
			if nameErr != nil {
				return nil, fmt.Errorf("pdu: file-open fileName: %w", nameErr)
			}
			req.FileName = name
		case 0x81: // initialPosition
			v, err := berutil.DecodeUnsigned(inner)
			if err != nil {
				return nil, fmt.Errorf("pdu: file-open initialPosition: %w", err)
			}
			req.InitialPosition = v
		default:
			return nil, fmt.Errorf("pdu: unexpected tag 0x%02x in UnmarshalFileOpenRequest", tag)
		}
	}
	if req.FileName == "" {
		return nil, fmt.Errorf("pdu: file-open request: missing fileName")
	}
	return req, nil
}

// UnmarshalFileReadRequest decodes a FileRead request body (just frsmId).
func UnmarshalFileReadRequest(data []byte) (int32, error) {
	v, err := berutil.DecodeInteger(data)
	if err != nil {
		return 0, fmt.Errorf("pdu: file-read request: %w", err)
	}
	return int32(v), nil
}

// UnmarshalFileCloseRequest decodes a FileClose request body (just frsmId).
func UnmarshalFileCloseRequest(data []byte) (int32, error) {
	v, err := berutil.DecodeInteger(data)
	if err != nil {
		return 0, fmt.Errorf("pdu: file-close request: %w", err)
	}
	return int32(v), nil
}

// UnmarshalFileDeleteRequest decodes a FileDelete request body.
func UnmarshalFileDeleteRequest(data []byte) (string, error) {
	return decodeFileName(data)
}

// UnmarshalFileDirectoryRequest decodes a FileDirectory request body.
func UnmarshalFileDirectoryRequest(data []byte) (*FileDirectoryRequest, error) {
	req := &FileDirectoryRequest{}
	offset := 0
	for offset < len(data) {
		tag, inner, n, err := berutil.DecodeTLVAt(data, offset)
		if err != nil {
			return nil, fmt.Errorf("pdu: file-directory request: %w", err)
		}
		offset += n
		switch tag {
		case 0xa0: // fileSpecification
			name, nameErr := decodeFileName(inner)
			if nameErr != nil {
				return nil, fmt.Errorf("pdu: file-directory fileSpec: %w", nameErr)
			}
			req.FileSpec = name
		case 0xa1: // continueAfter (FileName)
			name, nameErr := decodeFileName(inner)
			if nameErr != nil {
				return nil, fmt.Errorf("pdu: file-directory continueAfter: %w", nameErr)
			}
			req.ContinueAfter = name
		default:
			return nil, fmt.Errorf("pdu: unexpected tag 0x%02x in UnmarshalFileDirectoryRequest", tag)
		}
	}
	return req, nil
}

// UnmarshalFileRenameRequest decodes a FileRename request body.
func UnmarshalFileRenameRequest(data []byte) (*FileRenameRequest, error) {
	req := &FileRenameRequest{}
	offset := 0
	for offset < len(data) {
		tag, inner, n, err := berutil.DecodeTLVAt(data, offset)
		if err != nil {
			return nil, fmt.Errorf("pdu: file-rename request: %w", err)
		}
		offset += n
		switch tag {
		case 0xa0: // currentFileName
			name, nameErr := decodeFileName(inner)
			if nameErr != nil {
				return nil, fmt.Errorf("pdu: file-rename currentFileName: %w", nameErr)
			}
			req.CurrentFileName = name
		case 0xa1: // newFileName
			name, nameErr := decodeFileName(inner)
			if nameErr != nil {
				return nil, fmt.Errorf("pdu: file-rename newFileName: %w", nameErr)
			}
			req.NewFileName = name
		default:
			return nil, fmt.Errorf("pdu: unexpected tag 0x%02x in UnmarshalFileRenameRequest", tag)
		}
	}
	if req.CurrentFileName == "" || req.NewFileName == "" {
		return nil, fmt.Errorf("pdu: file-rename request: missing required field(s)")
	}
	return req, nil
}

// UnmarshalObtainFileRequest decodes an ObtainFile request body.
func UnmarshalObtainFileRequest(data []byte) (*ObtainFileRequest, error) {
	req := &ObtainFileRequest{}
	offset := 0
	for offset < len(data) {
		tag, inner, n, err := berutil.DecodeTLVAt(data, offset)
		if err != nil {
			return nil, fmt.Errorf("pdu: obtain-file request: %w", err)
		}
		offset += n
		switch tag {
		case 0xa1: // sourceFileName
			name, nameErr := decodeFileName(inner)
			if nameErr != nil {
				return nil, fmt.Errorf("pdu: obtain-file sourceFileName: %w", nameErr)
			}
			req.SourceFile = name
		case 0xa2: // destinationFileName
			name, nameErr := decodeFileName(inner)
			if nameErr != nil {
				return nil, fmt.Errorf("pdu: obtain-file destinationFileName: %w", nameErr)
			}
			req.DestinationFile = name
		default:
			return nil, fmt.Errorf("pdu: unexpected tag 0x%02x in UnmarshalObtainFileRequest", tag)
		}
	}
	if req.SourceFile == "" || req.DestinationFile == "" {
		return nil, fmt.Errorf("pdu: obtain-file request: missing required field(s)")
	}
	return req, nil
}

// --- Server-side response marshalers ---

// MarshalFileOpenResponse encodes a FileOpen response.
//
//	FileOpenResponse ::= SEQUENCE {
//	    frsmID          [0] IMPLICIT Integer32,
//	    fileAttributes  [1] IMPLICIT FileAttributes
//	}
//
//	FileAttributes ::= SEQUENCE {
//	    sizeOfFile     [0] IMPLICIT Unsigned32,
//	    lastModified   [1] IMPLICIT GeneralizedTime OPTIONAL
//	}
func MarshalFileOpenResponse(frsmID int32, size int64, lastModified time.Time) ([]byte, error) {
	if size < 0 || size > math.MaxUint32 {
		return nil, fmt.Errorf("pdu: file size %d exceeds MMS Unsigned32 range [0, %d]", size, math.MaxUint32)
	}
	frsmBytes := berutil.EncodeTLV(0x80, berutil.EncodeInt(int(frsmID)))

	sizeBytes := berutil.EncodeTLV(0x80, berutil.EncodeUint32(uint32(size)))
	timeStr := lastModified.UTC().Format("20060102150405Z")
	timeBytes := berutil.EncodeTLV(0x81, []byte(timeStr))
	attrInner := append(sizeBytes, timeBytes...)
	attrBytes := berutil.EncodeTLV(0xa1, attrInner)

	payload := make([]byte, 0, len(frsmBytes)+len(attrBytes))
	payload = append(payload, frsmBytes...)
	payload = append(payload, attrBytes...)
	return payload, nil
}

// MarshalFileReadResponse encodes a FileRead response.
//
//	FileReadResponse ::= SEQUENCE {
//	    fileData    [0] IMPLICIT OCTET STRING,
//	    moreFollows [1] IMPLICIT BOOLEAN DEFAULT TRUE
//	}
func MarshalFileReadResponse(data []byte, moreFollows bool) ([]byte, error) {
	dataBytes := berutil.EncodeTLV(0x80, data)

	var payload []byte
	payload = append(payload, dataBytes...)
	if !moreFollows {
		payload = append(payload, berutil.EncodeTLV(0x81, []byte{0x00})...)
	}
	return payload, nil
}

// MarshalFileDirectoryResponse encodes a FileDirectory response.
//
//	FileDirectoryResponse ::= SEQUENCE {
//	    listOfDirectoryEntry [0] IMPLICIT SEQUENCE OF DirectoryEntry,
//	    moreFollows          [1] IMPLICIT BOOLEAN DEFAULT TRUE OPTIONAL
//	}
//
//	DirectoryEntry ::= SEQUENCE {
//	    fileName       [0] IMPLICIT FileName,
//	    fileAttributes [1] IMPLICIT FileAttributes
//	}
func MarshalFileDirectoryResponse(entries []FileDirectoryEntry, moreFollows bool) ([]byte, error) {
	var entriesBytes []byte
	for _, e := range entries {
		if e.Size < 0 || e.Size > math.MaxUint32 {
			return nil, fmt.Errorf("pdu: file %q size %d exceeds MMS Unsigned32 range [0, %d]", e.FileName, e.Size, math.MaxUint32)
		}
		nameBytes := berutil.EncodeTLV(0xa0, encodeFileNameInner(e.FileName))

		sizeBytes := berutil.EncodeTLV(0x80, berutil.EncodeUint32(uint32(e.Size)))
		timeStr := e.LastModified.UTC().Format("20060102150405Z")
		timeBytes := berutil.EncodeTLV(0x81, []byte(timeStr))
		attrInner := append(sizeBytes, timeBytes...)
		attrBytes := berutil.EncodeTLV(0xa1, attrInner)

		entryInner := append(nameBytes, attrBytes...)
		entriesBytes = append(entriesBytes, berutil.EncodeTLV(0x30, entryInner)...)
	}

	listBytes := berutil.EncodeTLV(0xa0, entriesBytes)
	payload := listBytes
	if moreFollows {
		payload = append(payload, berutil.EncodeTLV(0x81, []byte{0xff})...)
	}
	return payload, nil
}

// --- Client-side response unmarshalers ---

// FileOpenResult holds the parsed FileOpen response fields.
type FileOpenResult struct {
	FrsmID       int32
	Size         int64
	LastModified time.Time
}

// UnmarshalFileOpenResponse decodes a FileOpen response. Returns an
// error if the frsmID or file size are missing.
func UnmarshalFileOpenResponse(data []byte) (*FileOpenResult, error) {
	r := &FileOpenResult{}
	var hasFrsmID, hasSize bool
	offset := 0
	for offset < len(data) {
		tag, inner, n, err := berutil.DecodeTLVAt(data, offset)
		if err != nil {
			return nil, fmt.Errorf("pdu: file-open response: %w", err)
		}
		offset += n
		switch tag {
		case 0x80: // frsmID
			v, err := berutil.DecodeInteger(inner)
			if err != nil {
				return nil, fmt.Errorf("pdu: file-open frsmID: %w", err)
			}
			r.FrsmID = int32(v)
			hasFrsmID = true
		case 0xa1: // fileAttributes
			gotSize, err := parseFileAttributes(inner, r)
			if err != nil {
				return nil, err
			}
			hasSize = gotSize
		default:
			return nil, fmt.Errorf("pdu: unexpected tag 0x%02x in UnmarshalFileOpenResponse", tag)
		}
	}
	if !hasFrsmID {
		return nil, fmt.Errorf("pdu: file-open response: missing frsmID")
	}
	if !hasSize {
		return nil, fmt.Errorf("pdu: file-open response: missing file size")
	}
	return r, nil
}

func parseFileAttributes(data []byte, r *FileOpenResult) (hasSize bool, _ error) {
	offset := 0
	for offset < len(data) {
		tag, inner, n, err := berutil.DecodeTLVAt(data, offset)
		if err != nil {
			return false, fmt.Errorf("pdu: file attributes: %w", err)
		}
		offset += n
		switch tag {
		case 0x80: // sizeOfFile
			v, err := berutil.DecodeUnsigned(inner)
			if err != nil {
				return false, fmt.Errorf("pdu: file size: %w", err)
			}
			r.Size = int64(v)
			hasSize = true
		case 0x81: // lastModified
			t, err := time.Parse("20060102150405Z", string(inner))
			if err != nil {
				return false, fmt.Errorf("pdu: file lastModified: %w", err)
			}
			r.LastModified = t
		default:
			return false, fmt.Errorf("pdu: unexpected tag 0x%02x in parseFileAttributes", tag)
		}
	}
	return hasSize, nil
}

// FileReadResult holds the parsed FileRead response fields.
// Data is an owned copy; it does not alias the transport buffer.
type FileReadResult struct {
	Data        []byte
	MoreFollows bool
}

// UnmarshalFileReadResponse decodes a FileRead response.
func UnmarshalFileReadResponse(data []byte) (*FileReadResult, error) {
	r := &FileReadResult{MoreFollows: true} // default per spec
	offset := 0
	for offset < len(data) {
		tag, inner, n, err := berutil.DecodeTLVAt(data, offset)
		if err != nil {
			return nil, fmt.Errorf("pdu: file-read response: %w", err)
		}
		offset += n
		switch tag {
		case 0x80: // fileData
			r.Data = make([]byte, len(inner))
			copy(r.Data, inner)
		case 0x81: // moreFollows
			if len(inner) > 0 {
				r.MoreFollows = inner[0] != 0
			}
		default:
			return nil, fmt.Errorf("pdu: unexpected tag 0x%02x in UnmarshalFileReadResponse", tag)
		}
	}
	return r, nil
}

// UnmarshalFileDirectoryResponse decodes a FileDirectory response.
func UnmarshalFileDirectoryResponse(data []byte) ([]FileDirectoryEntry, bool, error) {
	var entries []FileDirectoryEntry
	moreFollows := false
	offset := 0
	for offset < len(data) {
		tag, inner, n, err := berutil.DecodeTLVAt(data, offset)
		if err != nil {
			return nil, false, fmt.Errorf("pdu: file-directory response: %w", err)
		}
		offset += n
		switch tag {
		case 0xa0: // listOfDirectoryEntry
			parsed, err := parseDirectoryEntries(inner)
			if err != nil {
				return nil, false, err
			}
			entries = parsed
		case 0x81: // moreFollows
			if len(inner) > 0 {
				moreFollows = inner[0] != 0
			}
		default:
			return nil, false, fmt.Errorf("pdu: unexpected tag 0x%02x in UnmarshalFileDirectoryResponse", tag)
		}
	}
	return entries, moreFollows, nil
}

func parseDirectoryEntries(data []byte) ([]FileDirectoryEntry, error) {
	var entries []FileDirectoryEntry
	offset := 0
	for offset < len(data) {
		tag, inner, n, err := berutil.DecodeTLVAt(data, offset)
		if err != nil {
			return nil, fmt.Errorf("pdu: directory entry: %w", err)
		}
		if tag != 0x30 {
			return nil, fmt.Errorf("pdu: directory entry: expected SEQUENCE (0x30), got 0x%02x", tag)
		}
		offset += n

		e := FileDirectoryEntry{}
		eOff := 0
		for eOff < len(inner) {
			etag, einner, en, err := berutil.DecodeTLVAt(inner, eOff)
			if err != nil {
				return nil, fmt.Errorf("pdu: directory entry field: %w", err)
			}
			eOff += en
			switch etag {
			case 0xa0: // fileName
				name, nameErr := decodeFileName(einner)
				if nameErr != nil {
					return nil, fmt.Errorf("pdu: directory entry fileName: %w", nameErr)
				}
				e.FileName = name
			case 0xa1: // fileAttributes
				r := &FileOpenResult{}
				if _, err := parseFileAttributes(einner, r); err != nil {
					return nil, err
				}
				e.Size = r.Size
				e.LastModified = r.LastModified
			default:
				return nil, fmt.Errorf("pdu: unexpected tag 0x%02x in parseDirectoryEntries", etag)
			}
		}
		entries = append(entries, e)
		if len(entries) > maxDirectoryEntries {
			return nil, fmt.Errorf("pdu: directory entries %d exceeds maximum %d", len(entries), maxDirectoryEntries)
		}
	}
	return entries, nil
}

// --- FileName encoding helpers ---

// MMS FileName is defined as SEQUENCE OF GraphicString (ISO 9506-2).
// In practice, implementations use exactly one GraphicString element
// per FileName. The wire encoding is:
//
//	FileName ::= [IMPLICIT context] { GraphicString(0x19) }
//
// The default context tag 0xa0 is used for the first FileName field
// in a service request. FileRename/ObtainFile use 0xa0/0xa1/0xa2
// to distinguish their FileName parameters via IMPLICIT tagging.
//
// On decode, both the wrapped form ([ctx] { 0x19 ... }) and a bare
// GraphicString (0x19 ...) are accepted for interop tolerance.

func encodeFileName(name string) []byte {
	inner := encodeFileNameInner(name)
	return berutil.EncodeTLV(0xa0, inner)
}

func encodeFileNameTag(tag byte, name string) []byte {
	inner := encodeFileNameInner(name)
	return berutil.EncodeTLV(tag, inner)
}

func encodeFileNameInner(name string) []byte {
	return berutil.EncodeTLV(0x19, []byte(name))
}

// decodeFileName decodes a FileName from BER data. Accepts both the
// spec-faithful wrapped form and a bare GraphicString for compatibility:
//
//   - [ctx] { 0x19 len string }  — IMPLICIT SEQUENCE OF GraphicString
//   - 0x19 len string            — bare GraphicString (lenient)
func decodeFileName(data []byte) (string, error) {
	if len(data) == 0 {
		return "", fmt.Errorf("pdu: empty FileName")
	}
	tag, inner, n, err := berutil.DecodeTLVAt(data, 0)
	if err != nil {
		return "", fmt.Errorf("pdu: decode FileName: %w", err)
	}

	if tag == 0x19 {
		if n != len(data) {
			return "", fmt.Errorf("pdu: FileName: %d trailing bytes after GraphicString", len(data)-n)
		}
		if len(inner) > maxFileNameLength {
			return "", fmt.Errorf("pdu: file name length %d exceeds maximum %d", len(inner), maxFileNameLength)
		}
		return string(inner), nil
	}

	if tag&0x20 != 0 { // constructed
		if n != len(data) {
			return "", fmt.Errorf("pdu: FileName: %d trailing bytes after wrapper", len(data)-n)
		}
		itag, iinner, in, ierr := berutil.DecodeTLVAt(inner, 0)
		if ierr != nil {
			return "", fmt.Errorf("pdu: decode FileName inner: %w", ierr)
		}
		if in != len(inner) {
			return "", fmt.Errorf("pdu: FileName: %d trailing bytes inside wrapper", len(inner)-in)
		}
		if itag == 0x19 {
			if len(iinner) > maxFileNameLength {
				return "", fmt.Errorf("pdu: file name length %d exceeds maximum %d", len(iinner), maxFileNameLength)
			}
			return string(iinner), nil
		}
		return "", fmt.Errorf("pdu: FileName: expected GraphicString (0x19) inside wrapper, got 0x%02x", itag)
	}

	return "", fmt.Errorf("pdu: FileName: unexpected tag 0x%02x", tag)
}
