package repository

import (
	"gorm.io/gorm"
	"infinite-canvas/backend/internal/model"
	"time"
)

// SaveMediaUpscaleState fences every dispatch marker and receipt by the worker lease.
func (r *Repository) SaveMediaUpscaleState(task *model.Task) error {
	result := taskLeaseWriter(r.db.Model(&model.Task{}), task.LeaseOwner).Where("id = ? AND status = ?", task.ID, model.TaskStatusRunning).Updates(map[string]any{"input_json": task.InputJSON, "provider_request_id": task.ProviderRequestID, "updated_at": time.Now()})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrTaskStateConflict
	}
	return nil
}

// SaveCancelledMediaUpscaleReceipt accepts only the exact pre-dispatch input
// snapshot. Cancellation clears worker leases, so this immutable snapshot CAS
// fences the late receipt against another generation or newer writer.
func (r *Repository) SaveCancelledMediaUpscaleReceipt(task *model.Task, submittedInput string) error {
	result := r.db.Model(&model.Task{}).Where("id = ? AND user_id = ? AND type = ? AND status = ? AND input_json = ? AND provider_request_id = ''", task.ID, task.UserID, "media_upscale", model.TaskStatusCancelled, submittedInput).Updates(map[string]any{"input_json": task.InputJSON, "provider_request_id": task.ProviderRequestID, "updated_at": time.Now()})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrTaskStateConflict
	}
	return nil
}

// RecoverMediaUpscaleTask resumes only the accepted original request. Receipt,
// provider ID and dispatch marker are immutable; no new task identity is minted.
func (r *Repository) RecoverMediaUpscaleTask(task *model.Task, expectedInput string, limit int) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		var active int64
		if e := tx.Model(&model.Task{}).Where("user_id = ? AND status IN ?", task.UserID, []model.TaskStatus{model.TaskStatusQueued, model.TaskStatusRunning}).Count(&active).Error; e != nil {
			return e
		}
		if limit > 0 && active >= int64(limit) {
			return ErrActiveTaskLimit
		}
		now := time.Now()
		result := tx.Model(&model.Task{}).Where("id = ? AND user_id = ? AND type = ? AND status = ? AND input_json = ? AND provider_request_id = ?", task.ID, task.UserID, "media_upscale", model.TaskStatusFailed, expectedInput, task.ProviderRequestID).Updates(map[string]any{"input_json": task.InputJSON, "status": model.TaskStatusRunning, "stage": "恢复 Topaz 原任务结果", "error": "", "failure_diagnostics": nil, "completed_at": nil, "lease_owner": "", "lease_expires_at": nil, "next_poll_at": now, "updated_at": now})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrTaskStateConflict
		}
		return nil
	})
}
