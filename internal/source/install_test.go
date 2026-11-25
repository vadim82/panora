package source

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestInstallRegistersCompatibleProgramsAndReportsBrokenEntries(t *testing.T) {
	root := filepath.Join(t.TempDir(), "registry")
	input := t.TempDir()
	name := "fixture"
	manifest := Manifest{ID: name, Name: "Fixture", Version: "1", Protocol: Protocol, Command: []string{"fixture.py"}}
	writeManifest := func() {
		t.Helper()
		data, err := json.Marshal(manifest)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(input, "plugin.json"), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	writeManifest()
	if _, err := InstallPlugin(t.Context(), root, input); err == nil {
		t.Fatal("missing executable was accepted")
	}
	if err := os.WriteFile(filepath.Join(input, "fixture.py"), []byte(`#!/usr/bin/env python3
import json
import sys
req = json.load(sys.stdin)
assert req["method"] == "describe"
print(json.dumps({"jsonrpc":"2.0", "id":req["id"], "result":{
    "protocol":2, "id":"fixture", "name":"Fixture", "media_kinds":["image"],
    "operations":["describe", "validate", "discover", "resolve"]}}))
`), 0700); err != nil {
		t.Fatal(err)
	}
	id, err := InstallPlugin(t.Context(), root, input)
	if err != nil || id != name {
		t.Fatalf("install=%q %v", id, err)
	}
	if _, err := InstallPlugin(t.Context(), root, input); err == nil {
		t.Fatal("duplicate install was accepted")
	}
	manifest.ID = "other"
	manifest.Protocol = Protocol - 1
	writeManifest()
	if _, err := InstallPlugin(t.Context(), root, input); err == nil {
		t.Fatal("incompatible manifest was accepted")
	}
	if err := os.Mkdir(filepath.Join(root, "broken"), 0700); err != nil {
		t.Fatal(err)
	}
	installed, err := InstalledPlugins(root)
	if err != nil || len(installed) != 2 || installed[0].ID != "broken" || installed[0].Err == nil ||
		installed[1].ID != name || installed[1].Client == nil || installed[1].Err != nil {
		t.Fatalf("installed=%+v %v", installed, err)
	}
	if _, err := installed[1].Client.Describe(t.Context()); err != nil {
		t.Fatal(err)
	}
}
