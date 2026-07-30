package main

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gamesync/internal/core"
)

type fakeUpdateTimer struct {
	mu      sync.Mutex
	stopped bool
}

func (timer *fakeUpdateTimer) Stop() bool {
	timer.mu.Lock()
	defer timer.mu.Unlock()
	wasActive := !timer.stopped
	timer.stopped = true
	return wasActive
}

func (timer *fakeUpdateTimer) isStopped() bool {
	timer.mu.Lock()
	defer timer.mu.Unlock()
	return timer.stopped
}

func newUpdateMonitorTestApp(t *testing.T) *App {
	t.Helper()
	store, err := core.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	app := NewApp()
	app.store = store
	app.baseDir = store.DataDir()
	t.Cleanup(app.stopUpdateCheckScheduler)
	return app
}

func TestUpdateCheckStateJSONContract(t *testing.T) {
	checkedAt := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	state := core.UpdateCheckState{
		Status:    core.UpdateCheckPhaseSucceeded,
		Result:    &core.UpdateCheckResult{Status: core.UpdateStatusAvailable, LatestVersion: "1.2.3"},
		CheckedAt: &checkedAt,
	}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"status", "result", "checkedAt"} {
		if _, ok := payload[key]; !ok {
			t.Fatalf("missing JSON key %q", key)
		}
	}
}

func TestUpdateMonitorStartsImmediatelyAndSchedulesHourly(t *testing.T) {
	app := newUpdateMonitorTestApp(t)
	checked := make(chan struct{}, 1)
	scheduled := make(chan time.Duration, 1)
	app.updateCheckFn = func(context.Context) (core.UpdateCheckResult, error) {
		checked <- struct{}{}
		return core.UpdateCheckResult{Status: core.UpdateStatusLatest}, nil
	}
	app.updateCheckAfterFn = func(delay time.Duration, _ func()) backgroundTimer {
		scheduled <- delay
		return &fakeUpdateTimer{}
	}

	app.startUpdateCheckScheduler()
	select {
	case <-checked:
	case <-time.After(time.Second):
		t.Fatal("startup update check did not run")
	}
	select {
	case delay := <-scheduled:
		if delay != time.Hour {
			t.Fatalf("scheduled delay = %s", delay)
		}
	case <-time.After(time.Second):
		t.Fatal("next update check was not scheduled")
	}
}

func TestUpdateMonitorDeduplicatesAutomaticAndManualChecks(t *testing.T) {
	app := newUpdateMonitorTestApp(t)
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	app.updateCheckFn = func(context.Context) (core.UpdateCheckResult, error) {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-release
		return core.UpdateCheckResult{Status: core.UpdateStatusAvailable, LatestVersion: "1.2.3"}, nil
	}
	app.updateCheckAfterFn = func(time.Duration, func()) backgroundTimer { return &fakeUpdateTimer{} }
	app.startUpdateCheckScheduler()
	<-started
	manualDone := make(chan error, 1)
	go func() {
		_, err := app.CheckForUpdates()
		manualDone <- err
	}()
	deadline := time.Now().Add(time.Second)
	for {
		app.updateCheckMu.Lock()
		waiters := app.updateCheckRun.waiters
		app.updateCheckMu.Unlock()
		if waiters == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("manual check did not join the active request")
		}
		time.Sleep(time.Millisecond)
	}
	close(release)
	if err := <-manualDone; err != nil {
		t.Fatal(err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("checker calls = %d", got)
	}
}

func TestUpdateMonitorKeepsAvailableResultAfterFailure(t *testing.T) {
	app := newUpdateMonitorTestApp(t)
	app.updateCheckFn = func(context.Context) (core.UpdateCheckResult, error) {
		return core.UpdateCheckResult{Status: core.UpdateStatusAvailable, LatestVersion: "1.2.3"}, nil
	}
	if _, err := app.runUpdateCheck(context.Background()); err != nil {
		t.Fatal(err)
	}
	app.updateCheckFn = func(context.Context) (core.UpdateCheckResult, error) {
		return core.UpdateCheckResult{}, errors.New("offline")
	}
	if _, err := app.runUpdateCheck(context.Background()); err == nil {
		t.Fatal("expected injected failure")
	}
	state := app.currentUpdateCheckState()
	if state.Result == nil || state.Result.Status != core.UpdateStatusAvailable {
		t.Fatalf("cached result after failure = %#v", state.Result)
	}
}

func TestStopUpdateMonitorCancelsScheduledTimer(t *testing.T) {
	app := newUpdateMonitorTestApp(t)
	timer := &fakeUpdateTimer{}
	app.updateCheckMu.Lock()
	app.updateCheckStarted = true
	app.updateCheckTimer = timer
	app.updateCheckMu.Unlock()
	app.stopUpdateCheckScheduler()
	if !timer.isStopped() {
		t.Fatal("scheduled update timer was not stopped")
	}
}
