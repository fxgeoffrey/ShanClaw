//go:build darwin

package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateDaemonPlist(t *testing.T) {
	plist := GenerateDaemonPlist("/usr/local/bin/shan", "/tmp/daemon.log")

	checks := []struct {
		name    string
		substr  string
		present bool
	}{
		{"label", daemonLabel, true},
		{"binary path", "/usr/local/bin/shan", true},
		{"KeepAlive", "<key>KeepAlive</key>", true},
		{"KeepAlive true", "<true/>", true},
		{"RunAtLoad", "<key>RunAtLoad</key>", true},
		{"log path stdout", "<key>StandardOutPath</key>", true},
		{"log path stderr", "<key>StandardErrorPath</key>", true},
		{"log path value", "/tmp/daemon.log", true},
		{"daemon arg", "<string>daemon</string>", true},
		{"start arg", "<string>start</string>", true},
		{"-d flag absent", "-d</string>", false},
	}

	for _, c := range checks {
		if c.present && !strings.Contains(plist, c.substr) {
			t.Errorf("%s: expected %q in plist", c.name, c.substr)
		}
		if !c.present && strings.Contains(plist, c.substr) {
			t.Errorf("%s: unexpected %q in plist", c.name, c.substr)
		}
	}
}

func TestGenerateDaemonPlistEscapesXML(t *testing.T) {
	plist := GenerateDaemonPlist("/path/to/<shan>&\"test\"", "/log/<file>")
	if strings.Contains(plist, "<shan>") {
		t.Error("unescaped < in binary path")
	}
	if !strings.Contains(plist, "&lt;shan&gt;") {
		t.Error("missing escaped < in binary path")
	}
	if !strings.Contains(plist, "&amp;&quot;test&quot;") {
		t.Error("missing escaped & and \" in binary path")
	}
}

func TestDaemonPlistPath(t *testing.T) {
	p := DaemonPlistPath()
	if !strings.Contains(p, "com.shannon.daemon.plist") {
		t.Errorf("expected com.shannon.daemon.plist in path, got: %s", p)
	}
	if !strings.Contains(p, "LaunchAgents") {
		t.Errorf("expected LaunchAgents in path, got: %s", p)
	}
}

func TestWriteDaemonPlist(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.plist")
	content := GenerateDaemonPlist("/usr/local/bin/shan", "/tmp/daemon.log")

	if err := WriteDaemonPlist(path, content); err != nil {
		t.Fatalf("WriteDaemonPlist: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != content {
		t.Error("written content does not match expected content")
	}
}
