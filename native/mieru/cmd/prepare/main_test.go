package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestPinnedPatchIdempotenceAndDriftRejection(t *testing.T) {
	command := exec.Command("go", "env", "GOMODCACHE")
	output, err := command.Output()
	if err != nil {
		t.Fatal(err)
	}
	cache := string(output)
	for len(cache) > 0 && (cache[len(cache)-1] == '\n' || cache[len(cache)-1] == '\r') {
		cache = cache[:len(cache)-1]
	}
	source := filepath.Join(cache, "github.com", "enfein", "mieru", "v3@"+moduleVersion)
	directory := t.TempDir()
	for path := range originalHashes {
		data, err := os.ReadFile(filepath.Join(source, filepath.FromSlash(path)))
		if err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(directory, filepath.FromSlash(path))
		if err = os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(target, data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	patchPath, err := filepath.Abs(filepath.Join("..", "..", "patches", "mieru-lifecycle.patch"))
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(patchPath)
	if err != nil {
		t.Fatal(err)
	}
	patchHash := digest(data)
	if err = applyPatch(directory, patchPath, patchHash); err != nil {
		t.Fatal(err)
	}
	stampPath := filepath.Join(directory, ".telegramita-source.json")
	before, err := os.Stat(stampPath)
	if err != nil {
		t.Fatal(err)
	}
	if err = applyPatch(directory, patchPath, patchHash); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(stampPath)
	if err != nil || !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("idempotent patch application changed source")
	}
	if err = os.WriteFile(filepath.Join(directory, "apis", "client", "client.go"), []byte("upstream drift"), 0644); err != nil {
		t.Fatal(err)
	}
	if err = applyPatch(directory, patchPath, patchHash); err == nil {
		t.Fatal("modified patched source accepted")
	}
	if err = os.Remove(stampPath); err != nil {
		t.Fatal(err)
	}
	if err = applyPatch(directory, patchPath, patchHash); err == nil {
		t.Fatal("modified upstream source accepted")
	}
}
