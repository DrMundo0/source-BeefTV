package generation

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestReferenceUploadTLSDeadlineIsRecordedBeforeGeneration(t *testing.T) {
	t.Setenv("CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS", "127.0.0.1")
	address, closeServer := stalledTLS(t)
	defer closeServer()
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	receipts := &recordingReceipts{}
	ctx = WithRuntime(ctx, Runtime{Receipts: receipts})
	ctx = withReferenceUpload(ctx, ReferenceUploadMeta{Kind: "image", Index: 2})
	err := PutBeefAPISeedanceBytes(ctx, BeefAPISeedanceUploadSession{UploadURL: address + "/?X-Amz-Signature=PRIVATE"}, []byte("owned-fixture"))
	var upload *ReferenceUploadError
	if !errors.As(err, &upload) || !errors.Is(err, context.DeadlineExceeded) || upload.Meta.Index != 2 || strings.Contains(err.Error(), "PRIVATE") {
		t.Fatalf("upload cause lost or unsafe: %v", err)
	}
	seen := receipts.snapshot()
	if len(seen) != 1 || seen[0].Dispatched || seen[0].TransportPhase != "tls" || seen[0].TransportRoute != "direct" || len(seen[0].Body) != 0 {
		t.Fatalf("missing safe PUT receipt: %+v", seen)
	}
	if call, _ := CallMetaFromContext(seen[0].Request.Context()); call.RequestKind != "reference_put" {
		t.Fatalf("storage upload counted as model creation: %+v", call)
	}
	failure := ClassifyError(err)
	if failure.Uncertain || failure.BlocksManualRetry() || !failure.BlocksAutomaticRetry() || failure.ProviderCode != "reference_preupload_failed" {
		t.Fatalf("upload timeout confused with paid generation: %+v", failure)
	}
}
