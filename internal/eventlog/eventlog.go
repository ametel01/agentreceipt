package eventlog

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ametel01/agentreceipt/internal/model"
)

const genesisInput = "agentreceipt genesis"

type Writer struct {
	mu       sync.Mutex
	root     *os.Root
	file     *os.File
	prevHash string
	nextSeq  int64
	closed   bool
}

func GenesisHash() string {
	return hashBytes([]byte(genesisInput))
}

func hashBytes(data []byte) string {
	sum := sha256.Sum256(data)

	return "sha256:" + hex.EncodeToString(sum[:])
}

func Normalize(event model.Event) (model.Event, error) {
	if event.SessionID == "" {
		return model.Event{}, errors.New("event session_id is required")
	}
	if event.Source == "" {
		return model.Event{}, errors.New("event source is required")
	}
	if event.Type == "" {
		return model.Event{}, errors.New("event type is required")
	}
	if event.Payload == nil {
		event.Payload = map[string]any{}
	}
	if event.Provider == "" {
		event.Provider = "unknown"
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	} else {
		event.Timestamp = event.Timestamp.UTC()
	}

	return event, nil
}

func LinkEvent(prevHash string, event model.Event) (model.Event, error) {
	normalized, err := Normalize(event)
	if err != nil {
		return model.Event{}, err
	}
	if prevHash == "" {
		prevHash = GenesisHash()
	}
	normalized.PrevHash = prevHash
	normalized.EventHash = ""
	eventHash, err := EventHash(prevHash, normalized)
	if err != nil {
		return model.Event{}, err
	}
	normalized.EventHash = eventHash

	return normalized, nil
}

func EventHash(prevHash string, event model.Event) (string, error) {
	event.PrevHash = prevHash
	event.EventHash = ""
	data, err := model.MarshalCanonical(event)
	if err != nil {
		return "", err
	}
	sum := sha256.New()
	_, _ = sum.Write([]byte(prevHash))
	_, _ = sum.Write(data)

	return "sha256:" + hex.EncodeToString(sum.Sum(nil)), nil
}

func NewWriter(path string, prevHash string, nextSeq int64) (*Writer, error) {
	if nextSeq <= 0 {
		nextSeq = 1
	}
	if prevHash == "" {
		prevHash = GenesisHash()
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("create event log directory: %w", err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("open event log root: %w", err)
	}
	file, err := root.OpenFile(filepath.Base(path), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		_ = root.Close()
		return nil, fmt.Errorf("open event log: %w", err)
	}

	return &Writer{root: root, file: file, prevHash: prevHash, nextSeq: nextSeq}, nil
}

func (w *Writer) Append(event model.Event) (model.Event, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return model.Event{}, errors.New("event log writer is closed")
	}
	event.Seq = w.nextSeq
	linked, err := LinkEvent(w.prevHash, event)
	if err != nil {
		return model.Event{}, err
	}
	line, err := model.MarshalCanonical(linked)
	if err != nil {
		return model.Event{}, err
	}
	line = append(line, '\n')
	written, err := w.file.Write(line)
	if err != nil {
		return model.Event{}, fmt.Errorf("append event: %w", err)
	}
	if written != len(line) {
		return model.Event{}, io.ErrShortWrite
	}
	if err := w.file.Sync(); err != nil {
		return model.Event{}, fmt.Errorf("sync event log: %w", err)
	}
	w.prevHash = linked.EventHash
	w.nextSeq++

	return linked, nil
}

func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil
	}
	w.closed = true
	fileErr := w.file.Close()
	rootErr := w.root.Close()
	if fileErr != nil {
		return fileErr
	}

	return rootErr
}

func ReadFile(path string) ([]model.Event, error) {
	root, name, err := openRootForPath(path)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = root.Close()
	}()
	file, err := root.Open(name)
	if err != nil {
		return nil, fmt.Errorf("open event log: %w", err)
	}
	defer func() {
		_ = file.Close()
	}()

	return Read(file)
}

func Read(reader io.Reader) ([]model.Event, error) {
	scanner := bufio.NewScanner(reader)
	events := make([]model.Event, 0)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		if len(scanner.Bytes()) == 0 {
			continue
		}
		var event model.Event
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return nil, fmt.Errorf("decode event line %d: %w", lineNumber, err)
		}
		events = append(events, event)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read event log: %w", err)
	}

	return events, nil
}

func Replay(events []model.Event) (string, error) {
	prevHash := GenesisHash()
	for index, event := range events {
		expectedSeq := int64(index + 1)
		if event.Seq != expectedSeq {
			return "", fmt.Errorf("event sequence mismatch at index %d: got %d, want %d", index, event.Seq, expectedSeq)
		}
		if event.PrevHash != prevHash {
			return "", fmt.Errorf("event prev_hash mismatch at seq %d", event.Seq)
		}
		recomputed, err := EventHash(event.PrevHash, event)
		if err != nil {
			return "", err
		}
		if event.EventHash != recomputed {
			return "", fmt.Errorf("event_hash mismatch at seq %d", event.Seq)
		}
		prevHash = event.EventHash
	}

	return prevHash, nil
}

func openRootForPath(path string) (*os.Root, string, error) {
	clean := filepath.Clean(path)
	dir := filepath.Dir(clean)
	name := filepath.Base(clean)
	if name == "." || name == string(filepath.Separator) {
		return nil, "", fmt.Errorf("event log path %q does not name a file", path)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, "", fmt.Errorf("open event log root %q: %w", dir, err)
	}

	return root, name, nil
}
