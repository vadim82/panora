package service

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestServiceLogBoundsExistingAndNewOutput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs", "service.log")
	log, err := OpenLog(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []byte{'a', 'b', 'c'} {
		if _, err := log.Write(bytes.Repeat([]byte{value}, LogLimit)); err != nil {
			t.Fatal(err)
		}
	}
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
	for _, file := range []struct {
		path  string
		value byte
	}{{path, 'c'}, {path + ".1", 'b'}} {
		actual, err := os.ReadFile(file.path)
		if err != nil || !bytes.Equal(actual, bytes.Repeat([]byte{file.value}, LogLimit)) {
			t.Fatalf("rotation %s: %d bytes %v", file.path, len(actual), err)
		}
	}
	if err := os.WriteFile(path, bytes.Repeat([]byte{'x'}, LogLimit*3), 0600); err != nil {
		t.Fatal(err)
	}
	log, err = OpenLog(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(path); err != nil || info.Size() > LogLimit {
		t.Fatalf("existing log not bounded: %v %v", info, err)
	}
}

func TestServiceLogRejectsSymlink(t *testing.T) {
	path := filepath.Join(t.TempDir(), "service.log")
	original := path + ".original"
	if err := os.WriteFile(original, []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(original, path); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenLog(path); err == nil {
		t.Fatal("followed log symlink")
	}
	data, _ := os.ReadFile(original)
	if string(data) != "preserve" {
		t.Fatal("original changed")
	}
}
