package operations

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestSkillPublicationRequirementsDescriptorIsFinite(t *testing.T) {
	r := NewRegistry(nil, nil)
	RegisterDefaultOps(r)
	var schema struct {
		Properties struct {
			Metadata struct {
				Properties map[string]struct {
					Type        string
					Description string
					Items       struct{ Enum []string }
				}
			}
		}
	}
	if err := json.Unmarshal(r.ops["skill.publish.prepare"].Params, &schema); err != nil {
		t.Fatal(err)
	}
	requirements := schema.Properties.Metadata.Properties["requirements"]
	if !reflect.DeepEqual(requirements.Items.Enum, []string{"beeftv.canvas", "model.image", "model.video", "model.audio"}) || !strings.Contains(requirements.Description, "[]") || !strings.Contains(requirements.Description, "text") {
		t.Fatalf("model-facing capability contract is ambiguous: %+v", requirements)
	}
	if schema.Properties.Metadata.Properties["requirementNotes"].Type != "string" {
		t.Fatal("natural language requirements have no separate field")
	}
}
