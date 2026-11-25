package main

import "testing"

func TestWindowIdentityIsolatesPrivateSocketsAndManagedWindows(t *testing.T) {
	if windowIdentity("/tmp/a/control.sock", false) != windowIdentity("/tmp/a/./control.sock", false) ||
		windowIdentity("/tmp/a/control.sock", false) == windowIdentity("/tmp/b/control.sock", false) ||
		windowIdentity("/tmp/a/control.sock", false) == windowIdentity("/tmp/a/control.sock", true) {
		t.Fatal("window identity does not isolate private sockets or managed Linux windows")
	}
}
