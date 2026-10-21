package task

import "infinite-canvas/backend/internal/model"

// CreateMediaUpscale admits validated cloud media work through the standard
// durable task identity, quota and project policy boundary. No key is persisted.
func (s *Service) CreateMediaUpscale(user, project, modelName string, input map[string]any) (*model.Task, error) {
	return s.admitSpecialized(user, specializedAdmission{kind: localExecutorKind{Type: "media_upscale", Prompt: "Topaz AI 超分", Provider: "fal-topaz", Model: modelName, Stage: "等待云端超分", Log: "Topaz 超分已进入队列"}, projectID: project, input: input})
}
