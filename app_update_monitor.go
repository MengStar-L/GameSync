package main

import (
	"context"
	"time"

	"gamesync/internal/core"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

const updateCheckInterval = time.Hour

type updateCheckRun struct {
	done    chan struct{}
	result  core.UpdateCheckResult
	err     error
	waiters int
}

func (a *App) startUpdateCheckScheduler() {
	a.updateCheckMu.Lock()
	if a.updateCheckStarted && !a.updateCheckStopped {
		a.updateCheckMu.Unlock()
		return
	}
	a.updateCheckStarted = true
	a.updateCheckStopped = false
	if a.updateCheckTimer != nil {
		a.updateCheckTimer.Stop()
		a.updateCheckTimer = nil
	}
	a.updateCheckMu.Unlock()
	go a.executeAutomaticUpdateCheck()
}

func (a *App) stopUpdateCheckScheduler() {
	a.updateCheckMu.Lock()
	defer a.updateCheckMu.Unlock()
	a.updateCheckStarted = false
	a.updateCheckStopped = true
	if a.updateCheckTimer != nil {
		a.updateCheckTimer.Stop()
		a.updateCheckTimer = nil
	}
}

func (a *App) executeAutomaticUpdateCheck() {
	if _, err := a.runUpdateCheck(a.syncContext()); err != nil {
		wailsruntime.LogErrorf(a.ctx, "automatic update check failed: %v", err)
	}
	a.scheduleNextUpdateCheck()
}

func (a *App) scheduleNextUpdateCheck() {
	a.updateCheckMu.Lock()
	defer a.updateCheckMu.Unlock()
	if !a.updateCheckStarted || a.updateCheckStopped {
		return
	}
	if a.updateCheckTimer != nil {
		a.updateCheckTimer.Stop()
	}
	afterFn := a.updateCheckAfterFn
	if afterFn == nil {
		afterFn = func(delay time.Duration, callback func()) backgroundTimer {
			return time.AfterFunc(delay, callback)
		}
	}
	a.updateCheckTimer = afterFn(updateCheckInterval, func() {
		a.updateCheckMu.Lock()
		if !a.updateCheckStarted || a.updateCheckStopped {
			a.updateCheckMu.Unlock()
			return
		}
		a.updateCheckTimer = nil
		a.updateCheckMu.Unlock()
		go a.executeAutomaticUpdateCheck()
	})
}

func (a *App) runUpdateCheck(ctx context.Context) (core.UpdateCheckResult, error) {
	if err := a.ensureReady(); err != nil {
		return core.UpdateCheckResult{}, err
	}

	a.updateCheckMu.Lock()
	if active := a.updateCheckRun; active != nil {
		active.waiters++
		a.updateCheckMu.Unlock()
		select {
		case <-active.done:
			return active.result, active.err
		case <-ctx.Done():
			return core.UpdateCheckResult{}, ctx.Err()
		}
	}

	run := &updateCheckRun{done: make(chan struct{})}
	a.updateCheckRun = run
	checkingState := cloneUpdateCheckState(a.updateCheckState)
	checkingState.Status = core.UpdateCheckPhaseChecking
	a.updateCheckState = checkingState
	a.updateCheckMu.Unlock()
	a.emitRuntimeEvent("update:check_state", checkingState)

	checkFn := a.updateCheckFn
	if checkFn == nil {
		checkFn = func(checkContext context.Context) (core.UpdateCheckResult, error) {
			updater := core.NewUpdater(core.UpdateOptions{DataDir: a.store.DataDir()})
			return updater.Check(checkContext)
		}
	}
	result, err := checkFn(ctx)

	a.updateCheckMu.Lock()
	if err == nil {
		checkedAt := time.Now()
		resultCopy := result
		a.updateCheckState = core.UpdateCheckState{
			Status:    core.UpdateCheckPhaseSucceeded,
			Result:    &resultCopy,
			CheckedAt: &checkedAt,
		}
	} else if a.updateCheckState.Result != nil {
		a.updateCheckState.Status = core.UpdateCheckPhaseSucceeded
	} else {
		a.updateCheckState.Status = core.UpdateCheckPhaseIdle
	}
	finalState := cloneUpdateCheckState(a.updateCheckState)
	run.result = result
	run.err = err
	a.updateCheckRun = nil
	close(run.done)
	a.updateCheckMu.Unlock()

	a.emitRuntimeEvent("update:check_state", finalState)
	return result, err
}

func cloneUpdateCheckState(state core.UpdateCheckState) core.UpdateCheckState {
	cloned := state
	if state.Result != nil {
		result := *state.Result
		cloned.Result = &result
	}
	if state.CheckedAt != nil {
		checkedAt := *state.CheckedAt
		cloned.CheckedAt = &checkedAt
	}
	if cloned.Status == "" {
		cloned.Status = core.UpdateCheckPhaseIdle
	}
	return cloned
}

func (a *App) currentUpdateCheckState() core.UpdateCheckState {
	a.updateCheckMu.Lock()
	defer a.updateCheckMu.Unlock()
	return cloneUpdateCheckState(a.updateCheckState)
}

func (a *App) GetUpdateCheckState() (core.UpdateCheckState, error) {
	if err := a.ensureReady(); err != nil {
		return core.UpdateCheckState{}, err
	}
	return a.currentUpdateCheckState(), nil
}
