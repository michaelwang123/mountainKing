// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package template

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
	"go.uber.org/zap"
)

// TemplateWatcher watches template directories for file changes and triggers
// template reloading via the TemplateEngine. It uses fsnotify for file system
// notifications with a 500ms debounce to coalesce rapid events.
type TemplateWatcher struct {
	engine  *TemplateEngine
	watcher *fsnotify.Watcher
	logger  *zap.Logger
	done    chan struct{}
}

// NewTemplateWatcher creates a TemplateWatcher that monitors the given
// directories (and all their subdirectories recursively) for file changes.
// Each directory is walked via filepath.WalkDir and every subdirectory is
// added to the fsnotify watcher.
func NewTemplateWatcher(engine *TemplateEngine, dirs []string, logger *zap.Logger) (*TemplateWatcher, error) {
	if logger == nil {
		logger = zap.NewNop()
	}

	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	// Walk each directory recursively and add all subdirectories to the watcher.
	for _, dir := range dirs {
		if err := addDirRecursive(w, dir, logger); err != nil {
			w.Close()
			return nil, err
		}
	}

	return &TemplateWatcher{
		engine:  engine,
		watcher: w,
		logger:  logger,
		done:    make(chan struct{}),
	}, nil
}

// Start launches a goroutine that listens for fsnotify events.
// It uses a 500ms debounce: after receiving an event, it waits 500ms for
// more events before triggering engine.Reload(ctx, false).
// On fsnotify.Create events, if the created path is a directory it is
// dynamically added to the watcher.
func (tw *TemplateWatcher) Start() {
	go tw.watchLoop()
}

// Stop signals the watch goroutine to exit and closes the fsnotify watcher.
func (tw *TemplateWatcher) Stop() error {
	close(tw.done)
	return tw.watcher.Close()
}

// watchLoop is the main event loop that processes fsnotify events with debounce.
func (tw *TemplateWatcher) watchLoop() {
	var debounceTimer *time.Timer

	for {
		select {
		case <-tw.done:
			if debounceTimer != nil {
				debounceTimer.Stop()
			}
			tw.logger.Info("template watcher stopped")
			return

		case event, ok := <-tw.watcher.Events:
			if !ok {
				return
			}

			// If a new directory is created, add it to the watcher so that
			// files created inside it are also monitored.
			if event.Has(fsnotify.Create) {
				tw.maybeAddDirectory(event.Name)
			}

			// Reset the debounce timer on every event. This coalesces rapid
			// file changes (e.g. editor save producing multiple writes) into
			// a single reload after 500ms of quiet.
			if debounceTimer != nil {
				debounceTimer.Stop()
			}
			debounceTimer = time.AfterFunc(500*time.Millisecond, func() {
				tw.triggerReload()
			})

		case err, ok := <-tw.watcher.Errors:
			if !ok {
				return
			}
			tw.logger.Error("template watcher error", zap.Error(err))
		}
	}
}

// triggerReload calls engine.Reload with fromMutation=false (no cooldown).
func (tw *TemplateWatcher) triggerReload() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := tw.engine.Reload(ctx, false)
	if err != nil {
		tw.logger.Error("template watcher reload failed", zap.Error(err))
		return
	}

	tw.logger.Info("template watcher reload complete",
		zap.Int("success_count", result.SuccessCount),
		zap.Int("failure_count", len(result.Failures)),
		zap.Duration("duration", result.Duration),
	)
}

// maybeAddDirectory checks if the path is a directory and adds it to the
// watcher if so. This supports dynamic subdirectory creation.
func (tw *TemplateWatcher) maybeAddDirectory(path string) {
	info, err := os.Stat(path)
	if err != nil {
		return
	}
	if !info.IsDir() {
		return
	}
	if err := tw.watcher.Add(path); err != nil {
		tw.logger.Warn("failed to watch new directory",
			zap.String("path", path),
			zap.Error(err),
		)
	} else {
		tw.logger.Debug("watching new directory", zap.String("path", path))
	}
}

// addDirRecursive walks the directory tree rooted at dir and adds every
// directory to the fsnotify watcher.
func addDirRecursive(w *fsnotify.Watcher, dir string, logger *zap.Logger) error {
	return filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// If the root directory doesn't exist, log a warning and skip.
			if os.IsNotExist(err) && path == dir {
				logger.Warn("template watch directory does not exist, skipping",
					zap.String("path", dir),
				)
				return filepath.SkipDir
			}
			return err
		}
		if d.IsDir() {
			if addErr := w.Add(path); addErr != nil {
				logger.Warn("failed to add directory to watcher",
					zap.String("path", path),
					zap.Error(addErr),
				)
			}
		}
		return nil
	})
}
