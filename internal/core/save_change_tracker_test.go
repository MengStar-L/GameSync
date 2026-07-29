package core

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

const testSaveChangeDebounce = 20 * time.Millisecond

func TestSaveChangeTrackerCoalescesWritesAndDeletes(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "slot"), 0o755); err != nil {
		t.Fatal(err)
	}

	tracker, changes := newTestSaveChangeTracker(t)
	if err := tracker.RegisterGame("game-1", root, "rules-1"); err != nil {
		t.Fatalf("register game: %v", err)
	}

	savePath := filepath.Join(root, "slot", "save.dat")
	if err := os.WriteFile(savePath, []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(savePath, []byte("two"), 0o644); err != nil {
		t.Fatal(err)
	}

	change := waitForSaveChange(t, changes, "game-1")
	assertDirtyPaths(t, change, "slot/save.dat")

	if err := os.Remove(savePath); err != nil {
		t.Fatal(err)
	}
	change = waitForSaveChange(t, changes, "game-1")
	assertDirtyPaths(t, change, "slot/save.dat")
}

func TestSaveChangeTrackerWatchesNewSubdirectories(t *testing.T) {
	root := t.TempDir()
	tracker, changes := newTestSaveChangeTracker(t)
	if err := tracker.RegisterGame("game-1", root, "rules-1"); err != nil {
		t.Fatalf("register game: %v", err)
	}

	newDirectory := filepath.Join(root, "profile", "nested")
	if err := os.MkdirAll(newDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	directoryChange := waitForSaveChange(t, changes, "game-1")
	if !directoryChange.Rebuild {
		t.Fatalf("new directory should conservatively rebuild: %+v", directoryChange)
	}

	filePath := filepath.Join(newDirectory, "save.dat")
	if err := os.WriteFile(filePath, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	change := waitForSaveChange(t, changes, "game-1")
	assertDirtyPaths(t, change, "profile/nested/save.dat")
}

func TestSaveChangeTrackerUpdatesAndUnregistersGames(t *testing.T) {
	firstRoot := t.TempDir()
	secondRoot := t.TempDir()
	tracker, changes := newTestSaveChangeTracker(t)

	if err := tracker.RegisterGame("game-1", firstRoot, "rules-1"); err != nil {
		t.Fatalf("register game: %v", err)
	}
	if err := tracker.RegisterGame("game-1", firstRoot, "rules-1"); err != nil {
		t.Fatalf("idempotent register: %v", err)
	}
	assertNoSaveChange(t, changes, 3*testSaveChangeDebounce)

	if err := tracker.UpdateGame("game-1", firstRoot, "rules-2"); err != nil {
		t.Fatalf("update rules: %v", err)
	}
	if change := waitForSaveChange(t, changes, "game-1"); !change.Rebuild {
		t.Fatalf("rule change should rebuild: %+v", change)
	}

	if err := tracker.UpdateGame("game-1", secondRoot, "rules-2"); err != nil {
		t.Fatalf("update path: %v", err)
	}
	if change := waitForSaveChange(t, changes, "game-1"); !change.Rebuild {
		t.Fatalf("path change should rebuild: %+v", change)
	}

	if err := os.WriteFile(filepath.Join(firstRoot, "old.dat"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	assertNoSaveChange(t, changes, 3*testSaveChangeDebounce)

	if err := os.WriteFile(filepath.Join(secondRoot, "new.dat"), []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	assertDirtyPaths(t, waitForSaveChange(t, changes, "game-1"), "new.dat")

	if err := tracker.UnregisterGame("game-1"); err != nil {
		t.Fatalf("unregister game: %v", err)
	}
	if err := tracker.UnregisterGame("game-1"); err != nil {
		t.Fatalf("idempotent unregister: %v", err)
	}
	if err := os.WriteFile(filepath.Join(secondRoot, "after.dat"), []byte("after"), 0o644); err != nil {
		t.Fatal(err)
	}
	assertNoSaveChange(t, changes, 3*testSaveChangeDebounce)
}

func TestSaveChangeTrackerMarksDirectoryRemovalAsRebuild(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "profile")
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatal(err)
	}

	tracker, changes := newTestSaveChangeTracker(t)
	if err := tracker.RegisterGame("game-1", root, "rules-1"); err != nil {
		t.Fatalf("register game: %v", err)
	}
	if err := os.Remove(directory); err != nil {
		t.Fatal(err)
	}

	change := waitForSaveChange(t, changes, "game-1")
	if !change.Rebuild {
		t.Fatalf("directory removal should rebuild: %+v", change)
	}
}

func TestSaveChangeTrackerSharesOverlappingDirectories(t *testing.T) {
	outerRoot := t.TempDir()
	innerRoot := filepath.Join(outerRoot, "profile")
	if err := os.Mkdir(innerRoot, 0o755); err != nil {
		t.Fatal(err)
	}

	tracker, changes := newTestSaveChangeTracker(t)
	if err := tracker.RegisterGame("outer", outerRoot, "rules"); err != nil {
		t.Fatalf("register outer game: %v", err)
	}
	if err := tracker.RegisterGame("inner", innerRoot, "rules"); err != nil {
		t.Fatalf("register inner game: %v", err)
	}

	if err := os.WriteFile(filepath.Join(innerRoot, "shared.dat"), []byte("shared"), 0o644); err != nil {
		t.Fatal(err)
	}
	first := waitForSaveChange(t, changes, "")
	second := waitForSaveChange(t, changes, "")
	byGame := map[string]SaveChangeEvent{first.GameID: first, second.GameID: second}
	assertDirtyPaths(t, byGame["outer"], "profile/shared.dat")
	assertDirtyPaths(t, byGame["inner"], "shared.dat")

	if err := tracker.UnregisterGame("outer"); err != nil {
		t.Fatalf("unregister outer game: %v", err)
	}
	if err := os.WriteFile(filepath.Join(innerRoot, "inner.dat"), []byte("inner"), 0o644); err != nil {
		t.Fatal(err)
	}
	change := waitForSaveChange(t, changes, "inner")
	assertDirtyPaths(t, change, "inner.dat")
}

func TestSaveChangeTrackerMarksWatcherErrorsAsRebuild(t *testing.T) {
	firstRoot := t.TempDir()
	secondRoot := t.TempDir()
	tracker, changes := newTestSaveChangeTracker(t)
	if err := tracker.RegisterGame("game-1", firstRoot, "rules"); err != nil {
		t.Fatalf("register first game: %v", err)
	}
	if err := tracker.RegisterGame("game-2", secondRoot, "rules"); err != nil {
		t.Fatalf("register second game: %v", err)
	}

	select {
	case tracker.watcher.Errors <- errors.New("synthetic watcher error"):
	case <-time.After(3 * time.Second):
		t.Fatal("timed out injecting watcher error")
	}

	first := waitForSaveChange(t, changes, "")
	second := waitForSaveChange(t, changes, "")
	if !first.Rebuild || !second.Rebuild {
		t.Fatalf("watcher error should rebuild every game: %+v, %+v", first, second)
	}
	got := []string{first.GameID, second.GameID}
	slices.Sort(got)
	if !slices.Equal(got, []string{"game-1", "game-2"}) {
		t.Fatalf("watcher error affected games %v", got)
	}
}

func TestSaveChangeTrackerMissingDirectoryRebuildsAndCloseStopsLoop(t *testing.T) {
	tracker, changes := newTestSaveChangeTracker(t)
	missingRoot := filepath.Join(t.TempDir(), "missing")
	if err := tracker.RegisterGame("game-1", missingRoot, "rules"); err == nil {
		t.Fatal("registering a missing directory should fail")
	}
	if change := waitForSaveChange(t, changes, "game-1"); !change.Rebuild {
		t.Fatalf("missing directory should rebuild: %+v", change)
	}

	if err := tracker.Close(); err != nil {
		t.Fatalf("close tracker: %v", err)
	}
	select {
	case <-tracker.done:
	case <-time.After(3 * time.Second):
		t.Fatal("tracker loop did not terminate")
	}
	if err := tracker.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
	if err := tracker.RegisterGame("game-2", t.TempDir(), "rules"); !errors.Is(err, ErrSaveChangeTrackerClosed) {
		t.Fatalf("register after close error = %v", err)
	}
}

func newTestSaveChangeTracker(t *testing.T) (*SaveChangeTracker, <-chan SaveChangeEvent) {
	t.Helper()
	changes := make(chan SaveChangeEvent, 32)
	tracker, err := newSaveChangeTracker(testSaveChangeDebounce, func(change SaveChangeEvent) {
		changes <- change
	})
	if err != nil {
		t.Fatalf("new save change tracker: %v", err)
	}
	t.Cleanup(func() {
		if err := tracker.Close(); err != nil {
			t.Errorf("close save change tracker: %v", err)
		}
	})
	return tracker, changes
}

func waitForSaveChange(t *testing.T, changes <-chan SaveChangeEvent, gameID string) SaveChangeEvent {
	t.Helper()
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	for {
		select {
		case change := <-changes:
			if gameID == "" || change.GameID == gameID {
				return change
			}
		case <-timer.C:
			t.Fatalf("timed out waiting for save change for %q", gameID)
		}
	}
}

func assertNoSaveChange(t *testing.T, changes <-chan SaveChangeEvent, timeout time.Duration) {
	t.Helper()
	select {
	case change := <-changes:
		t.Fatalf("unexpected save change: %+v", change)
	case <-time.After(timeout):
	}
}

func assertDirtyPaths(t *testing.T, change SaveChangeEvent, want ...string) {
	t.Helper()
	if change.Rebuild {
		t.Fatalf("expected dirty paths, got rebuild: %+v", change)
	}
	if !slices.Equal(change.DirtyPaths, want) {
		t.Fatalf("dirty paths = %v, want %v", change.DirtyPaths, want)
	}
}
