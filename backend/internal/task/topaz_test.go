package task

import "testing"

func TestTopazAdmissionIsIdempotent(t *testing.T) {
	svc, _, _, _ := newSpecializedMem(nil)
	input := map[string]any{"resourceId": "owned", "mediaType": "image", "model": "Standard V2", "scale": 2.0, "metadata": map[string]any{"clientOperationId": "topaz-once", "nodeId": "output"}}
	first, e := svc.CreateMediaUpscale("owner", "", "Standard V2", input)
	if e != nil {
		t.Fatal(e)
	}
	second, e := svc.CreateMediaUpscale("owner", "", "Standard V2", input)
	if e != nil || first.ID != second.ID {
		t.Fatalf("idempotency failed: %v", e)
	}
	if first.Type != "media_upscale" || first.Provider != "fal-topaz" {
		t.Fatalf("wrong route: %+v", first)
	}
	input["scale"] = 3.0
	if _, e = svc.CreateMediaUpscale("owner", "", "Standard V2", input); e == nil {
		t.Fatal("changed input reused operation ID")
	}
}
