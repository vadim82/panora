//go:build wails && darwin

package main

// Wails v2's directory dialog references UTType on current macOS SDKs.
// Link its owning framework explicitly for the candidate build.

// #cgo LDFLAGS: -framework UniformTypeIdentifiers
import "C"
