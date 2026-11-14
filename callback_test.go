// SPDX-License-Identifier: MIT

package mms

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

// raised is the value the callbacks of these tests raise.
const raised = "callback value"

// raisingFiles is a FileProvider whose method named fail does not return
// normally.
type raisingFiles struct {
	*memFileProvider
	fail string
}

func (p *raisingFiles) check(method string) {
	if p.fail == method {
		panic(raised)
	}
}

func (p *raisingFiles) List(ctx context.Context, req FileListRequest) (*FileListResult, error) {
	p.check("List")
	return p.memFileProvider.List(ctx, req)
}

func (p *raisingFiles) Open(ctx context.Context, path string) (FileHandle, FileAttributes, error) {
	p.check("Open")
	return p.memFileProvider.Open(ctx, path)
}

func (p *raisingFiles) Read(ctx context.Context, h FileHandle, n int) ([]byte, bool, error) {
	p.check("Read")
	return p.memFileProvider.Read(ctx, h, n)
}

func (p *raisingFiles) Close(ctx context.Context, h FileHandle) error {
	p.check("Close")
	return p.memFileProvider.Close(ctx, h)
}

func (p *raisingFiles) Delete(ctx context.Context, path string) error {
	p.check("Delete")
	return p.memFileProvider.Delete(ctx, path)
}

func (p *raisingFiles) Rename(ctx context.Context, from, to string) error {
	p.check("Rename")
	return p.memFileProvider.Rename(ctx, from, to)
}

func (p *raisingFiles) ObtainFile(ctx context.Context, from, to string) error {
	p.check("ObtainFile")
	return p.memFileProvider.ObtainFile(ctx, from, to)
}

// raisingJournals is a JournalProvider whose method named fail does not
// return normally.
type raisingJournals struct {
	*memJournalProvider
	fail string
}

func (p *raisingJournals) ListJournals(ctx context.Context, domain string) ([]string, error) {
	if p.fail == "ListJournals" {
		panic(raised)
	}
	return p.memJournalProvider.ListJournals(ctx, domain)
}

func (p *raisingJournals) ReadTimeRange(ctx context.Context, domain, journal string, start, stop time.Time, n int) (*JournalResult, error) {
	if p.fail == "ReadTimeRange" {
		panic(raised)
	}
	return p.memJournalProvider.ReadTimeRange(ctx, domain, journal, start, stop, n)
}

func (p *raisingJournals) ReadStartAfter(ctx context.Context, domain, journal string, id []byte, after time.Time, n int) (*JournalResult, error) {
	if p.fail == "ReadStartAfter" {
		panic(raised)
	}
	return p.memJournalProvider.ReadStartAfter(ctx, domain, journal, id, after, n)
}

// raisingQueryJournals adds a JournalQueryProvider.ReadJournal that does
// not return normally.
type raisingQueryJournals struct{ *raisingJournals }

func (raisingQueryJournals) ReadJournal(context.Context, JournalQuery) (*JournalResult, error) {
	panic(raised)
}

// operationalProblem reports whether err is the ServiceError vmd-state
// vmd-operational-problem that answers a request whose callback did not
// return normally.
func operationalProblem(err error) bool {
	se, ok := errors.AsType[*ServiceError](err)
	return ok && se.Class == ErrorClassVMDState && se.Code == 2
}

// logValue is v as the text handler writes it: quoted when it has a space.
func logValue(v string) string {
	if strings.Contains(v, " ") {
		return strconv.Quote(v)
	}
	return v
}

// checkCallbackLog checks the Error record of a callback that did not
// return normally.
func checkCallbackLog(t *testing.T, logs *lockedBuffer, callback string) {
	t.Helper()
	s := logs.String()
	for _, want := range []string{
		`level=ERROR msg="mms: callback did not return normally"`,
		"callback=" + logValue(callback),
		fmt.Sprintf("value=%q", raised),
		"stack=",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("log lacks %s: %q", want, s)
		}
	}
}

// A handler, callback or provider method that does not return normally
// while the server serves a request is answered with ServiceError
// vmd-state vmd-operational-problem; the failure is logged with its stack
// and reported to Metrics, and the association goes on.
func TestServerCallbackNotReturningNormally(t *testing.T) {
	open := func(ctx context.Context, c *Client) (int32, error) {
		res, err := c.FileOpen(ctx, "f")
		if err != nil {
			return 0, err
		}
		return res.FrsmID, nil
	}
	for _, tc := range []struct {
		svc     Service
		fail    string // the raising method of the providers
		journal bool   // the journal provider implements JournalQueryProvider
		call    func(context.Context, *Client) error
	}{
		{svc: ServiceIdentify, call: func(ctx context.Context, c *Client) error { _, err := c.Identify(ctx); return err }},
		{svc: ServiceStatus, call: func(ctx context.Context, c *Client) error { _, err := c.Status(ctx); return err }},
		{svc: ServiceRead, call: func(ctx context.Context, c *Client) error {
			_, err := c.Read(ctx, ReadRequest{DomainID: "LD", ItemID: "r"})
			return err
		}},
		{svc: ServiceWrite, call: func(ctx context.Context, c *Client) error {
			_, err := c.Write(ctx, WriteRequest{DomainID: "LD", ItemID: "w", Value: NewInteger(1)})
			return err
		}},
		{svc: ServiceFileDirectory, fail: "List", call: func(ctx context.Context, c *Client) error {
			_, err := c.FileDirectory(ctx, FileDirectoryRequest{})
			return err
		}},
		{svc: ServiceFileOpen, fail: "Open", call: func(ctx context.Context, c *Client) error { _, err := open(ctx, c); return err }},
		{svc: ServiceFileRead, fail: "Read", call: func(ctx context.Context, c *Client) error {
			id, err := open(ctx, c)
			if err != nil {
				return fmt.Errorf("open: %w", err)
			}
			_, err = c.FileRead(ctx, id)
			return err
		}},
		{svc: ServiceFileClose, fail: "Close", call: func(ctx context.Context, c *Client) error {
			id, err := open(ctx, c)
			if err != nil {
				return fmt.Errorf("open: %w", err)
			}
			return c.FileClose(ctx, id)
		}},
		{svc: ServiceFileDelete, fail: "Delete", call: func(ctx context.Context, c *Client) error { return c.FileDelete(ctx, "f") }},
		{svc: ServiceFileRename, fail: "Rename", call: func(ctx context.Context, c *Client) error { return c.FileRename(ctx, "f", "g") }},
		{svc: ServiceObtainFile, fail: "ObtainFile", call: func(ctx context.Context, c *Client) error { return c.ObtainFile(ctx, "f", "g") }},
		{svc: ServiceGetNameList, fail: "ListJournals", call: func(ctx context.Context, c *Client) error {
			_, err := c.GetNameList(ctx, NameListRequest{ObjectClass: ObjectClassJournal, Scope: ObjectScopeDomain, DomainID: "LD"})
			return err
		}},
		{svc: ServiceReadJournal, fail: "ReadTimeRange", call: func(ctx context.Context, c *Client) error {
			_, err := c.ReadJournalTimeRange(ctx, "LD", "J", time.Time{}, time.Now())
			return err
		}},
		{svc: ServiceReadJournal, fail: "ReadStartAfter", call: func(ctx context.Context, c *Client) error {
			_, err := c.ReadJournalStartAfter(ctx, "LD", "J", time.Now(), []byte{1})
			return err
		}},
		{svc: ServiceReadJournal, fail: "ReadJournal", journal: true, call: func(ctx context.Context, c *Client) error {
			_, err := c.ReadJournalTimeRange(ctx, "LD", "J", time.Time{}, time.Now())
			return err
		}},
	} {
		name := tc.svc.String()
		if tc.fail != "" {
			name += "/" + tc.fail
		}
		t.Run(name, func(t *testing.T) {
			files := &raisingFiles{memFileProvider: newMemFileProvider(), fail: tc.fail}
			files.addFile("f", []byte("data"))
			journals := &raisingJournals{memJournalProvider: newMemJournalProvider(), fail: tc.fail}
			journals.addJournal("LD", "J")
			var jp JournalProvider = journals
			if tc.journal {
				jp = raisingQueryJournals{journals}
			}
			srv, ev, logs := limitServer(t, ServerOptions{FileProvider: files, JournalProvider: jp})
			srv.HandleIdentify(func(context.Context, IdentifyRequest) (*ServerIdentity, error) { panic(raised) })
			srv.HandleStatus(func(context.Context, StatusRequest) (*ServerStatus, error) { panic(raised) })
			for _, v := range []Variable{
				{Name: ObjectName{Scope: ObjectScopeDomain, Domain: "LD", ItemID: "r"}, TypeSpec: TypeSpec{Type: ValueTypeInteger, Size: 32},
					Read: func(context.Context) (*Value, error) { panic(raised) }},
				{Name: ObjectName{Scope: ObjectScopeDomain, Domain: "LD", ItemID: "w"}, TypeSpec: TypeSpec{Type: ValueTypeInteger, Size: 32},
					Read: func(context.Context) (*Value, error) { return NewInteger(0), nil }, Write: func(context.Context, *Value) error { panic(raised) }},
			} {
				if err := srv.RegisterVariable(v); err != nil {
					t.Fatal(err)
				}
			}
			c, served := serveLoopback(t, srv, DialOptions{})
			ctx := ctx5s(t)

			if err := tc.call(ctx, c); !operationalProblem(err) {
				t.Fatalf("%s = %v, want ServiceError vmd-state vmd-operational-problem", name, err)
			}
			checkCallbackLog(t, logs, "handler of "+tc.svc.String())
			if !strings.Contains(logs.String(), "service="+tc.svc.String()) || !strings.Contains(logs.String(), "invoke_id=") {
				t.Fatalf("log lacks the request: %q", logs.String())
			}
			failed := 0
			ev.mu.Lock()
			for _, r := range ev.requests {
				if errors.Is(r.Err, ErrCallbackFailed) && r.Service == tc.svc {
					failed++
				}
			}
			ev.mu.Unlock()
			if failed != 1 {
				t.Fatalf("%d request events match ErrCallbackFailed, want 1", failed)
			}
			// The association goes on.
			if _, err := c.GetNameList(ctx, NameListRequest{ObjectClass: ObjectClassDomain, Scope: ObjectScopeVMD}); err != nil {
				t.Fatalf("GetNameList after the failure: %v", err)
			}
			if err := c.Close(ctx); err != nil {
				t.Fatalf("Close: %v", err)
			}
			if err := waitServe(t, served); err != nil {
				t.Fatalf("Serve = %v", err)
			}
		})
	}
}

// An Authenticator that does not return normally refuses the association
// as an error does; Serve's error matches ErrAuthenticationFailed and
// ErrCallbackFailed, and the next association is decided as usual.
func TestAuthenticatorNotReturningNormally(t *testing.T) {
	var calls atomic.Int32
	var logs lockedBuffer
	srv := NewServer(ServerOptions{Logger: logs.logger(), Authenticate: func(context.Context, *AuthContext) (AuthResult, error) {
		if calls.Add(1) == 1 {
			panic(raised)
		}
		return AuthResult{Accept: true}, nil
	}})
	_, serveErr, err := dialServer(t, srv, DialOptions{})
	if _, ok := errors.AsType[*RejectionError](err); !ok {
		t.Fatalf("NewClient = %v, want a RejectionError", err)
	}
	serr := waitServe(t, serveErr)
	if _, ok := errors.AsType[*AuthenticationError](serr); !ok || !errors.Is(serr, ErrAuthenticationFailed) || !errors.Is(serr, ErrCallbackFailed) {
		t.Fatalf("Serve = %v, want an AuthenticationError matching ErrCallbackFailed", serr)
	}
	checkCallbackLog(t, &logs, "Authenticator")
	c, _, err := dialServer(t, srv, DialOptions{})
	if err != nil {
		t.Fatalf("next association: %v", err)
	}
	_ = c.Abort(context.Background())
}

// raisingMetrics is a Metrics whose methods do not return normally.
type raisingMetrics struct{}

func (raisingMetrics) ObserveRequest(RequestEvent)       { panic(raised) }
func (raisingMetrics) ObserveConnection(ConnectionEvent) { panic(raised) }

// Metrics methods that do not return normally are logged; the requests
// and associations they report go on, on both sides.
func TestMetricsNotReturningNormally(t *testing.T) {
	var srvLogs, cliLogs lockedBuffer
	srv := NewServer(ServerOptions{Logger: srvLogs.logger(), Metrics: raisingMetrics{}})
	if err := srv.RegisterDomain("LD"); err != nil {
		t.Fatal(err)
	}
	c, served := serveLoopback(t, srv, DialOptions{Logger: cliLogs.logger(), Metrics: raisingMetrics{}})
	ctx := ctx5s(t)
	names, err := c.GetNameList(ctx, NameListRequest{ObjectClass: ObjectClassDomain, Scope: ObjectScopeVMD})
	if err != nil || len(names.Names) != 1 {
		t.Fatalf("GetNameList = %v, %v", names, err)
	}
	if err := c.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := waitServe(t, served); err != nil {
		t.Fatalf("Serve = %v", err)
	}
	for _, logs := range []*lockedBuffer{&srvLogs, &cliLogs} {
		checkCallbackLog(t, logs, "Metrics.ObserveRequest")
		checkCallbackLog(t, logs, "Metrics.ObserveConnection")
	}
}

// A FileProvider.Close that does not return normally when the association
// ends is logged, and the other files are still closed.
func TestFileCloseAtEndNotReturningNormally(t *testing.T) {
	closed := make(chan FileHandle, 2)
	files := &closeRecorder{raisingFiles: &raisingFiles{memFileProvider: newMemFileProvider()}, closed: closed}
	files.addFile("f", []byte("data"))
	srv, _, logs := limitServer(t, ServerOptions{FileProvider: files})
	c, served := serveLoopback(t, srv, DialOptions{})
	ctx := ctx5s(t)
	for range 2 {
		if _, err := c.FileOpen(ctx, "f"); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.Abort(ctx); err != nil {
		t.Fatal(err)
	}
	_ = waitServe(t, served)
	if len(closed) != 2 {
		t.Fatalf("%d files closed, want 2", len(closed))
	}
	checkCallbackLog(t, logs, "FileProvider.Close")
}

// closeRecorder records each Close; the first does not return normally.
type closeRecorder struct {
	*raisingFiles
	closed chan FileHandle
}

func (p *closeRecorder) Close(_ context.Context, h FileHandle) error {
	p.closed <- h
	if len(p.closed) == 1 {
		panic(raised)
	}
	return nil
}

// A RawHook that does not return normally is logged; the PDU is sent or
// processed as usual.
func TestRawHookNotReturningNormally(t *testing.T) {
	for _, direction := range []string{RawHookSend, RawHookRecv} {
		t.Run(direction, func(t *testing.T) {
			var logs lockedBuffer
			srv := NewServer(ServerOptions{})
			if err := srv.RegisterDomain("LD"); err != nil {
				t.Fatal(err)
			}
			c, _ := serveLoopback(t, srv, DialOptions{Logger: logs.logger(), RawHook: func(d string, _ []byte) {
				if d == direction {
					panic(raised)
				}
			}})
			defer func() { _ = c.Abort(context.Background()) }()
			names, err := c.GetNameList(ctx5s(t), NameListRequest{ObjectClass: ObjectClassDomain, Scope: ObjectScopeVMD})
			if err != nil || len(names.Names) != 1 {
				t.Fatalf("GetNameList = %v, %v", names, err)
			}
			checkCallbackLog(t, &logs, "RawHook")
		})
	}
}

// An InformationReportHandler that does not return normally is logged with
// its stack, inline and from the report queue, and the next report is
// delivered.
func TestReportHandlerNotReturningNormally(t *testing.T) {
	for _, length := range []int{0, 4} {
		t.Run(fmt.Sprintf("queue %d", length), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				var logs lockedBuffer
				srv := NewServer(ServerOptions{})
				c, _ := serveLoopback(t, srv, DialOptions{Logger: logs.logger(), ReportQueue: ReportQueueOptions{Length: length}})
				defer func() { _ = c.Abort(context.Background()) }()
				var n atomic.Int32
				c.OnInformationReport(func(*InformationReportIndication) {
					if n.Add(1) == 1 {
						panic(raised)
					}
				})
				synctest.Wait()
				sendReport(t, srv)
				sendReport(t, srv)
				synctest.Wait()
				if n.Load() != 2 {
					t.Fatalf("%d reports delivered, want 2", n.Load())
				}
				checkCallbackLog(t, &logs, "InformationReportHandler")
			})
		})
	}
}

// dropRecorder records ObserveReportDropped events.
type dropRecorder struct {
	NopMetrics
	events []ReportDroppedEvent
}

func (d *dropRecorder) ObserveReportDropped(ev ReportDroppedEvent) { d.events = append(d.events, ev) }

// raisingDrops is a ReportDropObserver that does not return normally.
type raisingDrops struct{ NopMetrics }

func (raisingDrops) ObserveReportDropped(ReportDroppedEvent) { panic(raised) }

// guardedMetrics passes ObserveReportDropped to a Metrics that implements
// ReportDropObserver, logs one that does not return normally, and drops
// the event for a Metrics without it.
func TestGuardedMetricsReportDropped(t *testing.T) {
	var logs lockedBuffer
	rec := &dropRecorder{}
	ev := ReportDroppedEvent{RemoteAddr: "a", Full: ReportDropOldest, Dropped: 3}
	guardMetrics(rec, logs.logger()).(ReportDropObserver).ObserveReportDropped(ev)
	if len(rec.events) != 1 || rec.events[0] != ev {
		t.Fatalf("events = %+v, want %+v", rec.events, ev)
	}
	guardMetrics(raisingDrops{}, logs.logger()).(ReportDropObserver).ObserveReportDropped(ev)
	checkCallbackLog(t, &logs, "ReportDropObserver.ObserveReportDropped")
	guardMetrics(raisingMetrics{}, logs.logger()).(ReportDropObserver).ObserveReportDropped(ev)
	if _, ok := guardMetrics(nil, logs.logger()).(NopMetrics); !ok {
		t.Error("guardMetrics(nil) is not NopMetrics")
	}
}
