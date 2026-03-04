package agent

import "testing"

func TestApprovalCache_EmptyCache(t *testing.T) {
	c := NewApprovalCache()
	if c.WasApproved("bash", `{"command":"rm foo.txt"}`) {
		t.Fatal("empty cache should not report any approvals")
	}
}

func TestApprovalCache_SameToolSameArgs(t *testing.T) {
	c := NewApprovalCache()
	args := `{"command":"rm foo.txt"}`
	c.RecordApproval("bash", args)
	if !c.WasApproved("bash", args) {
		t.Fatal("same tool+args should be approved after recording")
	}
}

func TestApprovalCache_SameToolDifferentArgs(t *testing.T) {
	c := NewApprovalCache()
	c.RecordApproval("bash", `{"command":"rm foo.txt"}`)
	if c.WasApproved("bash", `{"command":"rm bar.txt"}`) {
		t.Fatal("same tool with different args should NOT be auto-approved")
	}
}

func TestApprovalCache_DifferentToolSameArgs(t *testing.T) {
	c := NewApprovalCache()
	args := `{"command":"rm foo.txt"}`
	c.RecordApproval("bash", args)
	if c.WasApproved("file_write", args) {
		t.Fatal("different tool with same args should NOT be auto-approved")
	}
}

func TestApprovalCache_MultipleApprovals(t *testing.T) {
	c := NewApprovalCache()
	c.RecordApproval("bash", `{"command":"rm foo.txt"}`)
	c.RecordApproval("bash", `{"command":"rm bar.txt"}`)
	c.RecordApproval("file_write", `{"path":"/tmp/x"}`)

	if !c.WasApproved("bash", `{"command":"rm foo.txt"}`) {
		t.Fatal("first approval should still be cached")
	}
	if !c.WasApproved("bash", `{"command":"rm bar.txt"}`) {
		t.Fatal("second approval should be cached")
	}
	if !c.WasApproved("file_write", `{"path":"/tmp/x"}`) {
		t.Fatal("third approval should be cached")
	}
	if c.WasApproved("bash", `{"command":"rm baz.txt"}`) {
		t.Fatal("unapproved combination should not be cached")
	}
}
