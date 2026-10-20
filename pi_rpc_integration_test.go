package main

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestPiRPCIntegrationMinimalLoop(t *testing.T) {
	client := startIntegrationPi(t)
	defer closeIntegrationPi(t, client)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	state, err := client.GetState(ctx)
	if err != nil {
		t.Fatalf("get_state: %v", err)
	}
	if state.SessionID == "" {
		t.Fatal("get_state returned an empty session ID")
	}
	t.Logf("handshake: streaming=%t model=%s", state.IsStreaming, state.Model.Name)

	requestID, err := client.Prompt("Reply with exactly BYOP_RPC_OK and no other text.")
	if err != nil {
		t.Fatalf("prompt: %v", err)
	}

	var streamedText string
	var finalText string
	accepted := false
	settled := false
	deadline := time.After(90 * time.Second)
	for !settled {
		select {
		case event := <-clientTestEvents(client):
			switch event.Kind {
			case piEventResponse:
				if event.RequestID == requestID && event.Command == "prompt" {
					if !event.Success {
						t.Fatalf("prompt rejected: %s", event.Error)
					}
					accepted = true
				}
			case piEventText:
				streamedText = event.Text
			case piEventMessage:
				finalText = event.Text
			case piEventSettled:
				settled = true
			case piEventProcessExit:
				t.Fatalf("Pi exited before agent_settled: %s", event.Error)
			case piEventParseError:
				t.Fatalf("Pi protocol error: %s", event.Error)
			}
		case <-deadline:
			t.Fatalf("timed out waiting for agent_settled; accepted=%t streamed=%t", accepted, streamedText != "")
		}
	}

	if !accepted {
		t.Fatal("did not receive a successful prompt response")
	}
	if strings.TrimSpace(streamedText) == "" && strings.TrimSpace(finalText) == "" {
		t.Fatal("agent_settled arrived without assistant text")
	}
	t.Logf("completion: accepted=%t streamed=%t settled=%t", accepted, streamedText != "", settled)
}

func TestPiRPCIntegrationStop(t *testing.T) {
	client := startIntegrationPi(t)
	defer closeIntegrationPi(t, client)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	if _, err := client.GetState(ctx); err != nil {
		cancel()
		t.Fatalf("get_state: %v", err)
	}
	cancel()

	requestID, err := client.Prompt("Count slowly from 1 to 1000. Output one number per line and do not use tools.")
	if err != nil {
		t.Fatalf("prompt: %v", err)
	}

	if event := waitForIntegrationEvent(t, client, 20*time.Second, func(event piEvent) bool {
		return event.Kind == piEventResponse && event.RequestID == requestID
	}); !event.Success {
		t.Fatalf("prompt rejected: %s", event.Error)
	}

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 30*time.Second)
	note, err := client.Stop(stopCtx)
	stopCancel()
	if err != nil {
		t.Fatalf("stop: %v", err)
	}

	event := waitForIntegrationEvent(t, client, 30*time.Second, func(event piEvent) bool {
		return event.Kind == piEventSettled
	})
	if !event.Aborted {
		t.Fatal("agent_settled did not report an aborted run")
	}
	t.Logf("stop: queue_note=%q aborted=%t", note, event.Aborted)
}

var integrationEventBuffers = make(map[*piClient]<-chan piEvent)

func startIntegrationPi(t *testing.T) *piClient {
	t.Helper()
	if os.Getenv("BYOP_PI_INTEGRATION") != "1" {
		t.Skip("set BYOP_PI_INTEGRATION=1 to run against the installed Pi CLI")
	}
	path, err := resolvePiPath()
	if err != nil {
		t.Fatal(err)
	}
	events := make(chan piEvent, 1024)
	client, err := startPiClient(path, "", func(event piEvent) {
		events <- event
	})
	if err != nil {
		t.Fatalf("start Pi: %v", err)
	}
	integrationEventBuffers[client] = events
	return client
}

func clientTestEvents(client *piClient) <-chan piEvent {
	return integrationEventBuffers[client]
}

func waitForIntegrationEvent(t *testing.T, client *piClient, timeout time.Duration, match func(piEvent) bool) piEvent {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case event := <-clientTestEvents(client):
			if event.Kind == piEventProcessExit {
				t.Fatalf("Pi exited before the expected event: %s", event.Error)
			}
			if event.Kind == piEventParseError {
				t.Fatalf("Pi protocol error: %s", event.Error)
			}
			if match(event) {
				return event
			}
		case <-deadline:
			t.Fatal("timed out waiting for Pi event")
		}
	}
}

func closeIntegrationPi(t *testing.T, client *piClient) {
	t.Helper()
	client.Close()
	select {
	case <-client.waitDone:
	case <-time.After(5 * time.Second):
		t.Error("Pi process did not exit within 5 seconds")
	}
	delete(integrationEventBuffers, client)
}
