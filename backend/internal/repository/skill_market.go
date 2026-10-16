package repository

import (
	"errors"
	"gorm.io/gorm"
	"infinite-canvas/backend/internal/model"
)

func (r *Repository) MarketSkill(userID, remoteID string) (*model.Skill, error) {
	var s model.Skill
	err := r.db.Where("owner_id = ? AND source_type = ? AND source_ref = ? AND status = ?", userID, "market", remoteID, 1).First(&s).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &s, err
}
func (r *Repository) SkillVersions(skillID string) ([]model.SkillVersion, error) {
	var rows []model.SkillVersion
	err := r.db.Where("skill_id = ?", skillID).Order("created_at desc").Find(&rows).Error
	return rows, err
}
func (r *Repository) SwitchSkillVersion(userID, skillID, versionID, instruction string) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		var s model.Skill
		if err := tx.First(&s, "id = ? AND owner_id = ?", skillID, userID).Error; err != nil {
			return err
		}
		var v model.SkillVersion
		if err := tx.First(&v, "id = ? AND skill_id = ?", versionID, skillID).Error; err != nil {
			return err
		}
		if err := tx.Model(&s).Updates(map[string]any{"current_version_id": v.ID, "content_hash": v.ContentHash, "version_label": v.VersionLabel, "file_count": v.FileCount, "total_bytes": v.TotalBytes, "source_commit": v.SourceCommit, "instruction": instruction}).Error; err != nil {
			return err
		}
		return tx.Model(&model.UserSkillState{}).Where("user_id = ? AND skill_id = ?", userID, skillID).Updates(map[string]any{"installed_version_id": v.ID, "added": true}).Error
	})
}

func (r *Repository) AddMarketSkillVersion(skill *model.Skill, version *model.SkillVersion, files []model.SkillFile) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := New(tx).AddSkillVersion(skill, version, files); err != nil {
			return err
		}
		return tx.Model(&model.UserSkillState{}).Where("user_id = ? AND skill_id = ?", skill.OwnerID, skill.ID).Updates(map[string]any{"installed_version_id": version.ID, "added": true}).Error
	})
}
