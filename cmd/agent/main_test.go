package main

import (
	"os"
	"path/filepath"
	"testing"
)

// Backing up nested paths requires copyFile to create parent directories.
func TestCopyFileCreatesParentDirs(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "server")
	if err := os.WriteFile(src, []byte("server-binary"), 0644); err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(root, "backup", "assets", "server")
	if err := copyFile(src, dst); err != nil {
		t.Fatalf("copyFile must create parent dirs for backup paths: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "server-binary" {
		t.Fatalf("copied content = %q", got)
	}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

// untarGz can overwrite any path under workDir, not just server/version.
func TestBackupDeploymentCopiesEveryOverwritableFile(t *testing.T) {
	root := t.TempDir()
	backup := filepath.Join(root, "backup_previous")
	writeTestFile(t, filepath.Join(root, "server"), "new-binary")
	writeTestFile(t, filepath.Join(root, "current_version.txt"), "v2")
	writeTestFile(t, filepath.Join(root, "start.sh"), "start")
	writeTestFile(t, filepath.Join(root, "assets", "app.js"), "js")
	writeTestFile(t, filepath.Join(root, "update.tar.gz"), "archive")
	writeTestFile(t, filepath.Join(backup, "stale"), "stale")

	if err := backupDeployment(root, backup); err != nil {
		t.Fatal(err)
	}
	for rel, want := range map[string]string{
		"server":                    "new-binary",
		"current_version.txt":       "v2",
		"start.sh":                  "start",
		filepath.Join("assets", "app.js"): "js",
	} {
		got, err := os.ReadFile(filepath.Join(backup, rel))
		if err != nil {
			t.Errorf("backup missing %s: %v", rel, err)
			continue
		}
		if string(got) != want {
			t.Errorf("backup %s = %q, want %q", rel, got, want)
		}
	}
	if _, err := os.Stat(filepath.Join(backup, "update.tar.gz")); !os.IsNotExist(err) {
		t.Error("downloaded archive must not be backed up")
	}
	if _, err := os.Stat(filepath.Join(backup, "stale")); !os.IsNotExist(err) {
		t.Error("stale backup contents must be wiped before a fresh backup")
	}
}

func TestRestoreBackupRestoresNestedFiles(t *testing.T) {
	root := t.TempDir()
	backup := filepath.Join(root, "backup")
	writeTestFile(t, filepath.Join(backup, "server"), "old-binary")
	writeTestFile(t, filepath.Join(backup, "assets", "app.js"), "old-js")

	if err := restoreBackup(root, backup); err != nil {
		t.Fatal(err)
	}
	for rel, want := range map[string]string{"server": "old-binary", filepath.Join("assets", "app.js"): "old-js"} {
		got, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Errorf("restore missing %s: %v", rel, err)
			continue
		}
		if string(got) != want {
			t.Errorf("restored %s = %q, want %q", rel, got, want)
		}
	}
}

func TestBackupDeploymentFailsOnBadTarget(t *testing.T) {
	root := t.TempDir()
	blocker := filepath.Join(root, "blocker")
	writeTestFile(t, blocker, "not-a-dir")
	if err := backupDeployment(root, filepath.Join(blocker, "backup")); err == nil {
		t.Fatal("backup failure must be returned so the update aborts before stopping the server")
	}
}
