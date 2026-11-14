// SPDX-License-Identifier: MIT

package mms

import (
	"fmt"
	"log/slog"
	"runtime/debug"
)

// callbackError is the error reported for an application callback that
// did not return normally. It matches [ErrCallbackFailed].
type callbackError struct {
	callback string
	value    any
}

func (e *callbackError) Error() string {
	return fmt.Sprintf("mms: %s did not return normally: %v", e.callback, e.value)
}

func (e *callbackError) Unwrap() error { return ErrCallbackFailed }

// callbackFailed logs a callback that did not return normally, with the
// value it raised and the stack of the goroutine, and returns the error
// that reports it. attrs are added to the log record.
func callbackFailed(logger *slog.Logger, callback string, value any, attrs ...any) error {
	attrs = append(attrs, "callback", callback, "value", value, "stack", string(debug.Stack()))
	logger.Error("mms: callback did not return normally", attrs...)
	return &callbackError{callback: callback, value: value}
}

// guard runs fn, which calls the application callback named callback.
// When fn does not return normally, guard recovers it, logs it with its
// stack and returns an error matching [ErrCallbackFailed]; otherwise it
// returns nil.
func guard(logger *slog.Logger, callback string, fn func()) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = callbackFailed(logger, callback, r)
		}
	}()
	fn()
	return nil
}

// guardedMetrics calls the application's [Metrics] through guard: a
// method that does not return normally is logged and the event dropped;
// the operation it reports goes on.
type guardedMetrics struct {
	m      Metrics
	logger *slog.Logger
}

// guardMetrics returns m called through guard, or NopMetrics for nil.
func guardMetrics(m Metrics, logger *slog.Logger) Metrics {
	if m == nil {
		return NopMetrics{}
	}
	return guardedMetrics{m: m, logger: logger}
}

func (g guardedMetrics) ObserveRequest(ev RequestEvent) {
	_ = guard(g.logger, "Metrics.ObserveRequest", func() { g.m.ObserveRequest(ev) })
}

func (g guardedMetrics) ObserveConnection(ev ConnectionEvent) {
	_ = guard(g.logger, "Metrics.ObserveConnection", func() { g.m.ObserveConnection(ev) })
}

func (g guardedMetrics) ObserveReportDropped(ev ReportDroppedEvent) {
	if o, ok := g.m.(ReportDropObserver); ok {
		_ = guard(g.logger, "ReportDropObserver.ObserveReportDropped", func() { o.ObserveReportDropped(ev) })
	}
}
