package agents

import (
	"os"
	"sort"
)

// AgentsAttachingSkill returns the names of agents whose _attached.yaml
// manifest references the given skill. The result is sorted alphabetically
// and is always a non-nil slice (empty slice when no agents attach the skill),
// so JSON responses render as "[]" rather than "null".
//
// Errors from reading a single agent's manifest are skipped — a corrupt
// manifest for agent A should not hide attachments in agent B. Only a
// filesystem error opening agentsDir itself is returned.
func AgentsAttachingSkill(agentsDir, skillName string) ([]string, error) {
	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
	}

	result := make([]string, 0)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		agentName := entry.Name()
		if len(agentName) == 0 || agentName[0] == '.' {
			continue
		}
		if err := ValidateAgentName(agentName); err != nil {
			continue
		}
		names, err := ReadAttachedSkills(agentsDir, agentName)
		if err != nil {
			// Corrupt or unreadable manifest — skip this agent, keep going.
			continue
		}
		for _, n := range names {
			if n == skillName {
				result = append(result, agentName)
				break
			}
		}
	}
	sort.Strings(result)
	return result, nil
}
