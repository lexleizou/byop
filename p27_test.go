package main

import (
	"path/filepath"
	"testing"
)

// P27: a brand-new Pi session must start from Pi's own state. Model memory
// keyed by the previous session's file may not leak into it, and the old
// session must keep its memory for a later restore.
func TestP27NewSessionDoesNotInheritFileKeyedModelMemory(t *testing.T) {
	prevFile := canonicalPiPath(filepath.Join(t.TempDir(), "prev.jsonl"))
	familyID := piModelChoiceID("magpie", "DeepSeek V4.1 Flash")
	remembered := modelTuple{VariantID: "deepseek-1m", Thinking: "high"}
	state := &appState{
		piSessionFile: prevFile,
		piSessionID:   "prev-id",
		modelTuples: map[string]map[string]modelTuple{
			prevFile: {familyID: remembered},
		},
	}
	// Pi created a brand-new session: fresh ID, no session file yet.
	state.applyPiSessionState(&piSessionState{SessionID: "new-id"})
	if _, exists := state.modelTuples["new-id"][familyID]; exists {
		t.Fatal("new session inherited the previous session's model memory")
	}
	if got := state.modelTuples[prevFile][familyID]; got != remembered {
		t.Fatalf("previous session tuple = %#v, want it preserved as %#v", got, remembered)
	}
	if _, ok := state.rememberedModelTuple("magpie", "DeepSeek V4.1 Flash"); ok {
		t.Fatal("new session resolved a remembered tuple from the previous session")
	}
}

// Switching between sessions keeps each session's memory under its own key,
// and returning to a known session restores that session's memory.
func TestP27SessionSwitchKeepsModelMemoryIsolated(t *testing.T) {
	familyID := piModelChoiceID("magpie", "DeepSeek V4.1 Flash")
	memoryA := modelTuple{VariantID: "deepseek-1m", Thinking: "max"}
	state := &appState{
		piSessionID: "session-a",
		modelTuples: map[string]map[string]modelTuple{
			"session-a": {familyID: memoryA},
		},
	}
	state.applyPiSessionState(&piSessionState{SessionID: "session-b"})
	if got := state.modelTuples["session-a"][familyID]; got != memoryA {
		t.Fatalf("session A tuple after switch = %#v, want %#v", got, memoryA)
	}
	if len(state.modelTuples["session-b"]) != 0 {
		t.Fatalf("session B tuples = %#v, want none", state.modelTuples["session-b"])
	}
	// Session B records its own choice while staying isolated from A.
	state.modelTuples["session-b"] = map[string]modelTuple{
		familyID: {VariantID: "deepseek-256k", Thinking: "low"},
	}
	state.applyPiSessionState(&piSessionState{SessionID: "session-a"})
	if got := state.modelTuples["session-a"][familyID]; got != memoryA {
		t.Fatalf("session A tuple after return = %#v, want %#v", got, memoryA)
	}
}

// The ID-to-file handoff within one session still migrates memory, and it
// must not disturb the memory of unrelated sessions.
func TestP27SessionIDFileHandoffMigratesWithoutTouchingOtherSessions(t *testing.T) {
	familyID := piModelChoiceID("magpie", "DeepSeek V4.1 Flash")
	memoryA := modelTuple{VariantID: "deepseek-1m", Thinking: "high"}
	memoryZ := modelTuple{VariantID: "deepseek-256k", Thinking: "low"}
	sessionFile := filepath.Join(t.TempDir(), "session.jsonl")
	state := &appState{
		piSessionID: "session-a",
		modelTuples: map[string]map[string]modelTuple{
			"session-a": {familyID: memoryA},
			"session-z": {familyID: memoryZ},
		},
	}
	state.applyPiSessionState(&piSessionState{
		SessionID:   "session-a",
		SessionFile: sessionFile,
	})
	sessionKey := canonicalPiPath(sessionFile)
	if got := state.modelTuples[sessionKey][familyID]; got != memoryA {
		t.Fatalf("migrated tuple = %#v, want %#v", got, memoryA)
	}
	if _, exists := state.modelTuples["session-a"]; exists {
		t.Fatal("session-ID tuple key survived migration")
	}
	if got := state.modelTuples["session-z"][familyID]; got != memoryZ {
		t.Fatalf("unrelated session tuple = %#v, want %#v", got, memoryZ)
	}
}
