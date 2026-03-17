package pdu

import (
	"encoding/asn1"
	"fmt"
	"testing"
	"time"

	"github.com/otfabric/go-mms/internal/asn1util"
	"github.com/otfabric/go-mms/internal/berutil"
	"github.com/otfabric/go-mms/internal/codec"
)

func TestMarshalFileOpenRequest_RoundTrip(t *testing.T) {
	data, err := MarshalFileOpenRequest(1, "test.dat", 0)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var raw asn1.RawValue
	rest, err := asn1.Unmarshal(data, &raw)
	if err != nil {
		t.Fatalf("unmarshal outer: %v", err)
	}
	if len(rest) != 0 {
		t.Fatalf("trailing bytes: %d", len(rest))
	}
	if raw.Tag != 0 {
		t.Errorf("outer tag = %d, want 0 (ConfirmedRequest)", raw.Tag)
	}

	invokeID, serviceRaw, err := codec.UnmarshalConfirmedRequest(raw.Bytes)
	if err != nil {
		t.Fatalf("unmarshal confirmed request: %v", err)
	}
	if invokeID != 1 {
		t.Errorf("invokeID = %d, want 1", invokeID)
	}
	if serviceRaw.Tag != asn1util.TagNumFileOpen {
		t.Errorf("service tag = %d, want %d (FileOpen)", serviceRaw.Tag, asn1util.TagNumFileOpen)
	}

	req, err := UnmarshalFileOpenRequest(serviceRaw.Bytes)
	if err != nil {
		t.Fatalf("unmarshal file-open request: %v", err)
	}
	if req.FileName != "test.dat" {
		t.Errorf("fileName = %q, want %q", req.FileName, "test.dat")
	}
}

func TestMarshalFileDirectoryRequest_RoundTrip(t *testing.T) {
	data, err := MarshalFileDirectoryRequest(5, "*.cfg", "")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var raw asn1.RawValue
	if _, err := asn1.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal outer: %v", err)
	}

	_, serviceRaw, err := codec.UnmarshalConfirmedRequest(raw.Bytes)
	if err != nil {
		t.Fatalf("unmarshal confirmed: %v", err)
	}
	if serviceRaw.Tag != asn1util.TagNumFileDirectory {
		t.Errorf("service tag = %d, want %d", serviceRaw.Tag, asn1util.TagNumFileDirectory)
	}

	req, err := UnmarshalFileDirectoryRequest(serviceRaw.Bytes)
	if err != nil {
		t.Fatalf("unmarshal directory request: %v", err)
	}
	if req.FileSpec != "*.cfg" {
		t.Errorf("fileSpec = %q, want %q", req.FileSpec, "*.cfg")
	}
}

func TestUnmarshalFileOpenResponse_Validation(t *testing.T) {
	t.Run("missing frsmID", func(t *testing.T) {
		sizeBytes := berutil.EncodeTLV(0x80, berutil.EncodeUint32(100))
		attrBytes := berutil.EncodeTLV(0xa1, sizeBytes)
		_, err := UnmarshalFileOpenResponse(attrBytes)
		if err == nil {
			t.Fatal("expected error for missing frsmID")
		}
	})

	t.Run("missing file size", func(t *testing.T) {
		frsmBytes := berutil.EncodeTLV(0x80, berutil.EncodeInt(1))
		_, err := UnmarshalFileOpenResponse(frsmBytes)
		if err == nil {
			t.Fatal("expected error for missing file size")
		}
	})

	t.Run("valid response", func(t *testing.T) {
		frsmBytes := berutil.EncodeTLV(0x80, berutil.EncodeInt(42))
		sizeBytes := berutil.EncodeTLV(0x80, berutil.EncodeUint32(1024))
		timeStr := time.Date(2025, 6, 15, 10, 30, 0, 0, time.UTC).Format("20060102150405Z")
		timeBytes := berutil.EncodeTLV(0x81, []byte(timeStr))
		attrInner := append(sizeBytes, timeBytes...)
		attrBytes := berutil.EncodeTLV(0xa1, attrInner)
		payload := append(frsmBytes, attrBytes...)

		r, err := UnmarshalFileOpenResponse(payload)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if r.FrsmID != 42 {
			t.Errorf("frsmID = %d, want 42", r.FrsmID)
		}
		if r.Size != 1024 {
			t.Errorf("size = %d, want 1024", r.Size)
		}
	})
}

func TestParseDirectoryEntries_BadTag(t *testing.T) {
	bad := berutil.EncodeTLV(0xa0, []byte{0x01, 0x02})
	_, err := parseDirectoryEntries(bad)
	if err == nil {
		t.Fatal("expected error for non-SEQUENCE tag")
	}
}

func TestDecodeFileName_Strict(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		_, err := decodeFileName(nil)
		if err == nil {
			t.Fatal("expected error for empty data")
		}
	})

	t.Run("valid GraphicString", func(t *testing.T) {
		data := berutil.EncodeTLV(0x19, []byte("hello.txt"))
		name, err := decodeFileName(data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if name != "hello.txt" {
			t.Errorf("name = %q, want %q", name, "hello.txt")
		}
	})

	t.Run("wrapped in context tag", func(t *testing.T) {
		inner := berutil.EncodeTLV(0x19, []byte("wrapped.dat"))
		data := berutil.EncodeTLV(0xa0, inner)
		name, err := decodeFileName(data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if name != "wrapped.dat" {
			t.Errorf("name = %q, want %q", name, "wrapped.dat")
		}
	})

	t.Run("wrong inner tag", func(t *testing.T) {
		inner := berutil.EncodeTLV(0x02, []byte{0x01})
		data := berutil.EncodeTLV(0xa0, inner)
		_, err := decodeFileName(data)
		if err == nil {
			t.Fatal("expected error for wrong inner tag")
		}
	})

	t.Run("unexpected primitive tag", func(t *testing.T) {
		data := berutil.EncodeTLV(0x02, []byte{0x42})
		_, err := decodeFileName(data)
		if err == nil {
			t.Fatal("expected error for unexpected tag")
		}
	})
}

func TestClassifyServiceTag(t *testing.T) {
	tests := []struct {
		tagNum int
		want   ServiceKind
	}{
		{asn1util.TagNumStatus, ServiceStatus},
		{asn1util.TagNumGetNameList, ServiceGetNameList},
		{asn1util.TagNumIdentify, ServiceIdentify},
		{asn1util.TagNumRead, ServiceRead},
		{asn1util.TagNumWrite, ServiceWrite},
		{asn1util.TagNumGetVariableAccessAttributes, ServiceGetVariableAccessAttrs},
		{asn1util.TagNumDefineNamedVariableList, ServiceDefineNamedVariableList},
		{asn1util.TagNumGetNamedVariableListAttrs, ServiceGetNamedVariableListAttrs},
		{asn1util.TagNumDeleteNamedVariableList, ServiceDeleteNamedVariableList},
		{asn1util.TagNumFileOpen, ServiceFileOpen},
		{asn1util.TagNumFileRead, ServiceFileRead},
		{asn1util.TagNumFileClose, ServiceFileClose},
		{asn1util.TagNumFileDelete, ServiceFileDelete},
		{asn1util.TagNumFileDirectory, ServiceFileDirectory},
		{999, ServiceUnknown},
	}
	for _, tt := range tests {
		got := ClassifyServiceTag(tt.tagNum)
		if got != tt.want {
			t.Errorf("ClassifyServiceTag(%d) = %s, want %s", tt.tagNum, got, tt.want)
		}
	}
}

func TestMarshalConfirmedRequest_ExtendedTag(t *testing.T) {
	for _, tagNum := range []int{72, 73, 74, 76, 77} {
		data, err := MarshalConfirmedRequest(1, tagNum, true, []byte{0x01})
		if err != nil {
			t.Fatalf("tagNum=%d: marshal: %v", tagNum, err)
		}

		var raw asn1.RawValue
		rest, err := asn1.Unmarshal(data, &raw)
		if err != nil {
			t.Fatalf("tagNum=%d: unmarshal outer: %v", tagNum, err)
		}
		if len(rest) != 0 {
			t.Errorf("tagNum=%d: %d trailing bytes", tagNum, len(rest))
		}

		_, serviceRaw, err := codec.UnmarshalConfirmedRequest(raw.Bytes)
		if err != nil {
			t.Fatalf("tagNum=%d: unmarshal confirmed: %v", tagNum, err)
		}
		if serviceRaw.Tag != tagNum {
			t.Errorf("tagNum=%d: decoded tag = %d", tagNum, serviceRaw.Tag)
		}
	}
}

func TestMarshalConfirmedResponse_ExtendedTag(t *testing.T) {
	for _, tagNum := range []int{72, 73, 77} {
		data, err := codec.MarshalConfirmedResponse(1, tagNum, true, []byte{0x01})
		if err != nil {
			t.Fatalf("tagNum=%d: marshal: %v", tagNum, err)
		}

		var raw asn1.RawValue
		rest, err := asn1.Unmarshal(data, &raw)
		if err != nil {
			t.Fatalf("tagNum=%d: unmarshal outer: %v", tagNum, err)
		}
		if len(rest) != 0 {
			t.Errorf("tagNum=%d: %d trailing bytes", tagNum, len(rest))
		}
		if raw.Tag != 1 {
			t.Errorf("tagNum=%d: outer tag = %d, want 1 (ConfirmedResponse)", tagNum, raw.Tag)
		}
	}
}

func TestMarshalFileOpenResponse_SizeOverflow(t *testing.T) {
	_, err := MarshalFileOpenResponse(1, int64(1)<<33, time.Now())
	if err == nil {
		t.Fatal("expected error for oversized file")
	}
	t.Logf("expected error: %v", err)

	_, err = MarshalFileOpenResponse(1, -1, time.Now())
	if err == nil {
		t.Fatal("expected error for negative file size")
	}
	t.Logf("expected error: %v", err)
}

func TestMarshalFileDirectoryResponse_SizeOverflow(t *testing.T) {
	_, err := MarshalFileDirectoryResponse([]FileDirectoryEntry{
		{FileName: "big.bin", Size: int64(1) << 33, LastModified: time.Now()},
	}, false)
	if err == nil {
		t.Fatal("expected error for oversized file in directory")
	}
	t.Logf("expected error: %v", err)
}

func TestDecodeFileName_InteropShapes(t *testing.T) {
	inner := berutil.EncodeTLV(0x19, []byte("test/path/file.dat"))
	wrapped := berutil.EncodeTLV(0xa0, inner)
	name, err := decodeFileName(wrapped)
	if err != nil {
		t.Fatalf("wrapped decode: %v", err)
	}
	if name != "test/path/file.dat" {
		t.Errorf("got %q, want 'test/path/file.dat'", name)
	}

	wrapped2 := berutil.EncodeTLV(0xa1, inner)
	name, err = decodeFileName(wrapped2)
	if err != nil {
		t.Fatalf("0xa1 wrapped decode: %v", err)
	}
	if name != "test/path/file.dat" {
		t.Errorf("got %q", name)
	}

	bare := berutil.EncodeTLV(0x19, []byte("bare.txt"))
	name, err = decodeFileName(bare)
	if err != nil {
		t.Fatalf("bare decode: %v", err)
	}
	if name != "bare.txt" {
		t.Errorf("got %q, want 'bare.txt'", name)
	}
}

func TestFileReadRequestRoundtrip(t *testing.T) {
	data, err := MarshalFileReadRequest(1, 5)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var raw asn1.RawValue
	if _, err := asn1.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal outer: %v", err)
	}

	invokeID, serviceRaw, err := codec.UnmarshalConfirmedRequest(raw.Bytes)
	if err != nil {
		t.Fatalf("unmarshal confirmed request: %v", err)
	}
	if invokeID != 1 {
		t.Errorf("invokeID = %d, want 1", invokeID)
	}
	if serviceRaw.Tag != asn1util.TagNumFileRead {
		t.Errorf("service tag = %d, want %d (FileRead)", serviceRaw.Tag, asn1util.TagNumFileRead)
	}

	frsmID, err := UnmarshalFileReadRequest(serviceRaw.Bytes)
	if err != nil {
		t.Fatalf("unmarshal file-read request: %v", err)
	}
	if frsmID != 5 {
		t.Errorf("frsmID = %d, want 5", frsmID)
	}
}

func TestFileCloseRequestRoundtrip(t *testing.T) {
	data, err := MarshalFileCloseRequest(2, 7)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var raw asn1.RawValue
	if _, err := asn1.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal outer: %v", err)
	}

	invokeID, serviceRaw, err := codec.UnmarshalConfirmedRequest(raw.Bytes)
	if err != nil {
		t.Fatalf("unmarshal confirmed request: %v", err)
	}
	if invokeID != 2 {
		t.Errorf("invokeID = %d, want 2", invokeID)
	}
	if serviceRaw.Tag != asn1util.TagNumFileClose {
		t.Errorf("service tag = %d, want %d (FileClose)", serviceRaw.Tag, asn1util.TagNumFileClose)
	}

	frsmID, err := UnmarshalFileCloseRequest(serviceRaw.Bytes)
	if err != nil {
		t.Fatalf("unmarshal file-close request: %v", err)
	}
	if frsmID != 7 {
		t.Errorf("frsmID = %d, want 7", frsmID)
	}
}

func TestFileDeleteRequestRoundtrip(t *testing.T) {
	data, err := MarshalFileDeleteRequest(3, "test.cfg")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var raw asn1.RawValue
	if _, err := asn1.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal outer: %v", err)
	}

	_, serviceRaw, err := codec.UnmarshalConfirmedRequest(raw.Bytes)
	if err != nil {
		t.Fatalf("unmarshal confirmed request: %v", err)
	}
	if serviceRaw.Tag != asn1util.TagNumFileDelete {
		t.Errorf("service tag = %d, want %d (FileDelete)", serviceRaw.Tag, asn1util.TagNumFileDelete)
	}

	fileName, err := UnmarshalFileDeleteRequest(serviceRaw.Bytes)
	if err != nil {
		t.Fatalf("unmarshal file-delete request: %v", err)
	}
	if fileName != "test.cfg" {
		t.Errorf("fileName = %q, want %q", fileName, "test.cfg")
	}
}

func TestFileRenameRequestRoundtrip(t *testing.T) {
	data, err := MarshalFileRenameRequest(4, "old.txt", "new.txt")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var raw asn1.RawValue
	if _, err := asn1.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal outer: %v", err)
	}

	_, serviceRaw, err := codec.UnmarshalConfirmedRequest(raw.Bytes)
	if err != nil {
		t.Fatalf("unmarshal confirmed request: %v", err)
	}
	if serviceRaw.Tag != asn1util.TagNumFileRename {
		t.Errorf("service tag = %d, want %d (FileRename)", serviceRaw.Tag, asn1util.TagNumFileRename)
	}

	req, err := UnmarshalFileRenameRequest(serviceRaw.Bytes)
	if err != nil {
		t.Fatalf("unmarshal file-rename request: %v", err)
	}
	if req.CurrentFileName != "old.txt" {
		t.Errorf("currentFileName = %q, want %q", req.CurrentFileName, "old.txt")
	}
	if req.NewFileName != "new.txt" {
		t.Errorf("newFileName = %q, want %q", req.NewFileName, "new.txt")
	}
}

func TestObtainFileRequestRoundtrip(t *testing.T) {
	data, err := MarshalObtainFileRequest(5, "source.bin", "dest.bin")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var raw asn1.RawValue
	if _, err := asn1.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal outer: %v", err)
	}

	_, serviceRaw, err := codec.UnmarshalConfirmedRequest(raw.Bytes)
	if err != nil {
		t.Fatalf("unmarshal confirmed request: %v", err)
	}
	if serviceRaw.Tag != asn1util.TagNumObtainFile {
		t.Errorf("service tag = %d, want %d (ObtainFile)", serviceRaw.Tag, asn1util.TagNumObtainFile)
	}

	req, err := UnmarshalObtainFileRequest(serviceRaw.Bytes)
	if err != nil {
		t.Fatalf("unmarshal obtain-file request: %v", err)
	}
	if req.SourceFile != "source.bin" {
		t.Errorf("sourceFile = %q, want %q", req.SourceFile, "source.bin")
	}
	if req.DestinationFile != "dest.bin" {
		t.Errorf("destinationFile = %q, want %q", req.DestinationFile, "dest.bin")
	}
}

func TestFileReadResponseRoundtrip(t *testing.T) {
	tests := []struct {
		name        string
		data        []byte
		moreFollows bool
	}{
		{"with data, moreFollows=true", []byte("hello world"), true},
		{"with data, moreFollows=false", []byte{0x01, 0x02, 0x03}, false},
		{"empty data, moreFollows=false", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded, err := MarshalFileReadResponse(tt.data, tt.moreFollows)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}

			result, err := UnmarshalFileReadResponse(encoded)
			if err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if string(result.Data) != string(tt.data) {
				t.Errorf("data = %q, want %q", result.Data, tt.data)
			}
			if result.MoreFollows != tt.moreFollows {
				t.Errorf("moreFollows = %v, want %v", result.MoreFollows, tt.moreFollows)
			}
		})
	}
}

func TestFileDirectoryResponseRoundtrip(t *testing.T) {
	now := time.Date(2025, 6, 15, 10, 30, 0, 0, time.UTC)
	entries := []FileDirectoryEntry{
		{FileName: "config.cfg", Size: 1024, LastModified: now},
		{FileName: "data/log.txt", Size: 65535, LastModified: now.Add(time.Hour)},
	}

	for _, moreFollows := range []bool{true, false} {
		t.Run(fmt.Sprintf("moreFollows=%v", moreFollows), func(t *testing.T) {
			encoded, err := MarshalFileDirectoryResponse(entries, moreFollows)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}

			got, gotMore, err := UnmarshalFileDirectoryResponse(encoded)
			if err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if gotMore != moreFollows {
				t.Errorf("moreFollows = %v, want %v", gotMore, moreFollows)
			}
			if len(got) != len(entries) {
				t.Fatalf("entries = %d, want %d", len(got), len(entries))
			}
			for i, e := range got {
				if e.FileName != entries[i].FileName {
					t.Errorf("entry[%d].FileName = %q, want %q", i, e.FileName, entries[i].FileName)
				}
				if e.Size != entries[i].Size {
					t.Errorf("entry[%d].Size = %d, want %d", i, e.Size, entries[i].Size)
				}
				if !e.LastModified.Equal(entries[i].LastModified) {
					t.Errorf("entry[%d].LastModified = %v, want %v", i, e.LastModified, entries[i].LastModified)
				}
			}
		})
	}
}

func TestFileRenameRequestMissingFields(t *testing.T) {
	t.Run("missing currentFileName", func(t *testing.T) {
		payload := encodeFileNameTag(0xa1, "new.txt")
		_, err := UnmarshalFileRenameRequest(payload)
		if err == nil {
			t.Fatal("expected error for missing currentFileName")
		}
	})

	t.Run("missing newFileName", func(t *testing.T) {
		payload := encodeFileNameTag(0xa0, "old.txt")
		_, err := UnmarshalFileRenameRequest(payload)
		if err == nil {
			t.Fatal("expected error for missing newFileName")
		}
	})
}

func TestObtainFileRequestMissingFields(t *testing.T) {
	t.Run("missing sourceFile", func(t *testing.T) {
		payload := encodeFileNameTag(0xa2, "dest.bin")
		_, err := UnmarshalObtainFileRequest(payload)
		if err == nil {
			t.Fatal("expected error for missing sourceFile")
		}
	})

	t.Run("missing destinationFile", func(t *testing.T) {
		payload := encodeFileNameTag(0xa1, "source.bin")
		_, err := UnmarshalObtainFileRequest(payload)
		if err == nil {
			t.Fatal("expected error for missing destinationFile")
		}
	})
}

func TestDecodeFileName_TrailingBytes(t *testing.T) {
	bare := berutil.EncodeTLV(0x19, []byte("ok.txt"))
	bareTrailing := append(append([]byte(nil), bare...), 0xff)
	if _, err := decodeFileName(bareTrailing); err == nil {
		t.Error("expected error for trailing bytes after bare GraphicString")
	}

	inner := berutil.EncodeTLV(0x19, []byte("ok.txt"))
	innerTrailing := append(append([]byte(nil), inner...), 0xaa)
	wrappedBad := berutil.EncodeTLV(0xa0, innerTrailing)
	if _, err := decodeFileName(wrappedBad); err == nil {
		t.Error("expected error for trailing bytes inside wrapper")
	}

	wrapped := berutil.EncodeTLV(0xa0, inner)
	wrappedTrailing := append(append([]byte(nil), wrapped...), 0xbb)
	if _, err := decodeFileName(wrappedTrailing); err == nil {
		t.Error("expected error for trailing bytes after wrapper")
	}
}

func TestMarshalFileOpenResponse(t *testing.T) {
	ts := time.Date(2024, 1, 15, 12, 30, 0, 0, time.UTC)
	data, err := MarshalFileOpenResponse(5, 1024, ts)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Fatal("empty response")
	}

	// Negative size should fail
	_, err = MarshalFileOpenResponse(5, -1, ts)
	if err == nil {
		t.Fatal("expected error for negative size")
	}

	// Size exceeding Unsigned32 max should fail
	_, err = MarshalFileOpenResponse(5, int64(1)<<33, ts)
	if err == nil {
		t.Fatal("expected error for oversized")
	}

	// Zero size should succeed
	data, err = MarshalFileOpenResponse(0, 0, ts)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Fatal("empty response for zero size")
	}
}

func TestDecodeFileName_TooLong(t *testing.T) {
	longName := make([]byte, maxFileNameLength+1)
	for i := range longName {
		longName[i] = 'a'
	}

	t.Run("bare GraphicString", func(t *testing.T) {
		data := berutil.EncodeTLV(0x19, longName)
		_, err := decodeFileName(data)
		if err == nil {
			t.Fatal("expected error for long file name")
		}
	})

	t.Run("wrapped in context tag", func(t *testing.T) {
		inner := berutil.EncodeTLV(0x19, longName)
		data := berutil.EncodeTLV(0xa0, inner)
		_, err := decodeFileName(data)
		if err == nil {
			t.Fatal("expected error for long file name in wrapper")
		}
	})

	t.Run("at limit is accepted", func(t *testing.T) {
		exactName := make([]byte, maxFileNameLength)
		for i := range exactName {
			exactName[i] = 'b'
		}
		data := berutil.EncodeTLV(0x19, exactName)
		name, err := decodeFileName(data)
		if err != nil {
			t.Fatalf("unexpected error for file name at limit: %v", err)
		}
		if len(name) != maxFileNameLength {
			t.Errorf("name length = %d, want %d", len(name), maxFileNameLength)
		}
	})
}

func TestParseDirectoryEntries_TooMany(t *testing.T) {
	now := time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC)
	entries := make([]FileDirectoryEntry, maxDirectoryEntries+1)
	for i := range entries {
		entries[i] = FileDirectoryEntry{
			FileName:     fmt.Sprintf("f%d.txt", i),
			Size:         100,
			LastModified: now,
		}
	}

	encoded, err := MarshalFileDirectoryResponse(entries, false)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	_, _, err = UnmarshalFileDirectoryResponse(encoded)
	if err == nil {
		t.Fatal("expected error for too many directory entries")
	}
}

func TestFileReadResponseDataOwnership(t *testing.T) {
	payload := []byte("hello world")
	svcPayload, err := MarshalFileReadResponse(payload, true)
	if err != nil {
		t.Fatal(err)
	}

	buf := make([]byte, len(svcPayload))
	copy(buf, svcPayload)

	result, err := UnmarshalFileReadResponse(buf)
	if err != nil {
		t.Fatal(err)
	}
	if string(result.Data) != "hello world" {
		t.Fatalf("data = %q, want %q", result.Data, "hello world")
	}

	for i := range buf {
		buf[i] = 0
	}
	if string(result.Data) != "hello world" {
		t.Fatal("returned Data aliases the transport buffer")
	}
}
