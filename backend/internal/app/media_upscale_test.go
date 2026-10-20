package app

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"infinite-canvas/backend/internal/generation"
	"infinite-canvas/backend/internal/model"
	localtask "infinite-canvas/backend/internal/task"
)

func TestUpscaleAdmissionPinsOfficialCredentialAndPayload(t *testing.T) {
	s, _, _, _ := creationTestService(t)
	if err := s.SaveLocalModelConfig([]byte(`{"channels":[{"id":"beefapi","baseUrl":"https://enterprise.beefapi.com","apiKey":"synthetic-disk-key","enabled":true}]}`)); err != nil {
		t.Fatal(err)
	}
	input := map[string]any{"mode": "image", "prompt": "AI 超分", "config": map[string]any{"credentialRef": "beefapi-enterprise", "apiKey": "forged-client-key", "baseUrl": "https://untrusted.invalid", "model": "topaz-standard-v2", "interfaceType": "newapi"}, "upscale": map[string]any{"factor": 2}, "referenceImages": []any{map[string]any{"storageKey": "resource:owned-source"}}}
	resolved, err := s.resolveManagedBeefAPISecrets(input)
	if err != nil {
		t.Fatal(err)
	}
	config := resolved["config"].(map[string]any)
	if config["apiKey"] != "synthetic-disk-key" || config["baseUrl"] != "https://enterprise.beefapi.com" {
		t.Fatal("client overrode managed credential origin")
	}
	if err := (taskCatalogAdapter{s}).ValidateCapability(resolved); err != nil {
		t.Fatal(err)
	}
	for _, request := range []localtask.SelectRequest{
		{Input: resolved, Model: "topaz-proteus", Type: "canvas_image"},
		{Input: resolved, Model: "topaz-standard-v2", Type: "canvas_video"},
		{Input: input, Model: "topaz-standard-v2", Type: "canvas_image"},
	} {
		if _, err := (taskCatalogAdapter{s}).Select("owner", request); err == nil {
			t.Fatal("mismatched task identity or unmanaged credentials accepted")
		}
	}
	if err := s.protectTaskSecrets(resolved); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(resolved)
	if strings.Contains(string(raw), "synthetic-disk-key") || strings.Contains(string(raw), "forged-client-key") {
		t.Fatal("plaintext key persisted")
	}
	decoded, err := s.decryptTaskInputJSON(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	var prepared generation.Input
	if err := json.Unmarshal([]byte(decoded), &prepared); err != nil {
		t.Fatal(err)
	}
	if prepared.Upscale == nil || prepared.Upscale.Factor != 2 || prepared.Config.APIKey != "synthetic-disk-key" {
		t.Fatal("sealed task lost upscale request")
	}
	started := time.Now()
	if !s.shouldDeferVideoProviderTask(model.Task{Type: "canvas_image", Model: "topaz-standard-v2", ProviderRequestID: "original", StartedAt: &started}, decoded, generation.StatePendingError{TaskID: "original"}) {
		t.Fatal("accepted image upscale cannot resume in existing queue")
	}
}

func TestUpscaleConfigRequiresConnectedAccount(t *testing.T) {
	s, _, _, _ := creationTestService(t)
	if _, err := s.upscaleConfig("topaz-standard-v2"); err == nil {
		t.Fatal("missing managed credential accepted")
	}
}
