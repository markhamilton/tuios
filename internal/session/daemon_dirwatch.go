package session

import (
	"fmt"
	"path/filepath"
	"sync"

	"github.com/Gaurav-Gosain/tuios/internal/dirwatch"
)

// Watching a listed folder for a client that cannot watch it itself.
//
// The rail's files section keeps its listing true by asking the kernel when
// the listed folder's names change (app/sidebar_files_watch.go). A client
// that asks its daemon, with MsgWatchDir, gets MsgDirChanged pushed when the
// names change.

// dirWatchSlot is a connection's one folder watch.
type dirWatchSlot struct {
	mu   sync.Mutex
	done chan struct{}
}

// replace ends the current watch and returns the done channel of a new one, or
// ends it and returns nil.
func (w *dirWatchSlot) replace(start bool) chan struct{} {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.done != nil {
		close(w.done)
		w.done = nil
	}
	if start {
		w.done = make(chan struct{})
	}
	return w.done
}

// stopDirWatch ends the connection's folder watch, when it closes.
func (cs *connState) stopDirWatch() { cs.dirWatch.replace(false) }

// handleWatchDir points the connection's folder watch at the folder named, or
// ends it for an empty one.
func (d *Daemon) handleWatchDir(cs *connState, msg *Message) error {
	var p WatchDirPayload
	if err := msg.ParsePayload(&p); err != nil {
		return fmt.Errorf("invalid watch-dir payload: %w", err)
	}
	if p.Dir == "" || !filepath.IsAbs(p.Dir) {
		cs.dirWatch.replace(false)
		return nil
	}
	dir := filepath.Clean(p.Dir)

	done := cs.dirWatch.replace(true)
	notify := func() {
		select {
		case <-done:
			// Replaced while the change settled: the client has moved on.
		default:
			_ = d.sendMessage(cs, MsgDirChanged, &DirChangedPayload{Dir: dir})
		}
	}
	// Off the connection's own goroutine, because starting a watch is a
	// syscall on a path that can be a mount that stopped answering.
	go d.runLocalDirWatch(dir, done, notify)
	return nil
}

// runLocalDirWatch reports each settled change to dir's names until done.
func (d *Daemon) runLocalDirWatch(dir string, done <-chan struct{}, notify func()) {
	events := make(chan struct{}, 1)
	w, err := dirwatch.Watch(dir, func() { dirwatch.Nudge(events) })
	if err != nil {
		return
	}
	defer w.Close()
	for {
		select {
		case <-done:
			return
		case <-d.ctx.Done():
			return
		case <-events:
		}
		if !dirwatch.AfterBurst(events, done) {
			return
		}
		notify()
	}
}
