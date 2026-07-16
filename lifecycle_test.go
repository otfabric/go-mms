// SPDX-License-Identifier: MIT

package mms

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestCloseDuringInFlightRequest(t *testing.T) {
	srv := testServer(t)
	client := connectClientServer(t, srv)

	errCh := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, err := client.GetNameList(ctx, NameListRequest{
			ObjectClass: ObjectClassNamedVariable,
			Scope:       ObjectScopeDomain,
			DomainID:    "testDomain",
		})
		errCh <- err
	}()

	time.Sleep(10 * time.Millisecond)

	if err := client.Close(context.Background()); err != nil {
		t.Logf("Close: %v", err)
	}

	select {
	case err := <-errCh:
		t.Logf("request result: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("request did not complete after Close")
	}
}

func TestDoubleCloseConcurrent(t *testing.T) {
	srv := testServer(t)
	client := connectClientServer(t, srv)

	var wg sync.WaitGroup
	wg.Add(2)

	for i := 0; i < 2; i++ {
		go func() {
			defer wg.Done()
			client.Close(context.Background())
		}()
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("concurrent Close calls deadlocked")
	}
}

func TestContextCancellationDuringRequest(t *testing.T) {
	srv := testServer(t)
	client := connectClientServer(t, srv)
	defer client.Close(context.Background())

	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		_, err := client.GetNameList(ctx, NameListRequest{
			ObjectClass: ObjectClassNamedVariable,
			Scope:       ObjectScopeDomain,
			DomainID:    "testDomain",
		})
		errCh <- err
	}()

	cancel()

	select {
	case err := <-errCh:
		if err != nil && err != context.Canceled {
			t.Logf("request error (expected Canceled or nil): %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("request did not complete after context cancellation")
	}
}

func TestAbortDuringInFlightRequest(t *testing.T) {
	srv := testServer(t)
	client := connectClientServer(t, srv)

	errCh := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, err := client.GetNameList(ctx, NameListRequest{
			ObjectClass: ObjectClassNamedVariable,
			Scope:       ObjectScopeDomain,
			DomainID:    "testDomain",
		})
		errCh <- err
	}()

	time.Sleep(10 * time.Millisecond)

	if err := client.Abort(context.Background()); err != nil {
		t.Logf("Abort: %v", err)
	}

	select {
	case err := <-errCh:
		t.Logf("request result after abort: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("request did not complete after Abort")
	}
}

func TestCloseWithTimeout(t *testing.T) {
	srv := testServer(t)
	client := connectClientServer(t, srv)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()

	err := client.Close(ctx)
	t.Logf("Close with short timeout: %v", err)
}
