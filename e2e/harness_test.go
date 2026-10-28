// SPDX-License-Identifier: MIT

// Package e2e runs the go-mms client against the go-mms server over real
// TCP (transport/iso), covering every exported Client, Server and
// ServerConn method and every value type. TestAPICoverage keeps it that way.
package e2e

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"math"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	mms "github.com/otfabric/go-mms"
	"github.com/otfabric/go-mms/transport/iso"
)

const (
	dom      = "dev"
	auxDom   = "aux"
	auxCount = 300
	journal  = "log"
	tick     = 5 * time.Second
)

var t0 = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

func timeout(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// serve runs srv on a loopback iso listener until the test ends.
func serve(t *testing.T, srv *mms.Server) string {
	t.Helper()
	ln, err := iso.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("iso.Listen: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = srv.ListenAndServe(ctx, ln)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	return ln.Addr().String()
}

// dial connects a client and closes it when the test ends.
func dial(t *testing.T, addr string, opts ...mms.DialOptions) *mms.Client {
	t.Helper()
	var isoOpts []iso.Option
	if len(opts) > 0 {
		isoOpts = append(isoOpts, iso.WithClientDialOptions(opts[0]))
	}
	c, err := iso.Dial(timeout(t), addr, isoOpts...)
	if err != nil {
		t.Fatalf("iso.Dial: %v", err)
	}
	t.Cleanup(func() { _ = c.Abort(context.Background()) })
	return c
}

// eventually polls cond until it holds or the deadline passes.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func domName(item string) mms.ObjectName {
	return mms.ObjectName{Scope: mms.ObjectScopeDomain, Domain: dom, ItemID: mms.ItemID(item)}
}

func vmdName(item string) mms.ObjectName {
	return mms.ObjectName{Scope: mms.ObjectScopeVMD, ItemID: mms.ItemID(item)}
}

func aaName(item string) mms.ObjectName {
	return mms.ObjectName{Scope: mms.ObjectScopeAssociation, ItemID: mms.ItemID(item)}
}

func specs(names ...mms.ObjectName) []mms.VariableSpec {
	out := make([]mms.VariableSpec, len(names))
	for i, n := range names {
		out[i] = mms.VariableSpec{Name: n}
	}
	return out
}

func mustEqual(t *testing.T, what string, got, want *mms.Value) {
	t.Helper()
	if !got.Equal(want) {
		t.Errorf("%s = %v, want %v", what, got, want)
	}
}

func dataAccessCode(t *testing.T, err error) mms.DataAccessErrorCode {
	t.Helper()
	var dae *mms.DataAccessError
	if !errors.As(err, &dae) {
		t.Fatalf("error %v is not a *mms.DataAccessError", err)
	}
	return dae.Code
}

func serviceError(t *testing.T, err error) *mms.ServiceError {
	t.Helper()
	var se *mms.ServiceError
	if !errors.As(err, &se) {
		t.Fatalf("error %v is not a *mms.ServiceError", err)
	}
	return se
}

// ---------------------------------------------------------------------------
// Value matrix: one variable per value constructor
// ---------------------------------------------------------------------------

type valueCase struct {
	name    string // variable item ID; also the constructor under test
	ts      mms.TypeSpec
	initial *mms.Value
	write   *mms.Value
}

var (
	f32   = mms.TypeSpec{Type: mms.ValueTypeFloat, FormatWidth: 32, ExponentWidth: 8}
	f64   = mms.TypeSpec{Type: mms.ValueTypeFloat, FormatWidth: 64, ExponentWidth: 11}
	i32   = mms.TypeSpec{Type: mms.ValueTypeInteger, Size: 32}
	boolT = mms.TypeSpec{Type: mms.ValueTypeBoolean}
	vis32 = mms.TypeSpec{Type: mms.ValueTypeVisibleString, Size: 32}

	recordType = mms.TypeSpec{Type: mms.ValueTypeStructure, Elements: []mms.TypeSpecElement{
		{Name: "count", Type: i32},
		{Name: "flag", Type: boolT},
		{Name: "label", Type: vis32},
		{Name: "samples", Type: mms.TypeSpec{Type: mms.ValueTypeArray, Count: 3, Element: &i32}},
	}}
	arrayType = mms.TypeSpec{Type: mms.ValueTypeArray, Count: 5, Element: &i32}
)

func intArray(vals ...int64) *mms.Value {
	elems := make([]*mms.Value, len(vals))
	for i, v := range vals {
		elems[i] = mms.NewInteger(v)
	}
	return mms.NewArray(elems)
}

func record(count int64, flag bool, label string, samples ...int64) *mms.Value {
	return mms.NewStructure([]*mms.Value{
		mms.NewInteger(count), mms.NewBoolean(flag), mms.NewVisibleString(label), intArray(samples...),
	})
}

// valueCases covers every Value constructor that yields MMS Data. Real and
// BooleanArray have no ISO 9506-2 TypeDescription, so their variables are
// declared with the closest type (floating-point, bit-string).
func valueCases() []valueCase {
	return []valueCase{
		{"NewBoolean", boolT, mms.NewBoolean(true), mms.NewBoolean(false)},
		{"NewInteger", i32, mms.NewInteger(-123456), mms.NewInteger(math.MaxInt32)},
		{"NewInteger64", mms.TypeSpec{Type: mms.ValueTypeInteger, Size: 64}, mms.NewInteger(math.MinInt64), mms.NewInteger(math.MaxInt64)},
		{"NewUnsigned", mms.TypeSpec{Type: mms.ValueTypeUnsigned, Size: 32}, mms.NewUnsigned(42), mms.NewUnsigned(math.MaxUint32)},
		{"NewFloat", f32, mms.NewFloat(1.5), mms.NewFloat(-0.25)},
		{"NewFloat64", f64, mms.NewFloat(math.Pi), mms.NewFloat(-math.E)},
		{"NewBitString", mms.TypeSpec{Type: mms.ValueTypeBitString, Size: 16}, mms.NewBitString([]byte{0xa5, 0x0f}), mms.NewBitString([]byte{0x01, 0x80})},
		{"NewBitStringWithLength", mms.TypeSpec{Type: mms.ValueTypeBitString, Size: 13}, mms.NewBitStringWithLength([]byte{0xff, 0xf8}, 13), mms.NewBitStringWithLength([]byte{0x80, 0x08}, 13)},
		{"NewOctetString", mms.TypeSpec{Type: mms.ValueTypeOctetString, Size: 8}, mms.NewOctetString([]byte{0, 1, 2, 0xff}), mms.NewOctetString([]byte("go-mms"))},
		{"NewVisibleString", vis32, mms.NewVisibleString("hello"), mms.NewVisibleString("world")},
		{"NewMmsString", mms.TypeSpec{Type: mms.ValueTypeMmsString, Size: 32}, mms.NewMmsString("grüße"), mms.NewMmsString("日本")},
		{"NewUTCTime", mms.TypeSpec{Type: mms.ValueTypeUTCTime}, mms.NewUTCTime(t0), mms.NewUTCTime(t0.Add(time.Hour))},
		{"NewUTCTimeWithQuality", mms.TypeSpec{Type: mms.ValueTypeUTCTime}, mms.NewUTCTimeWithQuality(t0, 0x0a), mms.NewUTCTimeWithQuality(t0.Add(time.Minute), 0x8a)},
		{"NewBinaryTime", mms.TypeSpec{Type: mms.ValueTypeBinaryTime}, mms.NewBinaryTime(t0.UnixMilli() + 123), mms.NewBinaryTime(time.Date(1984, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli())},
		{"NewGeneralizedTime", mms.TypeSpec{Type: mms.ValueTypeGeneralizedTime}, mms.NewGeneralizedTime(t0), mms.NewGeneralizedTime(t0.Add(24 * time.Hour))},
		{"NewBCD", mms.TypeSpec{Type: mms.ValueTypeBCD, Size: 8}, mms.NewBCD(1234), mms.NewBCD(98765)},
		{"NewObjectIdentifier", mms.TypeSpec{Type: mms.ValueTypeObjectIdentifier}, mms.NewObjectIdentifier([]int{1, 0, 9506, 2, 1}), mms.NewObjectIdentifier([]int{2, 999, 3})},
		{"NewReal", f64, mms.NewReal(6.25), mms.NewReal(-1e10)},
		{"NewBooleanArray", mms.TypeSpec{Type: mms.ValueTypeBitString, Size: 10}, mms.NewBooleanArray([]byte{0xb0, 0x40}, 10), mms.NewBooleanArray([]byte{0x7f, 0xc0}, 10)},
		{"NewArray", arrayType, intArray(1, 2, 3, 4, 5), intArray(-1, -2, -3, -4, -5)},
		{"NewStructure", recordType, record(7, true, "rec", 10, 20, 30), record(8, false, "updated", 11, 21, 31)},
	}
}

// ---------------------------------------------------------------------------
// Model server
// ---------------------------------------------------------------------------

// model is the server-side state shared by the client tests.
type model struct {
	srv      *mms.Server
	addr     string
	files    *memFiles
	journals *memJournals

	mu     sync.Mutex
	values map[string]*mms.Value // "domain/item" or "/item" for VMD scope
}

func (m *model) get(key string) *mms.Value {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.values[key].Clone()
}

func (m *model) set(key string, v *mms.Value) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.values[key] = v.Clone()
}

// variable registers a variable backed by m.values.
func (m *model) variable(t *testing.T, name mms.ObjectName, ts mms.TypeSpec, initial *mms.Value, writable bool) {
	t.Helper()
	key := string(name.Domain) + "/" + string(name.ItemID)
	m.values[key] = initial.Clone()
	v := mms.Variable{
		Name:     name,
		TypeSpec: ts,
		Read:     func(context.Context) (*mms.Value, error) { return m.get(key), nil },
	}
	if writable {
		v.Write = func(_ context.Context, val *mms.Value) error {
			m.set(key, val)
			return nil
		}
	}
	if err := m.srv.RegisterVariable(v); err != nil {
		t.Fatalf("RegisterVariable %s: %v", key, err)
	}
}

type modelOptions struct {
	maxPDU int
	auth   mms.Authenticator
}

// startModel builds a server with the full object model used by the
// client tests:
//
//	dev/<constructor>   one writable variable per valueCases entry
//	dev/readonly        Integer without a write handler
//	dev/unavailable     Read returns a DataAccessError value
//	dev/static          server-registered, non-deletable NVL
//	vmdvar              VMD-scoped Integer
//	aux/v000..v299      Integers for name-list paging
func startModel(t *testing.T, opts ...modelOptions) *model {
	t.Helper()
	var o modelOptions
	if len(opts) > 0 {
		o = opts[0]
	}
	m := &model{files: newMemFiles(), journals: newMemJournals(), values: map[string]*mms.Value{}}
	m.srv = mms.NewServer(mms.ServerOptions{
		MMS:             mms.ServerMMSOptions{MaxPDUSize: o.maxPDU},
		Authenticate:    o.auth,
		FileProvider:    m.files,
		JournalProvider: m.journals,
	})
	m.srv.HandleIdentify(func(context.Context, mms.IdentifyRequest) (*mms.ServerIdentity, error) {
		return &mms.ServerIdentity{Vendor: "otfabric", Model: "go-mms-e2e", Revision: "1"}, nil
	})
	m.srv.HandleStatus(func(_ context.Context, req mms.StatusRequest) (*mms.ServerStatus, error) {
		if req.ExtendedDerivation {
			return &mms.ServerStatus{Logical: mms.VMDLogicalStatusLimited, Physical: mms.VMDPhysicalStatusNeedsCommissioning}, nil
		}
		return &mms.ServerStatus{Logical: mms.VMDLogicalStatusNoStateChanges, Physical: mms.VMDPhysicalStatusPartiallyOperational}, nil
	})
	for _, d := range []string{dom, auxDom} {
		if err := m.srv.RegisterDomain(d); err != nil {
			t.Fatalf("RegisterDomain %s: %v", d, err)
		}
	}
	for _, vc := range valueCases() {
		m.variable(t, domName(vc.name), vc.ts, vc.initial, true)
	}
	m.variable(t, domName("readonly"), i32, mms.NewInteger(5), false)
	m.variable(t, vmdName("vmdvar"), i32, mms.NewInteger(99), true)
	if err := m.srv.RegisterVariable(mms.Variable{
		Name:     domName("unavailable"),
		TypeSpec: i32,
		Read: func(context.Context) (*mms.Value, error) {
			return mms.NewDataAccessError(mms.DataAccessErrorTemporarilyUnavailable), nil
		},
	}); err != nil {
		t.Fatalf("RegisterVariable unavailable: %v", err)
	}
	for i := range auxCount {
		name := mms.ObjectName{Scope: mms.ObjectScopeDomain, Domain: auxDom, ItemID: mms.ItemID(auxItem(i))}
		m.variable(t, name, i32, mms.NewInteger(int64(i)), true)
	}
	if err := m.srv.RegisterNamedVariableList(mms.NamedVariableList{
		Name:      domName("static"),
		Variables: specs(domName("NewBoolean"), domName("NewInteger")),
	}); err != nil {
		t.Fatalf("RegisterNamedVariableList: %v", err)
	}
	m.addr = serve(t, m.srv)
	return m
}

func auxItem(i int) string {
	return "v" + string(rune('0'+i/100)) + string(rune('0'+i/10%10)) + string(rune('0'+i%10))
}

// ---------------------------------------------------------------------------
// In-memory FileProvider
// ---------------------------------------------------------------------------

type memFile struct {
	data    []byte
	modTime time.Time
}

type memHandle struct {
	name string
	data []byte
	pos  int
}

type memFiles struct {
	mu    sync.Mutex
	files map[string]*memFile
	open  map[*memHandle]bool
}

// pattern returns n bytes of a repeating, position-dependent pattern.
func pattern(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i % 251)
	}
	return b
}

func newMemFiles() *memFiles {
	f := &memFiles{files: map[string]*memFile{}, open: map[*memHandle]bool{}}
	add := func(name string, data []byte, mod time.Time) { f.files[name] = &memFile{data, mod} }
	add("readme.txt", []byte("go-mms end-to-end\n"), t0)
	add("data/small.bin", pattern(1000), t0.Add(time.Hour))
	add("data/large.bin", pattern(200_000), t0.Add(2*time.Hour))
	add("delete.me", []byte("bye"), t0)
	add("rename.me", []byte("renamed"), t0)
	for i := range 60 {
		add("many/file-"+auxItem(i)+".dat", []byte{byte(i)}, t0)
	}
	return f
}

func (f *memFiles) List(_ context.Context, req mms.FileListRequest) (*mms.FileListResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	spec := strings.TrimSuffix(req.FileSpec, "/")
	var names []string
	for name := range f.files {
		if spec == "" || name == spec || strings.HasPrefix(name, spec+"/") {
			names = append(names, name)
		}
	}
	if spec != "" && len(names) == 0 {
		return nil, fs.ErrNotExist
	}
	sort.Strings(names)
	res := &mms.FileListResult{}
	for _, name := range names {
		if req.ContinueAfter != "" && name <= req.ContinueAfter {
			continue
		}
		if req.MaxEntries > 0 && len(res.Entries) == req.MaxEntries {
			res.MoreFollows = true
			break
		}
		file := f.files[name]
		res.Entries = append(res.Entries, mms.FileEntry{Name: name, Size: int64(len(file.data)), LastModified: file.modTime})
	}
	return res, nil
}

func (f *memFiles) Open(_ context.Context, path string) (mms.FileHandle, mms.FileAttributes, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	file, ok := f.files[path]
	if !ok {
		return nil, mms.FileAttributes{}, fs.ErrNotExist
	}
	h := &memHandle{name: path, data: file.data}
	f.open[h] = true
	return h, mms.FileAttributes{Size: int64(len(file.data)), LastModified: file.modTime}, nil
}

func (f *memFiles) Read(_ context.Context, handle mms.FileHandle, maxBytes int) ([]byte, bool, error) {
	h := handle.(*memHandle)
	n := min(maxBytes, len(h.data)-h.pos)
	chunk := h.data[h.pos : h.pos+n]
	h.pos += n
	return chunk, h.pos < len(h.data), nil
}

func (f *memFiles) Close(_ context.Context, handle mms.FileHandle) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.open, handle.(*memHandle))
	return nil
}

func (f *memFiles) openCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.open)
}

func (f *memFiles) has(name string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.files[name]
	return ok
}

func (f *memFiles) Delete(_ context.Context, path string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.files[path]; !ok {
		return fs.ErrNotExist
	}
	if path == "readme.txt" {
		return mms.ErrFileAccessDenied
	}
	delete(f.files, path)
	return nil
}

func (f *memFiles) Rename(_ context.Context, cur, next string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	file, ok := f.files[cur]
	if !ok {
		return fs.ErrNotExist
	}
	delete(f.files, cur)
	f.files[next] = file
	return nil
}

func (f *memFiles) ObtainFile(_ context.Context, src, dst string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	file, ok := f.files[src]
	if !ok {
		return fs.ErrNotExist
	}
	f.files[dst] = &memFile{data: bytes.Clone(file.data), modTime: file.modTime}
	return nil
}

// deviceStore serves fixed directory listings keyed by FileSpec, shaped
// like the file stores of real devices.
type deviceStore struct {
	*memFiles
	listings map[string][]mms.FileEntry
}

func (d deviceStore) List(_ context.Context, req mms.FileListRequest) (*mms.FileListResult, error) {
	entries, ok := d.listings[req.FileSpec]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return &mms.FileListResult{Entries: entries}, nil
}

// ---------------------------------------------------------------------------
// In-memory JournalProvider: dev/log holds journalSize entries, one per
// tick from t0, entry i carrying "seq"=i and "label"=<padding>.
// ---------------------------------------------------------------------------

const journalSize = 400

type memJournals struct{ entries []mms.JournalEntry }

func newMemJournals() *memJournals {
	j := &memJournals{}
	for i := range journalSize {
		j.entries = append(j.entries, mms.JournalEntry{
			EntryID:        []byte{byte(i >> 8), byte(i)},
			OccurrenceTime: t0.Add(time.Duration(i) * tick),
			Variables: []mms.JournalVariable{
				{Tag: "seq", Value: mms.NewInteger(int64(i))},
				{Tag: "label", Value: mms.NewVisibleString(strings.Repeat("x", 40))},
			},
		})
	}
	return j
}

func (j *memJournals) ListJournals(_ context.Context, domain string) ([]string, error) {
	if domain != dom {
		return nil, nil
	}
	return []string{journal}, nil
}

func (j *memJournals) lookup(domain, name string) error {
	if domain != dom || name != journal {
		return fs.ErrNotExist
	}
	return nil
}

func page(entries []mms.JournalEntry, maxEntries int) *mms.JournalResult {
	if maxEntries > 0 && len(entries) > maxEntries {
		return &mms.JournalResult{Entries: entries[:maxEntries], MoreFollows: true}
	}
	return &mms.JournalResult{Entries: entries}
}

func (j *memJournals) ReadTimeRange(_ context.Context, domain, name string, start, stop time.Time, maxEntries int) (*mms.JournalResult, error) {
	if err := j.lookup(domain, name); err != nil {
		return nil, err
	}
	var out []mms.JournalEntry
	for _, e := range j.entries {
		if !e.OccurrenceTime.Before(start) && !e.OccurrenceTime.After(stop) {
			out = append(out, e)
		}
	}
	return page(out, maxEntries), nil
}

func (j *memJournals) ReadStartAfter(_ context.Context, domain, name string, afterID []byte, _ time.Time, maxEntries int) (*mms.JournalResult, error) {
	if err := j.lookup(domain, name); err != nil {
		return nil, err
	}
	for i, e := range j.entries {
		if bytes.Equal(e.EntryID, afterID) {
			return page(j.entries[i+1:], maxEntries), nil
		}
	}
	return nil, fs.ErrNotExist
}

// rawRecorder collects RawHook frames so tests can observe PDUs the client
// API does not surface.
type rawRecorder struct {
	mu   sync.Mutex
	recv [][]byte
}

func (r *rawRecorder) hook(direction string, raw []byte) {
	if direction != "recv" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.recv = append(r.recv, bytes.Clone(raw))
}

func (r *rawRecorder) received() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.recv)
}
