package eventlog

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ametel01/agentreceipt/internal/model"
)

func TestLinkEventIsDeterministic(t *testing.T) {
	t.Parallel()

	event := model.Event{
		EventID:   "evt_test",
		SessionID: "ar_ses_test",
		Timestamp: time.Date(2026, 6, 16, 1, 2, 3, 0, time.UTC),
		Source:    "git_monitor",
		Type:      "git.snapshot",
		Payload:   map[string]any{"z": "last", "a": "first"},
	}
	first, err := LinkEvent("", event)
	if err != nil {
		t.Fatalf("LinkEvent() error = %v", err)
	}
	second, err := LinkEvent("", event)
	if err != nil {
		t.Fatalf("LinkEvent() second error = %v", err)
	}
	if first.EventHash != second.EventHash {
		t.Fatalf("event hashes differ: %s != %s", first.EventHash, second.EventHash)
	}
	if first.PrevHash != GenesisHash() {
		t.Fatalf("PrevHash = %q, want genesis hash", first.PrevHash)
	}
	if first.Provider != "unknown" {
		t.Fatalf("Provider = %q, want unknown", first.Provider)
	}
}

func TestWriterAppendAndReplay(t *testing.T) {
	t.Parallel()

	path := t.TempDir() + "/events.jsonl"
	writer, err := NewWriter(path, "", 1)
	if err != nil {
		t.Fatalf("NewWriter() error = %v", err)
	}
	first, err := writer.Append(testEvent("evt_1", "git.snapshot"))
	if err != nil {
		t.Fatalf("Append() first error = %v", err)
	}
	second, err := writer.Append(testEvent("evt_2", "fs.change"))
	if err != nil {
		t.Fatalf("Append() second error = %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if first.Seq != 1 || second.Seq != 2 {
		t.Fatalf("unexpected sequences: %d, %d", first.Seq, second.Seq)
	}
	if second.PrevHash != first.EventHash {
		t.Fatalf("second PrevHash = %q, want %q", second.PrevHash, first.EventHash)
	}

	events, err := ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	chainHash, err := Replay(events)
	if err != nil {
		t.Fatalf("Replay() error = %v", err)
	}
	if chainHash != second.EventHash {
		t.Fatalf("chain hash = %q, want %q", chainHash, second.EventHash)
	}
}

func TestReplayDetectsBrokenChain(t *testing.T) {
	t.Parallel()

	first, err := LinkEvent("", testEvent("evt_1", "git.snapshot"))
	if err != nil {
		t.Fatalf("LinkEvent() error = %v", err)
	}
	second, err := LinkEvent(first.EventHash, testEvent("evt_2", "fs.change"))
	if err != nil {
		t.Fatalf("LinkEvent() second error = %v", err)
	}
	first.Seq = 1
	second.Seq = 2
	second.PrevHash = GenesisHash()

	if _, err := Replay([]model.Event{first, second}); err == nil {
		t.Fatal("Replay() returned nil error for a broken chain")
	}
}

func TestNormalizeRejectsMissingRequiredFields(t *testing.T) {
	t.Parallel()

	for _, event := range []model.Event{
		{Source: "git_monitor", Type: "git.snapshot"},
		{SessionID: "ar_ses_test", Type: "git.snapshot"},
		{SessionID: "ar_ses_test", Source: "git_monitor"},
	} {
		if _, err := Normalize(event); err == nil {
			t.Fatalf("Normalize() returned nil error for event: %+v", event)
		}
	}
}

func TestReadRejectsMalformedJSONL(t *testing.T) {
	t.Parallel()

	if _, err := Read(strings.NewReader("{not-json}\n")); err == nil {
		t.Fatal("Read() returned nil error for malformed JSONL")
	}
}

func TestWriterRejectsAppendAfterClose(t *testing.T) {
	t.Parallel()

	writer, err := NewWriter(t.TempDir()+"/events.jsonl", "", 1)
	if err != nil {
		t.Fatalf("NewWriter() error = %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if _, err := writer.Append(testEvent("evt_1", "git.snapshot")); err == nil {
		t.Fatal("Append() returned nil error after Close()")
	}
}

func TestAppendTransactionAppendsAndExposesSnapshot(t *testing.T) {
	t.Parallel()

	path := t.TempDir() + "/events.jsonl"
	result, err := AppendTransaction(path, func(tx *AppendTx) error {
		snapshot := tx.Snapshot()
		if snapshot.EventCount != 0 || snapshot.ChainHash != GenesisHash() || snapshot.NextSeq != 1 {
			t.Fatalf("initial snapshot = %+v", snapshot)
		}
		first, err := tx.Append(testEvent("evt_1", "git.snapshot"))
		if err != nil {
			return err
		}
		if first.Seq != 1 {
			t.Fatalf("first seq = %d, want 1", first.Seq)
		}
		appendResult, err := tx.AppendAll([]model.Event{testEvent("evt_2", "fs.change")})
		if err != nil {
			return err
		}
		if appendResult.EventCount != 2 || appendResult.LastEvent.Seq != 2 {
			t.Fatalf("append result = %+v", appendResult)
		}
		snapshot = tx.Snapshot()
		if snapshot.EventCount != 2 || snapshot.NextSeq != 3 || len(snapshot.Events) != 2 {
			t.Fatalf("updated snapshot = %+v", snapshot)
		}

		return nil
	})
	if err != nil {
		t.Fatalf("AppendTransaction() error = %v", err)
	}
	if result.EventCount != 2 || result.LastEvent.Seq != 2 || result.ChainHash == "" {
		t.Fatalf("result = %+v", result)
	}
	events, err := ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("event count = %d, want 2", len(events))
	}
	if _, err := Replay(events); err != nil {
		t.Fatalf("Replay() error = %v", err)
	}
}

func TestAppendTransactionAllowsEmptyAppend(t *testing.T) {
	t.Parallel()

	path := t.TempDir() + "/events.jsonl"
	result, err := AppendBatch(path, nil)
	if err != nil {
		t.Fatalf("AppendBatch(nil) error = %v", err)
	}
	if result.EventCount != 0 || result.ChainHash != GenesisHash() || result.LastEvent.EventID != "" {
		t.Fatalf("empty append result = %+v", result)
	}
}

func TestAppendTransactionRejectsAppendAfterReturn(t *testing.T) {
	t.Parallel()

	path := t.TempDir() + "/events.jsonl"
	var saved *AppendTx
	if _, err := AppendTransaction(path, func(tx *AppendTx) error {
		saved = tx

		return nil
	}); err != nil {
		t.Fatalf("AppendTransaction() error = %v", err)
	}
	if _, err := saved.Append(testEvent("evt_1", "git.snapshot")); err == nil {
		t.Fatal("Append() after transaction returned nil error")
	}
}

func TestAppendTransactionSnapshotIsDefensiveCopy(t *testing.T) {
	t.Parallel()

	path := t.TempDir() + "/events.jsonl"
	if _, err := AppendBatch(path, []model.Event{testEvent("evt_1", "git.snapshot")}); err != nil {
		t.Fatalf("AppendBatch() error = %v", err)
	}
	if _, err := AppendTransaction(path, func(tx *AppendTx) error {
		snapshot := tx.Snapshot()
		snapshot.Events[0].EventID = "mutated"
		snapshot.Events[0].Payload["path"] = "mutated"
		next := tx.Snapshot()
		if next.Events[0].EventID != "evt_1" || next.Events[0].Payload["path"] != "README.md" {
			t.Fatalf("snapshot mutation leaked into tx: %+v", next.Events[0])
		}

		return nil
	}); err != nil {
		t.Fatalf("AppendTransaction() error = %v", err)
	}
}

func TestWithAppendLockSerializesReplayAppend(t *testing.T) {
	t.Parallel()

	path := t.TempDir() + "/events.jsonl"
	writer, err := NewWriter(path, "", 1)
	if err != nil {
		t.Fatalf("NewWriter() error = %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	const workers = 20
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for index := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := WithAppendLock(path, func() error {
				events, err := ReadFile(path)
				if err != nil {
					return err
				}
				prevHash, err := Replay(events)
				if err != nil {
					return err
				}
				writer, err := NewWriter(path, prevHash, int64(len(events)+1))
				if err != nil {
					return err
				}
				_, appendErr := writer.Append(testEvent(fmt.Sprintf("evt_%02d", index), "fs.change"))
				closeErr := writer.Close()
				if appendErr != nil {
					return appendErr
				}

				return closeErr
			})
			if err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("locked append failed: %v", err)
	}
	events, err := ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if len(events) != workers {
		t.Fatalf("event count = %d, want %d", len(events), workers)
	}
	if _, err := Replay(events); err != nil {
		t.Fatalf("Replay() error = %v", err)
	}
	for index, event := range events {
		if event.Seq != int64(index+1) {
			t.Fatalf("event %d seq = %d, want %d", index, event.Seq, index+1)
		}
	}
}

func TestAppendBatchSerializesConcurrentAppends(t *testing.T) {
	t.Parallel()

	path := t.TempDir() + "/events.jsonl"
	const workers = 20
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for index := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := AppendBatch(path, []model.Event{testEvent(fmt.Sprintf("evt_%02d", index), "fs.change")})
			if err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("AppendBatch() failed: %v", err)
	}
	events, err := ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if len(events) != workers {
		t.Fatalf("event count = %d, want %d", len(events), workers)
	}
	if _, err := Replay(events); err != nil {
		t.Fatalf("Replay() error = %v", err)
	}
}

func testEvent(eventID string, eventType string) model.Event {
	return model.Event{
		EventID:   eventID,
		SessionID: "ar_ses_test",
		Timestamp: time.Date(2026, 6, 16, 1, 2, 3, 0, time.UTC),
		Source:    "git_monitor",
		Type:      eventType,
		Payload:   map[string]any{"path": "README.md"},
	}
}
