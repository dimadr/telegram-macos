package main

import (
	"archive/tar"
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func contained(root, name string) (string, error) {
	if !filepath.IsAbs(name) {
		name = filepath.Join(root, name)
	}
	name = filepath.Clean(name)
	relative, err := filepath.Rel(root, name)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("cache entry outside workspace: %s", name)
	}
	return name, nil
}

func sparseFile(reader io.Reader, name string, header *tar.Header, buffer, zero []byte) (err error) {
	if existing, statErr := os.Lstat(name); statErr == nil && existing.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing to overwrite cache symlink: %s", name)
	}
	output, err := os.OpenFile(name, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := output.Close(); err == nil {
			err = closeErr
		}
	}()
	for remaining := header.Size; remaining > 0; {
		length := min(remaining, int64(len(buffer)))
		data := buffer[:length]
		if _, err = io.ReadFull(reader, data); err != nil {
			return err
		}
		if bytes.Equal(data, zero[:length]) {
			_, err = output.Seek(length, io.SeekCurrent)
		} else {
			_, err = output.Write(data)
		}
		if err != nil {
			return err
		}
		remaining -= length
	}
	if err = output.Truncate(header.Size); err != nil {
		return err
	}
	if err = output.Chmod(header.FileInfo().Mode().Perm()); err != nil {
		return err
	}
	return os.Chtimes(name, header.ModTime, header.ModTime)
}

func extract() error {
	root, err := os.Getwd()
	if err != nil {
		return err
	}
	archive, program := "", ""
	arguments := os.Args[1:]
	for index := 0; index < len(arguments); index++ {
		argument := arguments[index]
		switch argument {
		case "-xf", "-f", "--file", "-C", "--use-compress-program":
			index++
			if index == len(arguments) {
				return fmt.Errorf("missing argument for %s", argument)
			}
			switch argument {
			case "-C":
				root = arguments[index]
			case "--use-compress-program":
				program = arguments[index]
			default:
				archive = arguments[index]
			}
		case "-P", "--extract", "--delay-directory-restore":
		default:
			return fmt.Errorf("unsupported cache extraction argument: %s", argument)
		}
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return err
	}
	input, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer input.Close()
	var reader io.Reader = input
	var decoder *exec.Cmd
	if program != "" {
		command := strings.Fields(program)
		decoder = exec.Command(command[0], command[1:]...)
		decoder.Stdin, decoder.Stderr = input, os.Stderr
		reader, err = decoder.StdoutPipe()
		if err != nil {
			return err
		}
		if err = decoder.Start(); err != nil {
			return err
		}
		defer decoder.Process.Kill()
	}
	type directory struct {
		name     string
		mode     os.FileMode
		modified time.Time
	}
	var directories []directory
	buffer := make([]byte, 1024*1024)
	zero := make([]byte, len(buffer))
	tarReader := tar.NewReader(reader)
	for {
		header, nextErr := tarReader.Next()
		if nextErr == io.EOF {
			break
		}
		if nextErr != nil {
			return nextErr
		}
		name, pathErr := contained(root, header.Name)
		if pathErr != nil {
			return pathErr
		}
		if err = os.MkdirAll(filepath.Dir(name), 0755); err != nil {
			return err
		}
		parent, parentErr := filepath.EvalSymlinks(filepath.Dir(name))
		if parentErr != nil {
			return parentErr
		}
		if _, err = contained(root, parent); err != nil {
			return err
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err = os.MkdirAll(name, 0755); err != nil {
				return err
			}
			directories = append(directories, directory{name, header.FileInfo().Mode().Perm(), header.ModTime})
		case tar.TypeReg, tar.TypeRegA, tar.TypeGNUSparse:
			if err = sparseFile(tarReader, name, header, buffer, zero); err != nil {
				return err
			}
		case tar.TypeSymlink, tar.TypeLink:
			target := header.Linkname
			if header.Typeflag == tar.TypeSymlink && !filepath.IsAbs(target) {
				target = filepath.Join(filepath.Dir(name), target)
			}
			if target, err = contained(root, target); err != nil {
				return err
			}
			if err = os.Remove(name); err != nil && !os.IsNotExist(err) {
				return err
			}
			if header.Typeflag == tar.TypeSymlink {
				err = os.Symlink(header.Linkname, name)
			} else {
				err = os.Link(target, name)
			}
			if err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported cache entry type %d: %s", header.Typeflag, header.Name)
		}
	}
	for index := len(directories) - 1; index >= 0; index-- {
		entry := directories[index]
		if err = os.Chmod(entry.name, entry.mode); err != nil {
			return err
		}
		if err = os.Chtimes(entry.name, entry.modified, entry.modified); err != nil {
			return err
		}
	}
	if decoder != nil {
		if _, err = io.Copy(io.Discard, reader); err != nil {
			return err
		}
		return decoder.Wait()
	}
	return nil
}

func main() {
	if err := extract(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
