package codex

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"
)

type TailOptions struct {
	SessionID      string
	CWD            string
	Offset         int64
	LineOffset     int
	MaxOutputBytes int
	MaxTailBytes   int
}

type TailResult struct {
	ParseResult
	NextOffset     int64
	NextLineOffset int
	CompleteLines  int
}

const defaultMaxTailBytes = 1024 * 1024

func SessionCWD(path string) (string, bool, error) {
	root, name, err := openRootForPath(path)
	if err != nil {
		return "", false, err
	}
	defer func() {
		_ = root.Close()
	}()
	file, err := root.Open(name)
	if err != nil {
		return "", false, fmt.Errorf("open Codex JSONL: %w", err)
	}
	defer func() {
		_ = file.Close()
	}()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var raw map[string]any
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			continue
		}
		payload := mapField(raw, "payload")
		if payload == nil {
			payload = raw
		}
		if cwd := firstString(payload, "cwd", "current_dir", "working_directory"); cwd != "" {
			return filepath.Clean(cwd), true, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", false, err
	}

	return "", false, nil
}

func TailFile(path string, options TailOptions) (TailResult, error) {
	root, name, err := openRootForPath(path)
	if err != nil {
		return TailResult{}, err
	}
	defer func() {
		_ = root.Close()
	}()
	file, err := root.Open(name)
	if err != nil {
		return TailResult{}, fmt.Errorf("open Codex JSONL: %w", err)
	}
	defer func() {
		_ = file.Close()
	}()
	info, err := file.Stat()
	if err != nil {
		return TailResult{}, err
	}
	offset := options.Offset
	lineOffset := options.LineOffset
	if offset > info.Size() {
		offset = 0
		lineOffset = 0
	}
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return TailResult{}, err
	}
	maxTailBytes := options.MaxTailBytes
	if maxTailBytes <= 0 {
		maxTailBytes = defaultMaxTailBytes
	}
	chunk := make([]byte, maxTailBytes)
	n, err := file.Read(chunk)
	if err != nil && err != io.EOF {
		return TailResult{}, err
	}
	chunk = chunk[:n]
	tail := TailResult{NextOffset: offset, NextLineOffset: lineOffset}
	if len(chunk) == 0 {
		return tail, nil
	}
	lastNewline := bytes.LastIndexByte(chunk, '\n')
	if lastNewline < 0 {
		if len(chunk) == maxTailBytes {
			nextOffset, foundNewline, err := discardOversizedLine(file, offset+int64(len(chunk)), info.Size(), maxTailBytes)
			if err != nil {
				return TailResult{}, err
			}
			tail.Warnings = append(tail.Warnings, ParseWarning{
				LineNumber: lineOffset + 1,
				Code:       "tail_line_too_large",
				Message:    fmt.Sprintf("Codex JSONL line exceeded max tail read size of %d bytes", maxTailBytes),
			})
			tail.WarningCount = len(tail.Warnings)
			if foundNewline {
				tail.CompleteLines = 1
				tail.NextOffset = nextOffset
				tail.NextLineOffset = lineOffset + 1
			}
		}
		return tail, nil
	}
	complete := chunk[:lastNewline+1]
	tail.CompleteLines = bytes.Count(complete, []byte{'\n'})
	tail.NextOffset = offset + int64(len(complete))
	tail.NextLineOffset = lineOffset + tail.CompleteLines
	tail.ParseResult = ParseJSONL(bytes.NewReader(complete), ParseOptions{
		SessionID:      options.SessionID,
		CWD:            options.CWD,
		SourcePath:     path,
		MaxOutputBytes: options.MaxOutputBytes,
		LineOffset:     lineOffset,
	})

	return tail, nil
}

func discardOversizedLine(file interface {
	Read([]byte) (int, error)
}, startOffset int64, size int64, maxTailBytes int) (int64, bool, error) {
	buffer := make([]byte, maxTailBytes)
	offset := startOffset
	for offset < size {
		n, err := file.Read(buffer)
		if err != nil && err != io.EOF {
			return offset, false, err
		}
		if n == 0 {
			return offset, false, nil
		}
		if newline := bytes.IndexByte(buffer[:n], '\n'); newline >= 0 {
			return offset + int64(newline) + 1, true, nil
		}
		offset += int64(n)
		if err == io.EOF {
			return offset, false, nil
		}
	}

	return offset, false, nil
}
