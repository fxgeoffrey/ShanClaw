package daemon

import (
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Kocoro-lab/ShanClaw/internal/agents"
	"github.com/Kocoro-lab/ShanClaw/internal/skills"
)

type ruleEntry struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

func (s *Server) handleListRules(w http.ResponseWriter, r *http.Request) {
	rulesDir := filepath.Join(s.deps.ShannonDir, "rules")
	entries, err := os.ReadDir(rulesDir)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, http.StatusOK, map[string]interface{}{"rules": []ruleEntry{}})
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var names []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)

	rules := make([]ruleEntry, 0, len(names))
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(rulesDir, name))
		if err != nil {
			continue
		}
		ruleName := strings.TrimSuffix(name, ".md")
		rules = append(rules, ruleEntry{Name: ruleName, Content: string(data)})
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"rules": rules})
}

func (s *Server) handleGetRule(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := skills.ValidateSkillName(name); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	path := filepath.Join(s.deps.ShannonDir, "rules", name+".md")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			writeError(w, http.StatusNotFound, "rule not found: "+name)
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, ruleEntry{Name: name, Content: string(data)})
}

func (s *Server) handlePutRule(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := skills.ValidateSkillName(name); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var body struct {
		Content string `json:"content"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	if strings.TrimSpace(body.Content) == "" {
		writeError(w, http.StatusBadRequest, "content must not be empty")
		return
	}

	rulesDir := filepath.Join(s.deps.ShannonDir, "rules")
	if err := os.MkdirAll(rulesDir, 0700); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	path := filepath.Join(rulesDir, name+".md")
	_, statErr := os.Stat(path)
	if err := agents.AtomicWrite(path, []byte(body.Content)); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	status := http.StatusOK
	if os.IsNotExist(statErr) {
		status = http.StatusCreated
	}
	s.auditHTTPOp("PUT", "/rules/"+name, "wrote rule")
	writeJSON(w, status, map[string]string{"status": "updated", "name": name})
}

func (s *Server) handleDeleteRule(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("confirm") != "true" {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error":   "confirmation_required",
			"message": "This will permanently delete the rule. Add ?confirm=true to proceed.",
		})
		return
	}

	name := r.PathValue("name")
	if err := skills.ValidateSkillName(name); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	path := filepath.Join(s.deps.ShannonDir, "rules", name+".md")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		writeError(w, http.StatusNotFound, "rule not found: "+name)
		return
	}
	if err := os.Remove(path); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.auditHTTPOp("DELETE", "/rules/"+name, "deleted rule")
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
