package seed

import (
	"encoding/json"
	"os"
	"strings"
)

// parseSTIX reads a full MITRE ATT&CK enterprise STIX 2.1 bundle
// (enterprise-attack.json) and extracts attack-pattern objects into the
// technique records the seeder inserts. This is used when MITRE_STIX_PATH
// points at the official dataset for full coverage.
func parseSTIX(path string) ([]techniqueRecord, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var bundle struct {
		Objects []stixObject `json:"objects"`
	}
	if err := json.Unmarshal(raw, &bundle); err != nil {
		return nil, err
	}
	out := []techniqueRecord{}
	for _, o := range bundle.Objects {
		if o.Type != "attack-pattern" || o.Revoked || o.Deprecated {
			continue
		}
		var extID, tacticName string
		for _, ref := range o.ExternalReferences {
			if ref.SourceName == "mitre-attack" && ref.ExternalID != "" {
				extID = ref.ExternalID
				break
			}
		}
		if extID == "" {
			continue
		}
		if len(o.KillChainPhases) > 0 {
			tacticName = o.KillChainPhases[0].PhaseName
		}
		out = append(out, techniqueRecord{
			TechniqueID:    extID,
			Name:           o.Name,
			Tactic:         tacticName,
			Description:    o.Description,
			IsSubtechnique: o.IsSubtechnique,
			ParentID:       parentOf(extID),
			Platforms:      o.Platforms,
		})
	}
	return out, nil
}

func parentOf(id string) string {
	if i := strings.Index(id, "."); i > 0 {
		return id[:i]
	}
	return ""
}

type stixObject struct {
	Type               string `json:"type"`
	Name               string `json:"name"`
	Description        string `json:"description"`
	Revoked            bool   `json:"revoked"`
	Deprecated         bool   `json:"x_mitre_deprecated"`
	IsSubtechnique     bool   `json:"x_mitre_is_subtechnique"`
	Platforms          []string `json:"x_mitre_platforms"`
	ExternalReferences []struct {
		SourceName string `json:"source_name"`
		ExternalID string `json:"external_id"`
	} `json:"external_references"`
	KillChainPhases []struct {
		PhaseName string `json:"phase_name"`
	} `json:"kill_chain_phases"`
}
