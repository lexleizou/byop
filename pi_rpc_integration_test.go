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

func TestPiRPCIntegrationTool(t *testing.T) {
	client := startIntegrationPi(t)
	defer closeIntegrationPi(t, client)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	if _, err := client.GetState(ctx); err != nil {
		cancel()
		t.Fatalf("get_state: %v", err)
	}
	cancel()

	requestID, err := client.Prompt(
		"Use the bash tool exactly once to run this exact command: printf 'BYOP_TOOL_OK'. " +
			"Do not inspect files or use any other tool. After it succeeds, reply with exactly BYOP_TOOL_DONE.",
	)
	if err != nil {
		t.Fatalf("prompt: %v", err)
	}

	var bashStart *piEvent
	var bashEnd *piEvent
	var finalText string
	order := make([]string, 0, 5)
	accepted := false
	settled := false
	deadline := time.After(120 * time.Second)
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
					order = append(order, "accepted")
				}
			case piEventToolStart:
				order = append(order, "tool_start")
				if event.ToolCallID == "" {
					t.Fatal("tool start has an empty toolCallId")
				}
				if event.ToolName == "bash" && strings.Contains(string(event.ToolArgs), "BYOP_TOOL_OK") {
					if bashStart != nil {
						t.Fatal("Pi started the marker bash tool more than once")
					}
					copied := event
					bashStart = &copied
				}
			case piEventToolUpdate:
				if bashStart != nil && event.ToolCallID == bashStart.ToolCallID {
					order = append(order, "tool_update")
				}
			case piEventToolEnd:
				order = append(order, "tool_end")
				if bashStart != nil && event.ToolCallID == bashStart.ToolCallID {
					copied := event
					bashEnd = &copied
				}
			case piEventMessage:
				finalText = event.Text
			case piEventSettled:
				order = append(order, "settled")
				settled = true
			case piEventProcessExit:
				t.Fatalf("Pi exited before agent_settled: %s", event.Error)
			case piEventParseError:
				t.Fatalf("Pi protocol error: %s", event.Error)
			}
		case <-deadline:
			t.Fatalf("timed out waiting for the tool run; accepted=%t start=%t end=%t", accepted, bashStart != nil, bashEnd != nil)
		}
	}

	if !accepted {
		t.Fatal("did not receive a successful prompt response")
	}
	if bashStart == nil {
		t.Fatal("did not observe the marker bash tool start event")
	}
	if bashEnd == nil {
		t.Fatal("did not observe the matching marker bash tool end event")
	}
	if bashStart.ToolCallID != bashEnd.ToolCallID {
		t.Fatalf("correlation failed: start=%q end=%q", bashStart.ToolCallID, bashEnd.ToolCallID)
	}
	if bashEnd.ToolIsError {
		t.Fatal("marker bash tool ended with an error")
	}
	if !strings.Contains(formatToolResult(bashEnd.ToolResult), "BYOP_TOOL_OK") {
		t.Fatalf("tool result did not contain the marker: %s", string(bashEnd.ToolResult))
	}
	if bashEnd.ToolDurationMS < 0 {
		t.Fatalf("tool duration = %d, want non-negative", bashEnd.ToolDurationMS)
	}
	startAt := strings.Index(strings.Join(order, ","), "tool_start")
	endAt := strings.LastIndex(strings.Join(order, ","), "tool_end")
	settledAt := strings.LastIndex(strings.Join(order, ","), "settled")
	if startAt < 0 || endAt < startAt || settledAt < endAt {
		t.Fatalf("unexpected event order: %v", order)
	}
	t.Logf("tool: id=%s duration_ms=%d final=%q order=%v",
		bashStart.ToolCallID, bashEnd.ToolDurationMS, finalText, order)
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
