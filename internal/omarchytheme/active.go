// Package omarchytheme reads Omarchy's native theme state without changing it.
package omarchytheme

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type Active struct {
	Root       string
	ID         string
	Generation string
	IntroToken string
}

func ReadActive(home string) (Active, error) {
	current := filepath.Join(home, ".local", "state", "omarchy", "current")
	root := filepath.Join(current, "theme")
	if _, err := os.Lstat(current); errors.Is(err, os.ErrNotExist) {
		current = filepath.Join(home, ".config", "omarchy", "current")
		root = filepath.Join(current, "theme")
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return Active{}, fmt.Errorf("could not read the active Omarchy theme")
	}
	id := readMarker(filepath.Join(current, "theme.name"))
	if id == "" {
		target, err := os.Readlink(root)
		if err == nil {
			id = filepath.Base(filepath.Clean(target))
		}
	}
	if !validID(id) {
		return Active{}, fmt.Errorf("could not identify the active Omarchy theme")
	}
	// The directory is replaced as part of each theme set. Include that
	// identity even on versions which predate the explicit generation marker.
	generation := readMarker(filepath.Join(current, "theme.generation"))
	token := directoryIdentity(info)
	generation += fmt.Sprintf("/%s/%d", token, info.ModTime().UnixNano())
	return Active{Root: root, ID: id, Generation: generation, IntroToken: token}, nil
}

func validID(value string) bool {
	return value != "" && value != "." && value != ".." && len(value) <= 128 &&
		!strings.ContainsAny(value, "/\\\x00\r\n")
}

func readMarker(path string) string {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 256 {
		return ""
	}
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 257))
	if err != nil || len(data) > 256 {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func (a Active) Label() string {
	fields := strings.Fields(strings.ReplaceAll(a.ID, "-", " "))
	if len(fields) == 0 {
		return a.ID
	}
	for index, field := range fields {
		first := []rune(field)
		fields[index] = strings.ToUpper(string(first[0])) + string(first[1:])
	}
	return strings.Join(fields, " ")
}
