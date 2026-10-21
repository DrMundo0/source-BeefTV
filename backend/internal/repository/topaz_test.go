package repository

import (
	"errors"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"infinite-canvas/backend/internal/model"
	"testing"
)

func TestTopazCancelledReceiptUsesDispatchSnapshotCAS(t *testing.T) {
	db, e := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if e != nil {
		t.Fatal(e)
	}
	if e = db.AutoMigrate(&model.Task{}); e != nil {
		t.Fatal(e)
	}
	original := model.Task{ID: "task", UserID: "owner", Type: "media_upscale", Status: model.TaskStatusCancelled, InputJSON: `{"submitted":true}`}
	if e = db.Create(&original).Error; e != nil {
		t.Fatal(e)
	}
	repo := New(db)
	receipt := original
	receipt.InputJSON = `{"submitted":true,"receipt":{"request_id":"original"}}`
	receipt.ProviderRequestID = "original"
	if e = repo.SaveCancelledMediaUpscaleReceipt(&receipt, "different"); !errors.Is(e, ErrTaskStateConflict) {
		t.Fatalf("accepted wrong snapshot: %v", e)
	}
	if e = repo.SaveCancelledMediaUpscaleReceipt(&receipt, original.InputJSON); e != nil {
		t.Fatal(e)
	}
	if e = repo.SaveCancelledMediaUpscaleReceipt(&receipt, original.InputJSON); !errors.Is(e, ErrTaskStateConflict) {
		t.Fatalf("duplicate writer accepted: %v", e)
	}
	stored, e := repo.Task(original.ID)
	if e != nil || stored.Status != model.TaskStatusCancelled || stored.ProviderRequestID != "original" {
		t.Fatalf("receipt lost %+v %v", stored, e)
	}
}

func TestTopazRecoveryPreservesAcceptedIdentity(t *testing.T) {
	db, e := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if e != nil {
		t.Fatal(e)
	}
	if e = db.AutoMigrate(&model.Task{}); e != nil {
		t.Fatal(e)
	}
	original := model.Task{ID: "paid", UserID: "owner", Type: "media_upscale", Status: model.TaskStatusFailed, ProviderRequestID: "accepted", InputJSON: `{"submitted":true,"receipt":{"request_id":"accepted"}}`}
	if e = db.Create(&original).Error; e != nil {
		t.Fatal(e)
	}
	repo := New(db)
	prepared := original
	prepared.InputJSON = `{"submitted":true,"receipt":{"request_id":"accepted"},"recoveryFailures":0}`
	if e = repo.RecoverMediaUpscaleTask(&prepared, "wrong", 4); !errors.Is(e, ErrTaskStateConflict) {
		t.Fatalf("wrong snapshot allowed %v", e)
	}
	if e = repo.RecoverMediaUpscaleTask(&prepared, original.InputJSON, 4); e != nil {
		t.Fatal(e)
	}
	stored, e := repo.Task(original.ID)
	if e != nil || stored.Status != model.TaskStatusRunning || stored.ProviderRequestID != original.ProviderRequestID || stored.ID != original.ID {
		t.Fatalf("lost original identity %+v %v", stored, e)
	}
	if e = repo.RecoverMediaUpscaleTask(&prepared, original.InputJSON, 4); !errors.Is(e, ErrTaskStateConflict) {
		t.Fatalf("concurrent resume accepted %v", e)
	}
}
