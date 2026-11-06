package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

// startServe runs serve with handler on a random port and returns its URL,
// a cancel function that plays the role of SIGTERM, and serve's result.
func startServe(t *testing.T, handler http.HandlerFunc, drain, grace time.Duration) (string, context.CancelFunc, <-chan error) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- serve(ctx, &http.Server{Handler: handler}, ln, drain, grace) }()
	return "http://" + ln.Addr().String(), cancel, done
}

func TestServeDrainsInFlightRequests(t *testing.T) {
	started := make(chan struct{})
	url, sigterm, done := startServe(t, func(w http.ResponseWriter, r *http.Request) {
		close(started)
		time.Sleep(300 * time.Millisecond) // a request that's mid-flight at shutdown
		io.WriteString(w, "finished")
	}, 5*time.Second, time.Second)

	res := make(chan string, 1)
	go func() {
		r, err := http.Get(url)
		if err != nil {
			res <- "error: " + err.Error()
			return
		}
		b, _ := io.ReadAll(r.Body)
		r.Body.Close()
		res <- string(b)
	}()
	<-started
	sigterm()

	if got := <-res; got != "finished" {
		t.Errorf("in-flight request got %q, want it to finish", got)
	}
	if err := <-done; err != nil {
		t.Errorf("serve: %v", err)
	}
	if _, err := http.Get(url); err == nil {
		t.Error("server still accepting connections after shutdown")
	}
}

func TestServeCancelsRequestsThatOutlastTheDrain(t *testing.T) {
	started := make(chan struct{})
	cancelled := make(chan struct{})
	url, sigterm, done := startServe(t, func(w http.ResponseWriter, r *http.Request) {
		close(started)
		select {
		case <-r.Context().Done(): // like an S3 upload seeing its context end
			close(cancelled)
		case <-time.After(10 * time.Second):
		}
	}, 200*time.Millisecond, 2*time.Second)

	go http.Get(url)
	<-started
	start := time.Now()
	sigterm()

	select {
	case <-cancelled:
	case <-time.After(3 * time.Second):
		t.Fatal("long request's context was never cancelled")
	}
	if err := <-done; err != nil {
		t.Errorf("serve: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("shutdown took %s, want about the 200ms drain", elapsed)
	}
}
