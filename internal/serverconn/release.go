// SPDX-License-Identifier: MIT

package serverconn

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/otfabric/go-mms/internal/codec"
	"github.com/otfabric/go-mms/internal/isostack"
	"github.com/otfabric/go-mms/internal/session"
)

// DefaultReleaseTimeout bounds the wait for the client's A-RELEASE after
// a Conclude-Response when MMSOptions.ReleaseTimeout is zero.
const DefaultReleaseTimeout = 2 * time.Second

// ErrReleaseWithoutConclude reports a FINISH (A-RELEASE) received while
// the MMS association was not concluded. ISO 9506-2 Annex A.1.2.2 makes
// A-RELEASE.indication valid only after Conclude; the server aborts.
var ErrReleaseWithoutConclude = errors.New("serverconn: A-RELEASE without MMS Conclude")

// handleConclude answers a Conclude-Request and completes the orderly
// release (ISO 9506-2 Annex A.1.2.2): Conclude-ResponsePDU over P-DATA,
// then, on A-RELEASE.indication (FINISH + RLRQ), A-RELEASE.response
// (DISCONNECT + RLRE). The wait for the FINISH is bounded by
// ReleaseTimeout. A peer that closes the transport instead (some peers
// end a non-ACSE-release association after the Conclude-Response) ends
// the association normally.
func (c *Conn) handleConclude(ctx context.Context) error {
	resp := codec.MarshalConcludeResponse()
	if err := c.sendData(ctx, resp); err != nil {
		return fmt.Errorf("serverconn: send conclude response: %w", err)
	}
	c.logger.Info("mms: concluded")
	return c.awaitRelease(ctx)
}

func (c *Conn) awaitRelease(ctx context.Context) error {
	timeout := c.mmsOpts.ReleaseTimeout
	if timeout < 0 {
		return nil
	}
	if timeout == 0 {
		timeout = DefaultReleaseTimeout
	}
	rctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	data, err := c.transport.Receive(rctx)
	if err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("serverconn: await release: %w", ctx.Err())
		}
		c.logger.Info("mms: no A-RELEASE after conclude", "error", err)
		return nil
	}
	if sp, perr := session.Parse(data); perr == nil && (sp.Type == session.SpduAbort || sp.Type == session.SpduAbortAccept) {
		c.logger.Warn("mms: peer aborted after conclude", "detail", isostack.DescribeAbort(sp.UserData))
		return nil
	}
	if err := isostack.DecodeReleaseRequestOn(c.contexts, data); err != nil {
		// After the Conclude-Response only A-RELEASE is valid.
		_ = c.SendAbort(ctx)
		return fmt.Errorf("serverconn: expected A-RELEASE after conclude: %w", err)
	}
	c.sendMu.Lock()
	err = c.transport.Send(ctx, isostack.EncodeReleaseResponseOn(c.contexts))
	c.sendMu.Unlock()
	if err != nil {
		return fmt.Errorf("serverconn: send release response: %w", err)
	}
	c.logger.Info("mms: released")
	return nil
}

// handleUnexpectedRelease aborts an association whose peer sent FINISH
// before Conclude (ISO 9506-2 Annex A.1.2.2).
func (c *Conn) handleUnexpectedRelease(ctx context.Context) error {
	c.logger.Warn("mms: A-RELEASE without conclude; aborting")
	if err := c.SendAbort(ctx); err != nil {
		return errors.Join(ErrReleaseWithoutConclude, fmt.Errorf("serverconn: send abort: %w", err))
	}
	return ErrReleaseWithoutConclude
}

func isFinish(data []byte) bool {
	sp, err := session.Parse(data)
	return err == nil && sp.Type == session.SpduFinish
}

// SendAbort sends a user abort to the peer: Session ABORT carrying an
// ARU-PPDU with an ACSE ABRT (source acse-service-user) on the
// association's ACSE context (ISO 8823-1, ISO 8650-1). This is a
// best-effort send: errors are returned but the caller should close the
// transport regardless.
func (c *Conn) SendAbort(ctx context.Context) error {
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	return c.transport.Send(ctx, isostack.EncodeAbortOn(c.contexts))
}
