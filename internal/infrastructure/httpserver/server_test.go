package httpserver_test

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/UrielJaloto/paygo/internal/infrastructure/httpserver"
)

const patience = 5 * time.Second

type running struct {
	url    string
	result <-chan error
}

func serve(ctx context.Context, t *testing.T, handler http.Handler, shutdownTimeout time.Duration) running {
	t.Helper()
	listener, err := httpserver.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	result := make(chan error, 1)
	server := httpserver.New(listener.Addr().String(), handler, time.Second, shutdownTimeout)
	go func() { result <- server.Run(ctx, listener) }()
	return running{url: "http://" + listener.Addr().String(), result: result}
}

func receive[T any](t *testing.T, source <-chan T) T {
	t.Helper()
	select {
	case value := <-source:
		return value
	case <-time.After(patience):
		t.Fatal("timed out waiting on a channel")
		panic("unreachable")
	}
}

func blockingHandler(started chan<- struct{}, release <-chan struct{}) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release
		writer.WriteHeader(http.StatusOK)
	})
}

func TestRun_Request_ServesHandlerBody(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	current := serve(ctx, t, http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(writer, "pong")
	}), patience)

	response, err := http.Get(current.url)

	if err != nil {
		t.Fatalf("GET error = %v", err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	if string(body) != "pong" {
		t.Fatalf("body = %q, want pong", body)
	}
	cancel()
	if err := receive(t, current.result); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
}

func TestRun_CancelledWithRequestInFlight_CompletesRequest(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started, release := make(chan struct{}), make(chan struct{})
	current := serve(ctx, t, blockingHandler(started, release), patience)
	statuses := make(chan int, 1)
	go func() {
		response, err := http.Get(current.url)
		if err != nil {
			statuses <- 0
			return
		}
		defer response.Body.Close()
		statuses <- response.StatusCode
	}()
	receive(t, started)

	cancel()
	close(release)

	if got := receive(t, statuses); got != http.StatusOK {
		t.Fatalf("status = %d, want 200", got)
	}
	if err := receive(t, current.result); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
}

func TestRun_HandlerNeverReturns_ReturnsDeadlineExceeded(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	current := serve(ctx, t, blockingHandler(started, release), 50*time.Millisecond)
	go func() {
		if response, err := http.Get(current.url); err == nil {
			response.Body.Close()
		}
	}()
	receive(t, started)

	cancel()

	if err := receive(t, current.result); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run() error = %v, want %v", err, context.DeadlineExceeded)
	}
}

func TestRun_ClosedListener_ReturnsServeError(t *testing.T) {
	listener, err := httpserver.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	listener.Close()

	err = httpserver.New("", http.NotFoundHandler(), time.Second, time.Second).Run(context.Background(), listener)

	if !errors.Is(err, net.ErrClosed) {
		t.Fatalf("Run() error = %v, want %v", err, net.ErrClosed)
	}
}

func TestListen_AddressInUse_ReturnsError(t *testing.T) {
	taken, err := httpserver.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	defer taken.Close()
	if _, err := httpserver.Listen(taken.Addr().String()); err == nil {
		t.Fatal("Listen() error = nil, want address in use")
	}
}
