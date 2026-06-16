package fswatcher

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/ametel01/agentreceipt/internal/config"
	"github.com/ametel01/agentreceipt/internal/eventlog"
	"github.com/ametel01/agentreceipt/internal/model"
	"github.com/ametel01/agentreceipt/internal/storage"
)

const Source = "fs_watcher"

var dependencyPatterns = []string{
	"package.json",
	"package-lock.json",
	"pnpm-lock.yaml",
	"yarn.lock",
	"bun.lock",
	"bun.lockb",
	"go.mod",
	"go.sum",
	"Cargo.toml",
	"Cargo.lock",
	"pyproject.toml",
	"requirements*.txt",
}

type Classifier struct {
	SensitivePatterns  []string
	DependencyPatterns []string
}

type Classification struct {
	Sensitive  bool `json:"sensitive"`
	Dependency bool `json:"dependency"`
}

type Watcher struct {
	sessionID  string
	repoRoot   string
	classifier Classifier
	debounce   time.Duration
	watcher    *fsnotify.Watcher
	events     chan model.Event

	mu      sync.Mutex
	pending map[string]fsnotify.Op
	changed map[string]model.ChangedFile
	counter int64
}

func NewClassifier(cfg config.Config) Classifier {
	return Classifier{
		SensitivePatterns:  append([]string(nil), cfg.SensitivePaths...),
		DependencyPatterns: append([]string(nil), dependencyPatterns...),
	}
}

func (c Classifier) Classify(path string) Classification {
	normalized := filepath.ToSlash(filepath.Clean(path))

	return Classification{
		Sensitive:  matchesAny(normalized, c.SensitivePatterns),
		Dependency: matchesAny(normalized, c.DependencyPatterns),
	}
}

func New(repoRoot string, sessionID string, classifier Classifier, debounce time.Duration) (*Watcher, error) {
	if repoRoot == "" {
		return nil, errors.New("repo root is required")
	}
	if err := storage.ValidateSessionID(sessionID); err != nil {
		return nil, err
	}
	if debounce <= 0 {
		debounce = 50 * time.Millisecond
	}
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("create fs watcher: %w", err)
	}

	return &Watcher{
		sessionID:  sessionID,
		repoRoot:   repoRoot,
		classifier: classifier,
		debounce:   debounce,
		watcher:    watcher,
		events:     make(chan model.Event, 64),
		pending:    make(map[string]fsnotify.Op),
		changed:    make(map[string]model.ChangedFile),
	}, nil
}

func (w *Watcher) Start(ctx context.Context) error {
	if err := w.watchTree(); err != nil {
		return err
	}
	go w.run(ctx)

	return nil
}

func (w *Watcher) Events() <-chan model.Event {
	return w.events
}

func (w *Watcher) ChangedFiles() []model.ChangedFile {
	w.mu.Lock()
	defer w.mu.Unlock()

	files := make([]model.ChangedFile, 0, len(w.changed))
	for _, changed := range w.changed {
		files = append(files, changed)
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].Path < files[j].Path
	})

	return files
}

func (w *Watcher) Close() error {
	return w.watcher.Close()
}

func (w *Watcher) run(ctx context.Context) {
	defer close(w.events)
	timer := time.NewTimer(w.debounce)
	if !timer.Stop() {
		<-timer.C
	}
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			w.flush()
			return
		case event, ok := <-w.watcher.Events:
			if !ok {
				w.flush()
				return
			}
			if w.record(event) {
				resetTimer(timer, w.debounce)
			}
		case <-timer.C:
			w.flush()
		case <-w.watcher.Errors:
		}
	}
}

func (w *Watcher) record(event fsnotify.Event) bool {
	action := actionFor(event.Op)
	if action == "" || shouldIgnore(event.Name) {
		return false
	}
	rel, err := filepath.Rel(w.repoRoot, event.Name)
	if err != nil || strings.HasPrefix(rel, "..") {
		return false
	}
	rel = filepath.ToSlash(rel)
	if event.Op&fsnotify.Create != 0 {
		_ = w.addCreatedDirectory(event.Name)
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	w.pending[rel] |= event.Op
	classification := w.classifier.Classify(rel)
	w.changed[rel] = model.ChangedFile{
		Path:       rel,
		Action:     actionFor(w.pending[rel]),
		Sensitive:  classification.Sensitive,
		Dependency: classification.Dependency,
	}

	return true
}

func (w *Watcher) flush() {
	w.mu.Lock()
	pending := make(map[string]fsnotify.Op, len(w.pending))
	for path, op := range w.pending {
		pending[path] = op
	}
	w.pending = make(map[string]fsnotify.Op)
	w.mu.Unlock()

	paths := make([]string, 0, len(pending))
	for path := range pending {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		classification := w.classifier.Classify(path)
		w.counter++
		event, err := eventlog.Normalize(model.Event{
			EventID:   fmt.Sprintf("evt_fs_%d", w.counter),
			SessionID: w.sessionID,
			Timestamp: time.Now().UTC(),
			Source:    Source,
			Type:      "fs.change",
			CWD:       w.repoRoot,
			Payload: map[string]any{
				"path":       path,
				"action":     actionFor(pending[path]),
				"sensitive":  classification.Sensitive,
				"dependency": classification.Dependency,
				"op":         pending[path].String(),
			},
		})
		if err != nil {
			continue
		}
		w.events <- event
	}
}

func (w *Watcher) watchTree() error {
	return filepath.WalkDir(w.repoRoot, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			return nil
		}
		if shouldIgnore(path) {
			return filepath.SkipDir
		}
		if err := w.watcher.Add(path); err != nil {
			return fmt.Errorf("watch directory %q: %w", path, err)
		}

		return nil
	})
}

func (w *Watcher) addCreatedDirectory(path string) error {
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() || shouldIgnore(path) {
		return err
	}

	return w.watcher.Add(path)
}

func matchesAny(path string, patterns []string) bool {
	for _, pattern := range patterns {
		if matchesPattern(path, filepath.ToSlash(pattern)) {
			return true
		}
	}

	return false
}

func matchesPattern(path string, pattern string) bool {
	if strings.Contains(pattern, "**") {
		prefix := strings.TrimSuffix(strings.Split(pattern, "**")[0], "/")
		return path == prefix || strings.HasPrefix(path, prefix+"/")
	}
	if ok, _ := filepath.Match(pattern, path); ok {
		return true
	}

	return path == pattern || strings.HasSuffix(path, "/"+pattern)
}

func actionFor(op fsnotify.Op) string {
	switch {
	case op&fsnotify.Remove != 0:
		return "delete"
	case op&fsnotify.Rename != 0:
		return "rename"
	case op&fsnotify.Create != 0:
		return "create"
	case op&fsnotify.Write != 0:
		return "modify"
	default:
		return ""
	}
}

func shouldIgnore(path string) bool {
	slash := filepath.ToSlash(path)
	for _, part := range strings.Split(slash, "/") {
		if part == ".git" || part == storage.RootDir {
			return true
		}
	}

	return false
}

func resetTimer(timer *time.Timer, duration time.Duration) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	timer.Reset(duration)
}
