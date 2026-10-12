package repository

import "infinite-canvas/backend/internal/model"

func (r *Repository) SetGenerationDeliveryStage(userID, taskID, stage string) error {
	result := r.db.Model(&model.Task{}).Where("id = ? AND user_id = ? AND status = ?", taskID, userID, model.TaskStatusSucceeded).Update("stage", stage)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrTaskStateConflict
	}
	return nil
}
