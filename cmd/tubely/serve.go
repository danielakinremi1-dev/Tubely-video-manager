package main

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"time"
)

// How long a shutdown waits for in-flight requests, then how long it gives
// the stragglers to abort cleanly once their contexts are cancelled.
// Container platforms usually allow about 30s between SIGTERM and SIGKILL.
const (
	shutdownTimeout = 20 * time.Second
	abortGrace      = 5 * time.Second
)

// serve runs srv on ln until ctx is cancelled (SIGINT/SIGTERM), then shuts
// down gracefully:
//  1. stop accepting connections and let in-flight requests finish, for up
//     to drain;
//  2. if some are still running (say, a long upload), cancel their request
//     contexts so they abort cleanly: S3 multipart uploads are aborted and
//     temp files removed by their defers. Wait up to grace for that.
func serve(ctx context.Context, srv *http.Server, ln net.Listener, drain, grace time.Duration) error {
	baseCtx, cancelRequests := context.WithCancel(context.Background())
	defer cancelRequests()
	srv.BaseContext = func(net.Listener) context.Context { return baseCtx }

	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()

	select {
	case err := <-errc:
		return err // the listener failed; nothing to drain
	case <-ctx.Done():
	}

	log.Printf("shutting down: waiting up to %s for in-flight requests", drain)
	drainCtx, cancel := context.WithTimeout(context.Background(), drain)
	defer cancel()
	err := srv.Shutdown(drainCtx)
	if errors.Is(err, context.DeadlineExceeded) {
		log.Printf("requests still running after %s; cancelling them", drain)
		cancelRequests()
		graceCtx, cancel := context.WithTimeout(context.Background(), grace)
		defer cancel()
		err = srv.Shutdown(graceCtx)
	}
	if serveErr := <-errc; !errors.Is(serveErr, http.ErrServerClosed) {
		return serveErr
	}
	return err
}
