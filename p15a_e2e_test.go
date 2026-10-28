package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type p15aSeedSummary struct {
	Seeded         bool `json:"seeded"`
	SchemaRevision int  `json:"schema_revision"`
	DataRevision   int  `json:"data_revision"`
	RowCount       int  `json:"row_count"`
}

type p15aCLIResult struct {
	StructuredContent json.RawMessage `json:"structuredContent"`
	IsError           bool            `json:"isError"`
}

type p15aHistory struct {
	Count      int `json:"count"`
	Operations []struct {
		OperationID      string   `json:"operation_id"`
		SourceMessageIDs []string `json:"source_message_ids"`
		SourceSessionID  string   `json:"source_session_id"`
		Receipt          struct {
			Action        string   `json:"action"`
			AffectedCount int      `json:"affected_count"`
			RowIDs        []string `json:"row_ids"`
		} `json:"receipt"`
	} `json:"operations"`
}

type p15aQuery struct {
	MatchedCount int `json:"matched_count"`
	Rows         []struct {
		RowID    string `json:"row_id"`
		Revision int    `json:"revision"`
		Values   struct {
			RepaymentStatus *string `json:"repayment_status"`
		} `json:"values"`
	} `json:"rows"`
}

func p15aRunCLIJSON(t *testing.T, cliPath string, args ...string) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "node", append([]string{cliPath}, args...)...)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		t.Fatalf("lightweight-table CLI failed: %v; stderr=%s", err, strings.TrimSpace(stderr.String()))
	}
	return bytes.TrimSpace(stdout.Bytes())
}

func p15aPromptSettled(t *testing.T, client *piClient, prompt, targetTool string, expectedTargetCalls int) {
	t.Helper()
	requestID, err := client.Prompt(prompt, "")
	if err != nil {
		t.Fatalf("send prompt: %v", err)
	}
	accepted := false
	targetStarts := 0
	targetEnds := 0
	targetErrors := 0
	deadline := time.After(120 * time.Second)
	for {
		select {
		case event := <-client.eventsForP15aTest():
			switch event.Kind {
			case piEventResponse:
				if event.RequestID == requestID && event.Command == "prompt" {
					if !event.Success {
						t.Fatalf("Pi rejected prompt: %s", event.Error)
					}
					accepted = true
				}
			case piEventToolStart:
				if event.ToolName == targetTool {
					targetStarts++
				}
			case piEventToolEnd:
				if event.ToolName == targetTool {
					targetEnds++
					if event.ToolIsError {
						targetErrors++
					}
				}
			case piEventSettled:
				if event.Aborted {
					t.Fatal("Pi run settled as aborted")
				}
				if !accepted {
					t.Fatal("Pi settled without accepting the prompt")
				}
				if targetStarts != expectedTargetCalls || targetEnds != expectedTargetCalls {
					t.Fatalf(
						"target tool %s call count = starts:%d ends:%d, want %d",
						targetTool,
						targetStarts,
						targetEnds,
						expectedTargetCalls,
					)
				}
				if targetErrors != 0 {
					t.Fatalf("target tool %s returned an error", targetTool)
				}
				return
			case piEventProcessExit:
				t.Fatalf("Pi exited before agent_settled: %s", event.Error)
			case piEventParseError:
				t.Fatalf("Pi RPC parse error: %s", event.Error)
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %s to settle", targetTool)
		}
	}
}

func (p *piClient) eventsForP15aTest() <-chan piEvent {
	return clientTestEvents(p)
}

func TestP15aLightweightTableBridgeE2E(t *testing.T) {
	if os.Getenv("BYOP_P15A_E2E") != "1" {
		t.Skip("set BYOP_P15A_E2E=1 to run the real Pi lightweight-table link")
	}
	if strings.TrimSpace(os.Getenv("BYOP_PI_PATH")) == "" {
		t.Fatal("BYOP_PI_PATH must point to the controlled Pi wrapper")
	}

	repoRoot, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	bridge := filepath.Join(repoRoot, "plugins", "lightweight-table", "pi-extension.mjs")
	probe := filepath.Join(repoRoot, "plugins", "lightweight-table", "test", "probe-extension.mjs")
	cliPath := filepath.Join(repoRoot, "plugins", "lightweight-table", "cli.mjs")
	fixture := filepath.Join(
		repoRoot,
		"plugins",
		"lightweight-table",
		"test",
		"fixtures",
		"synthetic-bank-seed.json",
	)
	seedInput := strings.TrimSpace(os.Getenv("BYOP_P15A_SEED_INPUT"))
	if seedInput == "" {
		seedInput = fixture
	}

	scratchRoot := strings.TrimSpace(os.Getenv("BYOP_P15A_SCRATCH"))
	if scratchRoot == "" {
		home, homeErr := os.UserHomeDir()
		if homeErr != nil {
			t.Fatal(homeErr)
		}
		scratchRoot = filepath.Join(home, "AgentScratch", "20261028-byop-table-link")
	}
	if err := os.MkdirAll(scratchRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	runDir, err := os.MkdirTemp(scratchRoot, "p15a-e2e-")
	if err != nil {
		t.Fatal(err)
	}
	databasePath := strings.TrimSpace(os.Getenv("BYOP_LIGHTWEIGHT_TABLE_DB"))
	if databasePath == "" {
		databasePath = filepath.Join(runDir, "tables.sqlite")
	}

	t.Setenv("BYOP_PI_EPHEMERAL", "")
	t.Setenv("BYOP_WORKSPACE", runDir)
	t.Setenv("BYOP_PI_EXTENSIONS", bridge+string(os.PathListSeparator)+probe)
	t.Setenv("BYOP_LIGHTWEIGHT_TABLE_DB", databasePath)

	var seed p15aSeedSummary
	if err := json.Unmarshal(
		p15aRunCLIJSON(t, cliPath, "seed", "--db", databasePath, "--input", seedInput, "--if-empty"),
		&seed,
	); err != nil {
		t.Fatalf("decode seed summary: %v", err)
	}
	if seed.RowCount < 1 || seed.SchemaRevision < 1 {
		t.Fatalf("seed summary is incomplete: rows=%d schema=%d", seed.RowCount, seed.SchemaRevision)
	}
	t.Logf("seed: rows=%d schema_revision=%d data_revision=%d", seed.RowCount, seed.SchemaRevision, seed.DataRevision)

	path, err := resolvePiPath(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	events := make(chan piEvent, 2048)
	client, err := startPiClient(path, runDir, func(event piEvent) {
		events <- event
	})
	if err != nil {
		t.Fatal(err)
	}
	integrationEventBuffers[client] = events
	defer func() {
		closeIntegrationPi(t, client)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	state, err := client.GetState(ctx)
	cancel()
	if err != nil {
		t.Fatalf("get_state: %v", err)
	}
	if state.SessionID == "" {
		t.Fatal("Pi returned an empty session ID")
	}

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	directName := "Synthetic direct E2E " + suffix
	directArgs := map[string]any{
		"action":                   "insert",
		"expected_schema_revision": seed.SchemaRevision,
		"rows": []map[string]any{{
			"name":             directName,
			"category":         "financial",
			"funding_source":   "bank",
			"repayment_status": "not_started",
		}},
	}
	directJSON, err := json.Marshal(directArgs)
	if err != nil {
		t.Fatal(err)
	}
	p15aPromptSettled(
		t,
		client,
		"Call the MCP tool mcp__lightweight_table__table_change exactly twice consecutively with exactly the same arguments: "+
			string(directJSON)+
			". Do not call any other tool. After both calls succeed, reply exactly P15A_DIRECT_OK.",
		"mcp__lightweight_table__table_change",
		2,
	)

	var initialQuery p15aQuery
	initialQueryArgs, err := json.Marshal(map[string]any{
		"limit":      1,
		"projection": []string{"repayment_status"},
		"filters": []map[string]any{{
			"column_id": "repayment_status",
			"op":        "not_null",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var initialCall p15aCLIResult
	if err := json.Unmarshal(
		p15aRunCLIJSON(t, cliPath, "call", "--db", databasePath, "--tool", "table_query", "--arguments", string(initialQueryArgs)),
		&initialCall,
	); err != nil {
		t.Fatalf("decode initial row query: %v", err)
	}
	if initialCall.IsError {
		t.Fatalf("initial table_query returned an error: %s", initialCall.StructuredContent)
	}
	if err := json.Unmarshal(initialCall.StructuredContent, &initialQuery); err != nil {
		t.Fatalf("decode initial table_query: %v", err)
	}
	if initialQuery.MatchedCount < 1 ||
		len(initialQuery.Rows) != 1 ||
		initialQuery.Rows[0].Values.RepaymentStatus == nil {
		t.Fatal("seed did not provide a bank row with repayment_status")
	}
	initialRow := initialQuery.Rows[0]
	testStatus := "p15a_probe"
	if *initialRow.Values.RepaymentStatus == testStatus {
		testStatus = "p15a_probe_2"
	}
	nestedArgs, err := json.Marshal(map[string]any{
		"row_id":                   initialRow.RowID,
		"original_status":          *initialRow.Values.RepaymentStatus,
		"test_status":              testStatus,
		"expected_revision":        initialRow.Revision,
		"expected_schema_revision": seed.SchemaRevision,
	})
	if err != nil {
		t.Fatal(err)
	}
	p15aPromptSettled(
		t,
		client,
		"Call the tool lightweight_table_probe exactly once with these exact arguments: "+
			string(nestedArgs)+
			". The probe itself will query, update, restore, and verify the row. Do not call any other tool. "+
			"After it succeeds, reply exactly P15A_NESTED_OK.",
		"lightweight_table_probe",
		1,
	)

	var finalQuery p15aQuery
	finalQueryArgs, err := json.Marshal(map[string]any{
		"row_ids":    []string{initialRow.RowID},
		"projection": []string{"repayment_status"},
	})
	if err != nil {
		t.Fatal(err)
	}
	var finalCall p15aCLIResult
	if err := json.Unmarshal(
		p15aRunCLIJSON(t, cliPath, "call", "--db", databasePath, "--tool", "table_query", "--arguments", string(finalQueryArgs)),
		&finalCall,
	); err != nil {
		t.Fatalf("decode final row query: %v", err)
	}
	if finalCall.IsError {
		t.Fatalf("final table_query returned an error: %s", finalCall.StructuredContent)
	}
	if err := json.Unmarshal(finalCall.StructuredContent, &finalQuery); err != nil {
		t.Fatalf("decode final table_query: %v", err)
	}
	if finalQuery.MatchedCount != 1 ||
		len(finalQuery.Rows) != 1 ||
		finalQuery.Rows[0].Revision != initialRow.Revision+2 ||
		finalQuery.Rows[0].Values.RepaymentStatus == nil ||
		*finalQuery.Rows[0].Values.RepaymentStatus != *initialRow.Values.RepaymentStatus {
		t.Fatal("nested probe did not advance two revisions and restore repayment_status")
	}

	var history p15aHistory
	historyArgs, err := json.Marshal(map[string]any{
		"source_session_id": state.SessionID,
		"limit":             200,
	})
	if err != nil {
		t.Fatal(err)
	}
	var callResult p15aCLIResult
	if err := json.Unmarshal(
		p15aRunCLIJSON(t, cliPath, "call", "--db", databasePath, "--tool", "table_history", "--arguments", string(historyArgs)),
		&callResult,
	); err != nil {
		t.Fatalf("decode history result: %v", err)
	}
	if callResult.IsError {
		t.Fatalf("table_history returned an error: %s", callResult.StructuredContent)
	}
	if err := json.Unmarshal(callResult.StructuredContent, &history); err != nil {
		t.Fatalf("decode table_history: %v", err)
	}
	if history.Count != 3 {
		t.Fatalf("expected one deduplicated insert plus two updates, got %d", history.Count)
	}
	seenMessages := make(map[string]bool)
	insertReceipts := 0
	updateReceipts := 0
	for _, operation := range history.Operations {
		if operation.Receipt.AffectedCount != 1 ||
			len(operation.Receipt.RowIDs) != 1 ||
			len(operation.SourceMessageIDs) == 0 {
			t.Fatalf("operation %s has an incomplete source or receipt", operation.OperationID)
		}
		switch operation.Receipt.Action {
		case "insert":
			insertReceipts++
		case "update":
			updateReceipts++
			if operation.Receipt.RowIDs[0] != initialRow.RowID {
				t.Fatal("nested update receipt targeted a different row")
			}
		default:
			t.Fatalf("unexpected operation action %q", operation.Receipt.Action)
		}
		seenMessages[strings.Join(operation.SourceMessageIDs, ",")] = true
	}
	if insertReceipts != 1 || updateReceipts != 2 {
		t.Fatalf(
			"receipt counts = insert:%d update:%d, want insert:1 update:2",
			insertReceipts,
			updateReceipts,
		)
	}
	if len(seenMessages) != 2 {
		t.Fatal("direct and nested writes did not retain distinct Pi user message groups")
	}
	t.Logf(
		"e2e: session=%s operations=%d insert_receipts=%d update_receipts=%d source_groups=%d revision_delta=2 status=ok",
		state.SessionID,
		history.Count,
		insertReceipts,
		updateReceipts,
		len(seenMessages),
	)
}
