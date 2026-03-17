package pdu

import (
	"encoding/asn1"
	"testing"

	"github.com/otfabric/go-mms/internal/berutil"
)

// --- File decoder unknown-tag tests ---

func TestUnmarshalFileOpenRequestUnknownTag(t *testing.T) {
	fileNameInner := berutil.EncodeTLV(0x19, []byte("test.dat"))
	fileName := berutil.EncodeTLV(0xa0, fileNameInner)
	unknown := berutil.EncodeTLV(0x99, []byte{0x01})

	data := append(fileName, unknown...)
	_, err := UnmarshalFileOpenRequest(data)
	if err == nil {
		t.Fatal("expected error for unknown tag in FileOpenRequest")
	}
}

func TestUnmarshalFileDirectoryRequestUnknownTag(t *testing.T) {
	fileSpecInner := berutil.EncodeTLV(0x19, []byte("*.cfg"))
	fileSpec := berutil.EncodeTLV(0xa0, fileSpecInner)
	unknown := berutil.EncodeTLV(0x99, []byte{0x01})

	data := append(fileSpec, unknown...)
	_, err := UnmarshalFileDirectoryRequest(data)
	if err == nil {
		t.Fatal("expected error for unknown tag in FileDirectoryRequest")
	}
}

func TestUnmarshalFileRenameRequestUnknownTag(t *testing.T) {
	curInner := berutil.EncodeTLV(0x19, []byte("old.txt"))
	cur := berutil.EncodeTLV(0xa0, curInner)
	newInner := berutil.EncodeTLV(0x19, []byte("new.txt"))
	new_ := berutil.EncodeTLV(0xa1, newInner)
	unknown := berutil.EncodeTLV(0x99, []byte{0x01})

	var data []byte
	data = append(data, cur...)
	data = append(data, new_...)
	data = append(data, unknown...)
	_, err := UnmarshalFileRenameRequest(data)
	if err == nil {
		t.Fatal("expected error for unknown tag in FileRenameRequest")
	}
}

func TestUnmarshalObtainFileRequestUnknownTag(t *testing.T) {
	srcInner := berutil.EncodeTLV(0x19, []byte("src.bin"))
	src := berutil.EncodeTLV(0xa1, srcInner)
	dstInner := berutil.EncodeTLV(0x19, []byte("dst.bin"))
	dst := berutil.EncodeTLV(0xa2, dstInner)
	unknown := berutil.EncodeTLV(0x99, []byte{0x01})

	var data []byte
	data = append(data, src...)
	data = append(data, dst...)
	data = append(data, unknown...)
	_, err := UnmarshalObtainFileRequest(data)
	if err == nil {
		t.Fatal("expected error for unknown tag in ObtainFileRequest")
	}
}

func TestUnmarshalFileOpenResponseUnknownTag(t *testing.T) {
	frsmBytes := berutil.EncodeTLV(0x80, berutil.EncodeInt(1))
	sizeBytes := berutil.EncodeTLV(0x80, berutil.EncodeUint32(100))
	attrBytes := berutil.EncodeTLV(0xa1, sizeBytes)
	unknown := berutil.EncodeTLV(0x99, []byte{0x01})

	var data []byte
	data = append(data, frsmBytes...)
	data = append(data, attrBytes...)
	data = append(data, unknown...)
	_, err := UnmarshalFileOpenResponse(data)
	if err == nil {
		t.Fatal("expected error for unknown tag in FileOpenResponse")
	}
}

func TestUnmarshalFileReadResponseUnknownTag(t *testing.T) {
	fileData := berutil.EncodeTLV(0x80, []byte("data"))
	unknown := berutil.EncodeTLV(0x99, []byte{0x01})

	data := append(fileData, unknown...)
	_, err := UnmarshalFileReadResponse(data)
	if err == nil {
		t.Fatal("expected error for unknown tag in FileReadResponse")
	}
}

func TestUnmarshalReadJournalRequestUnknownTag(t *testing.T) {
	name, err := EncodeObjectName(ObjectNameWire{
		Scope:    ScopeDomain,
		DomainID: "D",
		ItemID:   "J",
	})
	if err != nil {
		t.Fatalf("EncodeObjectName: %v", err)
	}
	nameWrapped := berutil.EncodeTLV(0xa0, name)
	unknown := berutil.EncodeTLV(0x99, []byte{0x01})

	data := append(nameWrapped, unknown...)
	_, err = UnmarshalReadJournalRequest(data)
	if err == nil {
		t.Fatal("expected error for unknown tag in ReadJournalRequest")
	}
}

func TestUnmarshalReadJournalResponseUnknownTag(t *testing.T) {
	unknown := berutil.EncodeTLV(0x99, []byte{0x01})
	_, _, err := UnmarshalReadJournalResponse(unknown)
	if err == nil {
		t.Fatal("expected error for unknown tag in ReadJournalResponse")
	}
}

// --- Trailing-bytes tests ---

func TestUnmarshalAccessResultsTrailingBytes(t *testing.T) {
	item := berutil.EncodeTLV(TagDataBoolean, []byte{0xff})
	data := append(append([]byte(nil), item...), 0xff)
	_, err := UnmarshalAccessResults(data)
	if err == nil {
		t.Fatal("expected error for trailing bytes in access results")
	}
}

func TestUnmarshalWriteResponseTrailingBytes(t *testing.T) {
	content := berutil.EncodeTLV(0x81, nil) // single success
	content = append(content, 0xff)         // trailing byte

	serviceData := asn1.RawValue{
		Class:      asn1.ClassContextSpecific,
		Tag:        5,
		IsCompound: true,
		Bytes:      content,
	}
	_, err := UnmarshalWriteResponse(serviceData)
	if err == nil {
		t.Fatal("expected error for trailing bytes in write response")
	}
}

func TestUnmarshalInformationReportTrailingBytes(t *testing.T) {
	// Build a valid InformationReport inner content with a trailing byte.
	// variableListName: [1] CONSTRUCTED { [0] IMPLICIT "v" }
	varListName := berutil.EncodeTLV(0xa1, berutil.EncodeTLV(0x80, []byte("v")))
	// listOfAccessResult: [0] { boolean true }
	boolVal := berutil.EncodeTLV(TagDataBoolean, []byte{0xff})
	resultList := berutil.EncodeTLV(0xa0, boolVal)

	// irContent = varAccessSpec + resultList + trailing byte
	var irContent []byte
	irContent = append(irContent, varListName...)
	irContent = append(irContent, resultList...)
	irContent = append(irContent, 0xff)

	// Wrap as tagInfoReport (0xa0)
	data := berutil.EncodeTLV(0xa0, irContent)
	_, err := UnmarshalInformationReport(data)
	if err == nil {
		t.Fatal("expected error for trailing bytes in InformationReport")
	}
}
