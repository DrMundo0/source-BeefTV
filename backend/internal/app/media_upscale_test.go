package app

import (
	"context"
	"errors"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/outbound"
	"infinite-canvas/backend/internal/topaz"
	"io"
	"testing"
	"time"
)

func TestTopazRecoveryErrorClassification(t *testing.T) {
	for _, tc := range []struct {
		err   error
		retry bool
	}{
		{&topaz.HTTPError{StatusCode: 422}, false}, {&topaz.HTTPError{StatusCode: 401}, false}, {&topaz.HTTPError{StatusCode: 402}, false}, {&topaz.HTTPError{StatusCode: 429}, true}, {&topaz.HTTPError{StatusCode: 503}, true}, {&topaz.NetworkError{}, true},
		{&outbound.HTTPStatusError{StatusCode: 404}, false}, {&outbound.HTTPStatusError{StatusCode: 502}, true}, {errors.New("输出媒体探测失败"), false}, {BadAuthRequest("存储配额不足"), false}, {io.ErrUnexpectedEOF, true}, {context.DeadlineExceeded, true}, {context.Canceled, false},
	} {
		if got := topazRecoveryRetryable(tc.err); got != tc.retry {
			t.Fatalf("%v retry=%v want %v", tc.err, got, tc.retry)
		}
	}
}

func TestTopazRunningDownloadAdmissionAndCancellation(t *testing.T) {
	svc, db := newTimelineTaskTestService(t)
	receipt := `{"submitted":true,"receipt":{"request_id":"accepted","status_url":"https://queue.fal.run/topaz/requests/accepted/status","response_url":"https://queue.fal.run/topaz/requests/accepted"}}`
	task := model.Task{ID: "topaz-download", UserID: "owner", Type: "media_upscale", Status: model.TaskStatusRunning, ProviderRequestID: "accepted", InputJSON: receipt}
	if e := db.Create(&task).Error; e != nil {
		t.Fatal(e)
	}
	ctx, finish, e := svc.beginGenerationDownload("owner", task.ID+":0", time.Minute)
	if e != nil {
		t.Fatalf("accepted running download refused: %v", e)
	}
	defer finish(false)
	svc.cancelActiveTask(task.ID)
	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatal("local cancel did not stop download")
	}
	if _, _, e = svc.beginGenerationDownload("foreign", task.ID+":0", time.Minute); e == nil {
		t.Fatal("foreign download allowed")
	}
	if e = db.Model(&model.Task{}).Where("id = ?", task.ID).Update("input_json", `{"submitted":true}`).Error; e != nil {
		t.Fatal(e)
	}
	if _, _, e = svc.beginGenerationDownload("owner", task.ID+":1", time.Minute); e == nil {
		t.Fatal("unconfirmed running download allowed")
	}
	if e = db.Model(&model.Task{}).Where("id = ?", task.ID).Updates(map[string]any{"input_json": receipt, "status": model.TaskStatusCancelled}).Error; e != nil {
		t.Fatal(e)
	}
	if _, _, e = svc.beginGenerationDownload("owner", task.ID+":2", time.Minute); e == nil {
		t.Fatal("cancelled download allowed")
	}
}
