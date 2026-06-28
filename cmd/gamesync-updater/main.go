package main

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type installFile struct {
	source string
	target string
	backup string
}

func main() {
	pid := flag.Int("pid", 0, "main process id")
	archive := flag.String("archive", "", "update archive path")
	targetDir := flag.String("target-dir", "", "target app directory")
	exePath := flag.String("exe", "", "app executable path")
	expectedHash := flag.String("sha256", "", "expected archive sha256")
	logPath := flag.String("log", "", "updater log path")
	flag.Parse()

	if err := runUpdate(*pid, *archive, *targetDir, *exePath, *expectedHash, *logPath); err != nil {
		writeLog(*logPath, "update failed: "+err.Error())
		os.Exit(1)
	}
}

func runUpdate(pid int, archivePath string, targetDir string, exePath string, expectedHash string, logPath string) error {
	writeLog(logPath, "updater started")
	if err := waitForProcessExit(pid, 90*time.Second); err != nil {
		return err
	}
	if err := verifyFileSHA256(archivePath, expectedHash); err != nil {
		return err
	}
	updateRoot := filepath.Join(targetDir, ".gamesync-update")
	stagingDir := filepath.Join(updateRoot, "staging-"+strconv.FormatInt(time.Now().UnixNano(), 10))
	rollbackDir := filepath.Join(updateRoot, "rollback-"+strconv.FormatInt(time.Now().UnixNano(), 10))
	defer os.RemoveAll(stagingDir)
	if err := os.MkdirAll(stagingDir, 0o755); err != nil {
		return fmt.Errorf("create staging dir: %w", err)
	}
	if err := os.MkdirAll(rollbackDir, 0o755); err != nil {
		return fmt.Errorf("create rollback dir: %w", err)
	}
	if err := extractZipSafe(archivePath, stagingDir); err != nil {
		return err
	}
	installed, err := installStagedFiles(stagingDir, targetDir, rollbackDir)
	if err != nil {
		_ = restoreRollback(installed)
		return err
	}
	writeLog(logPath, "files replaced")
	command := exec.Command(exePath)
	if err := command.Start(); err != nil {
		return fmt.Errorf("restart app: %w", err)
	}
	writeLog(logPath, "app restarted")
	return nil
}

func waitForProcessExit(pid int, timeout time.Duration) error {
	if pid <= 0 {
		return nil
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		running, err := processRunning(pid)
		if err != nil {
			return err
		}
		if !running {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("process %d did not exit before timeout", pid)
}

func processRunning(pid int) (bool, error) {
	if runtime.GOOS == "windows" {
		output, err := exec.Command("tasklist", "/FI", fmt.Sprintf("PID eq %d", pid), "/NH").CombinedOutput()
		if err != nil {
			return false, fmt.Errorf("check process: %w", err)
		}
		text := strings.ToLower(string(output))
		return strings.Contains(text, strconv.Itoa(pid)) && !strings.Contains(text, "no tasks"), nil
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false, nil
	}
	err = process.Signal(os.Signal(nil))
	return err == nil, nil
}

func verifyFileSHA256(path string, expected string) error {
	expected = strings.ToLower(strings.TrimSpace(expected))
	if expected == "" {
		return errors.New("expected sha256 is empty")
	}
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open archive: %w", err)
	}
	defer file.Close()
	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return fmt.Errorf("hash archive: %w", err)
	}
	actual := hex.EncodeToString(hasher.Sum(nil))
	if actual != expected {
		return fmt.Errorf("archive sha256 mismatch")
	}
	return nil
}

func extractZipSafe(archivePath string, destination string) error {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("open update zip: %w", err)
	}
	defer reader.Close()
	for _, file := range reader.File {
		targetPath, err := safeJoin(destination, file.Name)
		if err != nil {
			return err
		}
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(targetPath, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			return err
		}
		src, err := file.Open()
		if err != nil {
			return err
		}
		dst, err := os.Create(targetPath)
		if err != nil {
			_ = src.Close()
			return err
		}
		_, copyErr := io.Copy(dst, src)
		closeErr := dst.Close()
		_ = src.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

func installStagedFiles(stagingDir string, targetDir string, rollbackDir string) ([]installFile, error) {
	var installed []installFile
	err := filepath.WalkDir(stagingDir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		relPath, err := filepath.Rel(stagingDir, path)
		if err != nil {
			return err
		}
		targetPath, err := safeJoin(targetDir, relPath)
		if err != nil {
			return err
		}
		backupPath, err := safeJoin(rollbackDir, relPath)
		if err != nil {
			return err
		}
		record := installFile{source: path, target: targetPath, backup: backupPath}
		if _, err := os.Stat(targetPath); err == nil {
			if err := copyFile(targetPath, backupPath); err != nil {
				return fmt.Errorf("backup %s: %w", relPath, err)
			}
			installed = append(installed, record)
		}
		if err := copyFile(path, targetPath); err != nil {
			return fmt.Errorf("replace %s: %w", relPath, err)
		}
		if record.backup == "" {
			installed = append(installed, record)
		}
		return nil
	})
	if err != nil {
		return installed, err
	}
	return installed, nil
}

func restoreRollback(files []installFile) error {
	var messages []string
	for _, file := range files {
		if strings.TrimSpace(file.backup) == "" {
			continue
		}
		if _, err := os.Stat(file.backup); err != nil {
			continue
		}
		if err := copyFile(file.backup, file.target); err != nil {
			messages = append(messages, err.Error())
		}
	}
	if len(messages) > 0 {
		return errors.New(strings.Join(messages, "; "))
	}
	return nil
}

func safeJoin(root string, relPath string) (string, error) {
	root = strings.TrimSpace(root)
	relPath = filepath.ToSlash(strings.TrimSpace(relPath))
	if root == "" || relPath == "" || strings.HasPrefix(relPath, "/") {
		return "", fmt.Errorf("unsafe path: %s", relPath)
	}
	nativeRel := filepath.FromSlash(relPath)
	if filepath.IsAbs(nativeRel) {
		return "", fmt.Errorf("unsafe path: %s", relPath)
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	targetAbs, err := filepath.Abs(filepath.Join(rootAbs, nativeRel))
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(rootAbs, targetAbs)
	if err != nil {
		return "", err
	}
	if relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return "", fmt.Errorf("unsafe path: %s", relPath)
	}
	return targetAbs, nil
}

func copyFile(source string, destination string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	tempPath := destination + ".tmp"
	output, err := os.Create(tempPath)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil {
		_ = os.Remove(tempPath)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tempPath)
		return closeErr
	}
	_ = os.Remove(destination)
	if err := os.Rename(tempPath, destination); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	return nil
}

func writeLog(path string, message string) {
	path = strings.TrimSpace(path)
	if path == "" {
		return
	}
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer file.Close()
	_, _ = fmt.Fprintf(file, "%s %s\n", time.Now().Format(time.RFC3339), message)
}
