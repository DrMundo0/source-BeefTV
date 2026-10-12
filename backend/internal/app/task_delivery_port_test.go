package app

import (
	"context"
	"testing"

	"infinite-canvas/backend/internal/generation"
	"infinite-canvas/backend/internal/outbound"
)

// In-generation result downloads report under the executing BeefTV task, so one
// projection covers both phases.
func TestTaskDeliveryPortRecordsUnderTaskIdentityAndClears(t *testing.T) {
	svc := &Service{}
	port := appTaskDeliveryPort{service: svc, taskID: "task-9"}

	port.ReportDelivery(outbound.ResumableProgress{DownloadedBytes: 512, TotalBytes: 2048, BytesPerSecond: 100, Attempt: 1, State: "downloading"})
	got := svc.deliveryProgressRegistry().forTask("task-9")
	if got == nil || len(got.Outputs) != 1 {
		t.Fatalf("progress not projected: %#v", got)
	}
	output := got.Outputs[0]
	if output.OutputIndex != 0 || output.DownloadedBytes != 512 || output.TotalBytes != 2048 || output.State != "downloading" {
		t.Fatalf("unexpected projection: %+v", output)
	}

	port.ReportDelivery(outbound.ResumableProgress{TotalBytes: -1, Attempt: 2, State: "waiting"})
	if got := svc.deliveryProgressRegistry().forTask("task-9"); got == nil || got.Outputs[0].State != "waiting" {
		t.Fatalf("waiting report did not replace the entry: %+v", got)
	}
	if other := svc.deliveryProgressRegistry().forTask("task-10"); other != nil {
		t.Fatalf("progress leaked into another task: %+v", other)
	}

	port.ClearDelivery()
	if got := svc.deliveryProgressRegistry().forTask("task-9"); got != nil {
		t.Fatalf("progress not cleared: %+v", got)
	}
}

// Video downloads pass the provider's task id to the generation layer. Progress
// must still land on the BeefTV task the user sees, never the provider id.
func TestVideoDownloadProgressUsesBeefTVTaskIDNotProviderID(t *testing.T) {
	const beefTaskID = "beeftv-task-1"
	const providerTaskID = "task_WvIGYZ_provider"
	svc := &Service{}
	ctx := generation.WithRuntime(context.Background(), generation.Runtime{
		Delivery: appTaskDeliveryPort{service: svc, taskID: beefTaskID},
	})
	policy := generation.DefaultVideoPollPolicy()
	policy.MaxDownloadTries = 1

	data, mimeType, err := generation.RunVideoDownload(ctx, providerTaskID, policy, func(context.Context) ([]byte, string, error) {
		progress := svc.deliveryProgressRegistry().forTask(beefTaskID)
		if progress == nil || len(progress.Outputs) != 1 || progress.Outputs[0].State != "downloading" {
			t.Fatalf("progress not visible on the BeefTV task while downloading: %+v", progress)
		}
		if leaked := svc.deliveryProgressRegistry().forTask(providerTaskID); leaked != nil {
			t.Fatalf("progress keyed by provider id: %+v", leaked)
		}
		return []byte("video"), "video/mp4", nil
	})
	if err != nil || string(data) != "video" || mimeType != "video/mp4" {
		t.Fatalf("download = %q %q %v", data, mimeType, err)
	}
	if after := svc.deliveryProgressRegistry().forTask(beefTaskID); after != nil {
		t.Fatalf("progress not cleared after download: %+v", after)
	}
}
