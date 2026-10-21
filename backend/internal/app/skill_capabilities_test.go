package app

import (
	"encoding/json"
	"errors"
	"testing"

	"infinite-canvas/backend/internal/kernel"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/skillhub"
	"infinite-canvas/backend/internal/skills"
)

func TestSkillRequirementsRejectUnconfiguredMediaBeforeExecution(t *testing.T) {
	s, _, _ := newAssistantTurnService(t)
	item, err := s.CreateSkill("local", SkillMutationRequest{SkillName: "文字技能", Description: "按输入组织结果", Instruction: "列出三点。", Tag: "others", IsPrivate: true})
	if err != nil {
		t.Fatal(err)
	}
	pin := skills.Pin{SkillID: item.SkillID, VersionID: item.VersionID, ContentHash: item.ContentHash}
	if _, err := s.ValidateAgentSkills("local", []skills.Pin{pin}); err != nil {
		t.Fatal("text skill required media configuration", err)
	}
	for _, requirement := range []string{"model.video", "model.audio", "model.image", "unknown.capability"} {
		raw, _ := json.Marshal(skillhub.Release{Metadata: skillhub.Metadata{SkillID: "reviewed", Requirements: []string{requirement}}, ReleaseID: "r_1"})
		if err := s.Database().Model(&model.SkillVersion{}).Where("id = ?", pin.VersionID).Update("market_release_json", string(raw)).Error; err != nil {
			t.Fatal(err)
		}
		_, err := s.ValidateAgentSkills("local", []skills.Pin{pin})
		var unavailable *kernel.AppError
		if !errors.As(err, &unavailable) || unavailable.Reason != "skill_capability_unavailable" {
			t.Fatalf("%s executed without known capability: %v", requirement, err)
		}
	}
}
