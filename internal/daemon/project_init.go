package daemon

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/Kocoro-lab/ShanClaw/internal/agents"
)

func (s *Server) handleProjectInit(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CWD          string `json:"cwd"`
		Instructions string `json:"instructions,omitempty"`
	}
	if !decodeBody(w, r, &req) {
		return
	}

	if !filepath.IsAbs(req.CWD) {
		writeError(w, http.StatusBadRequest, "cwd must be an absolute path")
		return
	}
	info, err := os.Stat(req.CWD)
	if err != nil {
		writeError(w, http.StatusBadRequest, "cwd does not exist: "+req.CWD)
		return
	}
	if !info.IsDir() {
		writeError(w, http.StatusBadRequest, "cwd is not a directory: "+req.CWD)
		return
	}

	shannonClean := filepath.Clean(s.deps.ShannonDir)
	cwdClean := filepath.Clean(req.CWD)
	if cwdClean == shannonClean || strings.HasPrefix(cwdClean, shannonClean+string(filepath.Separator)) {
		writeError(w, http.StatusBadRequest, "cannot initialize project inside the global shannon directory")
		return
	}

	dotShannon := filepath.Join(req.CWD, ".shannon")
	var created, existed []string

	if _, err := os.Stat(dotShannon); os.IsNotExist(err) {
		if err := os.MkdirAll(dotShannon, 0700); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		created = append(created, ".shannon/")
	} else {
		existed = append(existed, ".shannon/")
	}

	if req.Instructions != "" {
		instPath := filepath.Join(dotShannon, "instructions.md")
		if _, err := os.Stat(instPath); os.IsNotExist(err) {
			if err := agents.AtomicWrite(instPath, []byte(req.Instructions)); err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			created = append(created, ".shannon/instructions.md")
		} else {
			existed = append(existed, ".shannon/instructions.md")
		}
	}

	if created == nil {
		created = []string{}
	}
	if existed == nil {
		existed = []string{}
	}

	s.auditHTTPOp("POST", "/project/init", "initialized project at "+req.CWD)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"created": created,
		"existed": existed,
	})
}
