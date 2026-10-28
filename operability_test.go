// SPDX-License-Identifier: MIT

package mms

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/otfabric/go-mms/internal/codec"
	"github.com/otfabric/go-mms/internal/pdu"
)

func TestClassify(t *testing.T) {
	if classify(nil, ErrClosed) != nil {
		t.Error("classify(nil) != nil")
	}
	base := fmt.Errorf("mms: x: %w", context.Canceled)
	err := classify(base, ErrClosed, ErrProtocol)
	if err.Error() != base.Error() {
		t.Errorf("message changed: %q", err)
	}
	for _, target := range []error{ErrClosed, ErrProtocol, context.Canceled} {
		if !errors.Is(err, target) {
			t.Errorf("classified error does not match %v", target)
		}
	}
	// Already-matching kinds are not added again.
	if again := classify(err, ErrClosed); again != err {
		t.Error("classify re-wrapped an error that already matches")
	}
	if !errors.Is(invalidArgf("a %d", 1), ErrInvalidArgument) ||
		!errors.Is(decodeErrorf("b"), ErrDecodeFailed) ||
		!errors.Is(invalidPDUf("c"), ErrInvalidPDU) {
		t.Error("helper does not wrap its sentinel")
	}
}

func TestServiceError_CodeName(t *testing.T) {
	cases := []struct {
		se   ServiceError
		want string
	}{
		{ServiceError{Class: ErrorClassAccess, Code: 2}, "object-non-existent"},
		{ServiceError{Class: ErrorClassFile, Code: 9}, "insufficient-space-in-filestore"},
		{ServiceError{Class: ErrorClassVMDState, Code: 0}, "other"},
		{ServiceError{Class: ErrorClassAccess, Code: 99}, ""},
		{ServiceError{Class: ErrorClassAccess, Code: -1}, ""},
		{ServiceError{Class: ErrorClassOthers, Code: 3}, ""},
		{ServiceError{Class: ErrorClass(42), Code: 0}, ""},
	}
	for _, c := range cases {
		if got := c.se.CodeName(); got != c.want {
			t.Errorf("%v/%d: CodeName = %q, want %q", c.se.Class, c.se.Code, got, c.want)
		}
	}
	named := &ServiceError{Class: ErrorClassAccess, Code: 2, InvokeID: 7}
	if !strings.Contains(named.Error(), "(object-non-existent)") {
		t.Errorf("Error() = %q", named)
	}
	unnamed := &ServiceError{Class: ErrorClassOthers, Code: 3}
	if strings.Contains(unnamed.Error(), "(") {
		t.Errorf("Error() = %q", unnamed)
	}
	// Every class except Others has names.
	for c := ErrorClassVMDState; c < ErrorClassOthers; c++ {
		if (&ServiceError{Class: c}).CodeName() != "other" {
			t.Errorf("class %v has no code names", c)
		}
	}
}

func TestRejectError(t *testing.T) {
	re := &RejectError{Type: RejectConfirmedRequest, Reason: 1, InvokeID: 4, HasInvokeID: true}
	var pe *ProtocolError
	if !errors.As(re, &pe) || pe.Phase != PhaseMMS || !strings.Contains(pe.Message, "type=1 reason=1 invokeID=4") {
		t.Fatalf("RejectError does not unwrap to the legacy ProtocolError: %v", pe)
	}
	if !errors.Is(re, ErrProtocol) {
		t.Error("RejectError does not match ErrProtocol")
	}
	if !strings.Contains(re.Error(), "confirmed-requestPDU") {
		t.Errorf("Error() = %q", re)
	}
	noID := &RejectError{Type: RejectPDUError, Reason: 2}
	if !strings.Contains(noID.Error(), "invokeID=absent") {
		t.Errorf("Error() = %q", noID)
	}
	for typ := RejectConfirmedRequest; typ <= RejectConcludeError; typ++ {
		if strings.HasPrefix(typ.String(), "RejectType(") {
			t.Errorf("%d has no name", int(typ))
		}
	}
	if RejectType(0).String() != "RejectType(0)" || RejectType(12).String() != "RejectType(12)" {
		t.Error("unknown RejectType names")
	}
}

func TestClient_HandleReject_Typed(t *testing.T) {
	c := &Client{}
	content := codec.MarshalRejectPDU(9, 1, 1)
	kind, inner, err := pdu.DecodePdu(content)
	if err != nil || kind != pdu.PduReject {
		t.Fatalf("decode reject: %v %v", kind, err)
	}
	err = c.handleReject(9, inner)
	var re *RejectError
	if !errors.As(err, &re) || re.Type != RejectConfirmedRequest || re.Reason != 1 || re.InvokeID != 9 || !re.HasInvokeID {
		t.Fatalf("handleReject = %#v", err)
	}
}

func TestEnumStrings(t *testing.T) {
	for _, c := range []struct{ got, want string }{
		{RoleClient.String(), "client"},
		{RoleServer.String(), "server"},
		{Role(5).String(), "Role(5)"},
		{ConnectionEstablished.String(), "established"},
		{ConnectionRejected.String(), "rejected"},
		{ConnectionClosed.String(), "closed"},
		{ConnectionState(9).String(), "ConnectionState(9)"},
		{ServiceRead.String(), "Read"},
		{ServiceGetVariableAccessAttributes.String(), "GetVariableAccessAttributes"},
		{ServiceReadJournal.String(), "ReadJournal"},
		{ServiceUnknown.String(), "Unknown"},
		{Service(-1).String(), "Service(-1)"},
		{Service(100).String(), "Service(100)"},
	} {
		if c.got != c.want {
			t.Errorf("String() = %q, want %q", c.got, c.want)
		}
	}
	var m Metrics = NopMetrics{}
	m.ObserveRequest(RequestEvent{})
	m.ObserveConnection(ConnectionEvent{})
	if metricsOrNop(nil) == nil || (&Client{}).metricsSink() == nil {
		t.Error("nil metrics not replaced by NopMetrics")
	}
	if remoteAddrOf(nil) != "" {
		t.Error("remoteAddrOf(nil) != \"\"")
	}
}

func TestRequestService(t *testing.T) {
	req, err := pdu.MarshalIdentifyRequest(3)
	if err != nil {
		t.Fatal(err)
	}
	if got := requestService(req); got != ServiceIdentify {
		t.Errorf("requestService = %v", got)
	}
	if got := requestService([]byte{0xff}); got != ServiceUnknown {
		t.Errorf("garbage: %v", got)
	}
	if got := requestService(codec.MarshalConcludeRequest()); got != ServiceUnknown {
		t.Errorf("conclude: %v", got)
	}
	if got := requestService([]byte{0xa0, 0x03, 0x02, 0x01, 0x01}); got != ServiceUnknown {
		t.Errorf("confirmed request without service: %v", got)
	}
}

func TestContextErr(t *testing.T) {
	err := contextErr(1, context.DeadlineExceeded)
	if !errors.Is(err, ErrInvokeTimeout) || !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("deadline: %v", err)
	}
	err = contextErr(1, context.Canceled)
	if errors.Is(err, ErrInvokeTimeout) || !errors.Is(err, context.Canceled) {
		t.Errorf("cancel: %v", err)
	}
}

func TestRetryPolicy_Backoff(t *testing.T) {
	var zero RetryPolicy
	if got := zero.Backoff(1); got != 100*time.Millisecond {
		t.Errorf("zero policy first backoff = %v", got)
	}
	if got := zero.Backoff(100); got != 30*time.Second {
		t.Errorf("zero policy capped backoff = %v", got)
	}
	p := RetryPolicy{InitialBackoff: time.Second, MaxBackoff: 5 * time.Second, Multiplier: 3}
	for attempt, want := range map[int]time.Duration{1: time.Second, 2: 3 * time.Second, 3: 5 * time.Second, 9: 5 * time.Second} {
		if got := p.Backoff(attempt); got != want {
			t.Errorf("Backoff(%d) = %v, want %v", attempt, got, want)
		}
	}
	p.Jitter = 0.5
	for range 100 {
		d := p.jittered(time.Second)
		if d < 500*time.Millisecond || d > time.Second {
			t.Fatalf("jittered = %v", d)
		}
	}
	p.Jitter = 7 // clamped to 1
	if d := p.jittered(time.Second); d < 0 || d > time.Second {
		t.Fatalf("jittered with clamp = %v", d)
	}
	p.Jitter = -1
	if d := p.jittered(time.Second); d != time.Second {
		t.Errorf("negative jitter = %v", d)
	}
}

func TestIsRetryable(t *testing.T) {
	for err, want := range map[error]bool{
		nil:                      false,
		context.Canceled:         false,
		context.DeadlineExceeded: true,
		ErrInvalidArgument:       false,
		&AuthenticationError{}:   false,
		classify(errors.New("x"), ErrConnectionRejected): false,
		ErrClosed:             true,
		errors.New("refused"): true,
	} {
		if got := IsRetryable(err); got != want {
			t.Errorf("IsRetryable(%v) = %v, want %v", err, got, want)
		}
	}
}

func TestRetry(t *testing.T) {
	ctx := context.Background()
	fail := errors.New("fail")
	fast := RetryPolicy{MaxAttempts: 3, InitialBackoff: time.Microsecond}

	calls := 0
	if err := Retry(ctx, fast, func(context.Context) error { calls++; return fail }); !errors.Is(err, fail) || calls != 3 {
		t.Errorf("exhausted: err=%v calls=%d", err, calls)
	}

	calls = 0
	var retried []int
	p := fast
	p.OnRetry = func(attempt int, err error, _ time.Duration) { retried = append(retried, attempt) }
	err := Retry(ctx, p, func(context.Context) error {
		calls++
		if calls < 2 {
			return fail
		}
		return nil
	})
	if err != nil || calls != 2 || len(retried) != 1 || retried[0] != 1 {
		t.Errorf("success on second: err=%v calls=%d retried=%v", err, calls, retried)
	}

	calls = 0
	if err := Retry(ctx, fast, func(context.Context) error { calls++; return ErrInvalidArgument }); calls != 1 || !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("non-retryable: err=%v calls=%d", err, calls)
	}

	calls = 0
	custom := fast
	custom.Retryable = func(error) bool { return false }
	if _ = Retry(ctx, custom, func(context.Context) error { calls++; return fail }); calls != 1 {
		t.Errorf("custom Retryable: calls=%d", calls)
	}

	calls = 0
	if err := Retry(ctx, RetryPolicy{}, func(context.Context) error { calls++; return fail }); calls != 1 || !errors.Is(err, fail) {
		t.Errorf("zero policy: err=%v calls=%d", err, calls)
	}

	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	calls = 0
	if err := Retry(cancelled, fast, func(context.Context) error { calls++; return nil }); calls != 0 || !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled before start: err=%v calls=%d", err, calls)
	}

	// Unlimited attempts end when the context does, interrupting the wait.
	short, cancel2 := context.WithTimeout(ctx, 30*time.Millisecond)
	defer cancel2()
	slow := RetryPolicy{MaxAttempts: -1, InitialBackoff: time.Hour}
	err = Retry(short, slow, func(context.Context) error { return fail })
	if !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, fail) {
		t.Errorf("unlimited: %v", err)
	}

	def := DefaultRetryPolicy()
	if def.MaxAttempts != 5 || def.Jitter == 0 {
		t.Errorf("DefaultRetryPolicy = %+v", def)
	}
}

func TestValueEqual_Floats(t *testing.T) {
	nan := math.NaN()
	if !NewFloat(nan).Equal(NewFloat(nan)) || !NewReal(nan).Equal(NewReal(-nan)) {
		t.Error("NaN not equal to NaN")
	}
	if NewFloat(0).Equal(NewFloat(math.Copysign(0, -1))) {
		t.Error("+0 equals -0")
	}
	if NewReal(1).Equal(NewReal(nan)) || NewReal(nan).Equal(NewReal(1)) {
		t.Error("NaN equals a number")
	}
}
