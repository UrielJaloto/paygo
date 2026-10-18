//go:build integration && unix

package main_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptrace"
	"strings"
	"syscall"
	"testing"
	"time"
)

const continueTimeout = time.Minute

func TestE2E_GracefulShutdown(t *testing.T) {
	running := startStack(t)
	_, token := running.issueKey(t, ownerOne)
	reader, writer := io.Pipe()
	reading := make(chan struct{})
	finished := make(chan int, 1)
	go func() { finished <- postStreamed(running.api+"/v1/accounts", token, reader, reading) }()
	select {
	case <-reading:
	case <-time.After(receiveTimeout):
		t.Fatal("the server never started reading the body")
	}
	writeChunk(t, writer, `{"curr`)
	if err := running.process.command.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("signaling the api: %v", err)
	}
	eventually(t, stopTimeout, "the api to refuse connections", func() bool {
		response, err := http.Get(running.api + "/healthz")
		if err != nil {
			return true
		}
		response.Body.Close()
		return false
	})
	writeChunk(t, writer, `ency":"BRL"}`)
	writer.Close()
	if status := <-finished; status != http.StatusCreated {
		t.Fatalf("in-flight status = %d, want %d", status, http.StatusCreated)
	}
	<-running.process.done
	if running.process.err != nil {
		t.Fatalf("exit = %v, want 0", running.process.err)
	}
}

func writeChunk(t *testing.T, writer io.Writer, chunk string) {
	t.Helper()
	if _, err := io.Copy(writer, strings.NewReader(chunk)); err != nil {
		t.Fatalf("writing the body: %v", err)
	}
}

func postStreamed(target, token string, body io.Reader, reading chan<- struct{}) int {
	trace := &httptrace.ClientTrace{Got100Continue: func() { close(reading) }}
	request, err := http.NewRequestWithContext(httptrace.WithClientTrace(context.Background(), trace), http.MethodPost, target, body)
	if err != nil {
		return 0
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Expect", "100-continue")
	client := http.Client{Transport: &http.Transport{ExpectContinueTimeout: continueTimeout}}
	response, err := client.Do(request)
	if err != nil {
		return 0
	}
	defer response.Body.Close()
	return response.StatusCode
}
