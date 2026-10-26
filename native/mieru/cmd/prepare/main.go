package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const modulePath = "github.com/enfein/mieru/v3"
const moduleVersion = "v3.39.0"
const moduleSum = "h1:22WJN9i7I4FrY1cTAVp5KfKOFm4sWrXVOXhqf5fkdr8="

var originalHashes = map[string]string{
	"apis/client/client.go":       "8ad71d11043bb9c4ccc1572e223b8e812dd5790049d7e69d2f30b7b7cdce1abb",
	"apis/internal/early_conn.go": "6f36862be81571ff336559011ab2f8d7ca38d50561171ef2ea7adde967d84111",
}

type sourceStamp struct {
	Patch  string
	Module string
	Files  map[string]string
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func fileDigest(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return digest(data), nil
}

func gitApply(directory, patchPath string, check bool) error {
	args := []string{"apply"}
	if check {
		args = append(args, "--check")
	}
	args = append(args, patchPath)
	command := exec.Command("git", args...)
	command.Dir = directory
	for _, entry := range os.Environ() {
		name := strings.SplitN(entry, "=", 2)[0]
		if name != "GIT_DIR" && name != "GIT_WORK_TREE" && name != "GIT_INDEX_FILE" && name != "GIT_CEILING_DIRECTORIES" {
			command.Env = append(command.Env, entry)
		}
	}
	command.Env = append(command.Env, "GIT_CEILING_DIRECTORIES="+filepath.ToSlash(filepath.Dir(directory)))
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("Mieru lifecycle patch rejected: %s", strings.TrimSpace(string(output)))
	}
	return nil
}

func applyPatch(directory, patchPath, patchHash string) error {
	stampPath := filepath.Join(directory, ".telegramita-source.json")
	if data, err := os.ReadFile(stampPath); err == nil {
		var stamp sourceStamp
		if json.Unmarshal(data, &stamp) != nil || stamp.Patch != patchHash || stamp.Module != moduleSum || len(stamp.Files) != len(originalHashes) {
			return errors.New("Mieru source stamp mismatch")
		}
		for path, expected := range stamp.Files {
			if _, valid := originalHashes[path]; !valid {
				return errors.New("Mieru source stamp contains unexpected path")
			}
			if hash, err := fileDigest(filepath.Join(directory, filepath.FromSlash(path))); err != nil || hash != expected || hash == originalHashes[path] {
				return errors.New("patched Mieru source changed")
			}
		}
		return nil
	}
	for path, expected := range originalHashes {
		if hash, err := fileDigest(filepath.Join(directory, filepath.FromSlash(path))); err != nil || hash != expected {
			return fmt.Errorf("Mieru %s source checksum mismatch: %s", moduleVersion, path)
		}
	}
	if err := gitApply(directory, patchPath, true); err != nil {
		return err
	}
	if err := gitApply(directory, patchPath, false); err != nil {
		return err
	}
	stamp := sourceStamp{Patch: patchHash, Module: moduleSum, Files: make(map[string]string)}
	for path := range originalHashes {
		hash, err := fileDigest(filepath.Join(directory, filepath.FromSlash(path)))
		if err != nil {
			return err
		}
		if hash == originalHashes[path] {
			return errors.New("Mieru lifecycle patch was not applied")
		}
		stamp.Files[path] = hash
	}
	data, err := json.Marshal(stamp)
	if err != nil {
		return err
	}
	return os.WriteFile(stampPath, data, 0600)
}

func copyTree(source, destination string) error {
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkError error) error {
		if walkError != nil {
			return walkError
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		if !entry.Type().IsRegular() {
			return errors.New("unexpected file type in Mieru module")
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0644)
	})
}

func prepare() (string, error) {
	root, err := os.Getwd()
	if err != nil {
		return "", err
	}
	patchPath := filepath.Join(root, "patches", "mieru-lifecycle.patch")
	patchData, err := os.ReadFile(patchPath)
	if err != nil {
		return "", err
	}
	fixture, err := os.ReadFile(filepath.Join(root, "patches", "lifecycle_test.go.txt"))
	if err != nil {
		return "", err
	}
	patchHash := digest(append(patchData, fixture...))
	command := exec.Command("go", "mod", "download", "-json", modulePath+"@"+moduleVersion)
	output, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("download pinned Mieru module: %w", err)
	}
	var module struct{ Dir, Version, Sum string }
	if json.Unmarshal(output, &module) != nil || module.Version != moduleVersion || module.Sum != moduleSum || module.Dir == "" {
		return "", errors.New("Mieru module version or checksum mismatch")
	}
	source := filepath.Join(root, "build", "mieru-"+moduleVersion+"-"+patchHash[:12])
	if _, err := os.Stat(source); errors.Is(err, os.ErrNotExist) {
		if err = os.MkdirAll(source, 0755); err != nil {
			return "", err
		}
		for _, name := range []string{"apis", "pkg", "internal"} {
			if err = copyTree(filepath.Join(module.Dir, name), filepath.Join(source, name)); err != nil {
				return "", err
			}
		}
		for _, name := range []string{"go.mod", "go.sum", "LICENSE"} {
			data, readError := os.ReadFile(filepath.Join(module.Dir, name))
			if readError != nil {
				return "", readError
			}
			if err = os.WriteFile(filepath.Join(source, name), data, 0644); err != nil {
				return "", err
			}
		}
	}
	if err = applyPatch(source, patchPath, patchHash); err != nil {
		return "", err
	}
	if err = os.WriteFile(filepath.Join(source, "apis", "client", "telegramita_lifecycle_test.go"), fixture, 0644); err != nil {
		return "", err
	}
	modfile := filepath.Join(root, "build", "patched.mod")
	for _, name := range []string{"go.mod", "go.sum"} {
		data, readError := os.ReadFile(filepath.Join(root, name))
		if readError != nil {
			return "", readError
		}
		extension := filepath.Ext(name)
		if err = os.WriteFile(strings.TrimSuffix(modfile, ".mod")+extension, data, 0644); err != nil {
			return "", err
		}
	}
	command = exec.Command("go", "mod", "edit", "-modfile="+modfile, "-replace="+modulePath+"="+filepath.ToSlash(source))
	if err = command.Run(); err != nil {
		return "", err
	}
	return modfile, nil
}

func main() {
	modfile, err := prepare()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(modfile)
}
