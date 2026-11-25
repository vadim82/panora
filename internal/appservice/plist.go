package appservice

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"os"
)

type agent struct {
	Label     string
	Arguments []string
	Home      string
	RunAtLoad bool
}

func (a agent) marshal() []byte {
	var b bytes.Buffer
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?><plist version="1.0"><dict>`)
	text := func(s string) {
		b.WriteString("<string>")
		_ = xml.EscapeText(&b, []byte(s))
		b.WriteString("</string>")
	}
	key := func(s string) { b.WriteString("<key>" + s + "</key>") }
	key("Label")
	text(a.Label)
	key("ProgramArguments")
	b.WriteString("<array>")
	for _, s := range a.Arguments {
		text(s)
	}
	b.WriteString("</array>")
	key("EnvironmentVariables")
	b.WriteString("<dict>")
	key("HOME")
	text(a.Home)
	key("PATH")
	text("/usr/bin:/bin:/usr/sbin:/sbin")
	b.WriteString("</dict>")
	key("RunAtLoad")
	if a.RunAtLoad {
		b.WriteString("<true/>")
	} else {
		b.WriteString("<false/>")
	}
	key("KeepAlive")
	b.WriteString("<false/>")
	key("LimitLoadToSessionType")
	text("Aqua")
	key("ProcessType")
	text("Background")
	key("AssociatedBundleIdentifiers")
	b.WriteString("<array>")
	text("org.panora.app")
	b.WriteString("</array>")
	key("StandardOutPath")
	text("/dev/null")
	key("StandardErrorPath")
	text("/dev/null")
	b.WriteString("</dict></plist>\n")
	return b.Bytes()
}

type plistNode struct {
	XMLName  xml.Name
	Text     string      `xml:",chardata"`
	Children []plistNode `xml:",any"`
}

func readAgent(path string) (agent, error) {
	var a agent
	file, err := os.Open(path)
	if err != nil {
		return a, err
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, 65537))
	if err != nil {
		return a, err
	}
	if len(raw) > 65536 {
		return a, fmt.Errorf("startup registration exceeds 64 KiB")
	}
	var node plistNode
	if err := xml.Unmarshal(raw, &node); err != nil {
		return a, fmt.Errorf("read startup registration: %w", err)
	}
	if node.XMLName.Local != "plist" || len(node.Children) != 1 || node.Children[0].XMLName.Local != "dict" {
		return a, fmt.Errorf("invalid startup registration")
	}
	entries := node.Children[0].Children
	if len(entries)%2 != 0 {
		return a, fmt.Errorf("invalid startup dictionary")
	}
	seen := make(map[string]bool)
	for i := 0; i < len(entries); i += 2 {
		key, value := entries[i], entries[i+1]
		if key.XMLName.Local != "key" || seen[key.Text] {
			return a, fmt.Errorf("invalid or repeated startup key")
		}
		seen[key.Text] = true
		switch key.Text {
		case "Label":
			if value.XMLName.Local != "string" {
				return a, fmt.Errorf("invalid startup label")
			}
			a.Label = value.Text
		case "ProgramArguments":
			if value.XMLName.Local != "array" || len(value.Children) > 32 {
				return a, fmt.Errorf("invalid startup arguments")
			}
			for _, child := range value.Children {
				if child.XMLName.Local != "string" {
					return a, fmt.Errorf("invalid startup argument")
				}
				a.Arguments = append(a.Arguments, child.Text)
			}
		case "RunAtLoad":
			if value.XMLName.Local != "true" && value.XMLName.Local != "false" {
				return a, fmt.Errorf("invalid startup preference")
			}
			a.RunAtLoad = value.XMLName.Local == "true"
		case "EnvironmentVariables":
			children := value.Children
			if value.XMLName.Local != "dict" || len(children)%2 != 0 {
				return a, fmt.Errorf("invalid startup environment")
			}
			for j := 0; j < len(children); j += 2 {
				if children[j].XMLName.Local != "key" || children[j+1].XMLName.Local != "string" {
					return a, fmt.Errorf("invalid startup environment")
				}
				if children[j].Text == "HOME" {
					a.Home = children[j+1].Text
				}
			}
		}
	}
	if !bytes.Equal(raw, a.marshal()) {
		return a, fmt.Errorf("startup registration has unsupported edits; inspect it before replacing it")
	}
	return a, nil
}
