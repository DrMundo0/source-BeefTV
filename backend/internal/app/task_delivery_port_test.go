package app

import (
	"testing"

	"infinite-canvas/backend/internal/outbound"
)

// In-generation result downloads report under the same "taskID:0" identity the
// post-success delivery uses, so one projection covers both phases.
func TestTaskDeliveryPortRecordsUnderTaskIdentityAndClears(t *testing.T) {
	svc := &Service{}
	port := appTaskDeliveryPort{service: svc}

	port.ReportDelivery("task-9", outbound.ResumableProgress{DownloadedBytes: 512, TotalBytes: 2048, BytesPerSecond: 100, Attempt: 1, State: "downloading"})
	got := svc.deliveryProgressRegistry().forTask("task-9")
	if got == nil || len(got.Outputs) != 1 {
		t.Fatalf("progress not projected: %#v", got)
	}
	output := got.Outputs[0]
	if output.OutputIndex != 0 || output.DownloadedBytes != 512 || output.TotalBytes != 2048 || output.State != "downloading" {
		t.Fatalf("unexpected projection: %+v", output)
	}

	port.ReportDelivery("task-9", outbound.ResumableProgress{TotalBytes: -1, Attempt: 2, State: "waiting"})
	if got := svc.deliveryProgressRegistry().forTask("task-9"); got == nil || got.Outputs[0].State != "waiting" {
		t.Fatalf("waiting report did not replace the entry: %+v", got)
	}
	if other := svc.deliveryProgressRegistry().forTask("task-10"); other != nil {
		t.Fatalf("progress leaked into another task: %+v", other)
	}

	port.ClearDelivery("task-9")
	if got := svc.deliveryProgressRegistry().forTask("task-9"); got != nil {
		t.Fatalf("progress not cleared: %+v", got)
	}
}
