package topaz

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestTopazPayloadContracts(t *testing.T) {
	for _, tc := range []struct {
		kind, mode, model string
		scale             float64
	}{{"image", "precision", "Standard V2", 2}, {"image", "generative", "Wonder 3.5", 3}, {"image", "transparent", "Transparent", 4}, {"video", "precision", "Proteus", 2}, {"video", "generative", "Starlight Fast 2", 3}, {"video", "generative", "Starlight Precise 2.6", 4}} {
		r := Request{ResourceID: "owned", ChannelID: "fal", Mode: tc.mode, Model: tc.model, Scale: tc.scale}
		if e := Validate(r, tc.kind); e != nil {
			t.Fatal(e)
		}
		p := Payload(r, tc.kind, "https://fal.media/input")
		if _, ok := p["input"]; ok {
			t.Fatal("must use bare JSON")
		}
		if _, ok := p["target_fps"]; ok {
			t.Fatal("must preserve fps")
		}
		if tc.kind == "image" && tc.mode == "precision" && p["face_enhancement"] != false {
			t.Fatal("precision must explicitly disable face enhancement")
		}
		if tc.kind == "video" && p["H264_output"] != true {
			t.Fatal("must request H264")
		}
		if tc.mode == "transparent" && len(p) != 1 {
			t.Fatal("transparent only accepts image URL")
		}
	}
}
func TestTopazRejectsUnsupportedInputs(t *testing.T) {
	for _, r := range []Request{{ResourceID: "owned", ChannelID: "fal", Mode: "precision", Model: "Standard V2", Scale: 4.5}, {ResourceID: "owned", ChannelID: "fal", Mode: "transparent", Model: "Transparent", Scale: 2}, {ResourceID: "owned", ChannelID: "fal", Mode: "generative", Model: "Astra 2", Scale: 2}} {
		if Validate(r, "image") == nil {
			t.Fatalf("accepted %+v", r)
		}
	}
}
func TestTopazQueueAuthAndSafeErrors(t *testing.T) {
	c := Client{Key: "test-secret", HTTP: &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "Key test-secret" {
			t.Fatal("incorrect auth")
		}
		return &http.Response{StatusCode: 422, Body: io.NopCloser(strings.NewReader("secret upstream body"))}, nil
	})}}
	e := c.Do(context.Background(), "POST", "https://queue.fal.run/topaz/upscale/image/precision", map[string]any{"image_url": "x"}, nil, true)
	if e == nil || strings.Contains(e.Error(), "secret") {
		t.Fatal("unsafe error")
	}
	for _, u := range []string{"https://evil.test/topaz/requests/1", "http://queue.fal.run/topaz/requests/1", "https://queue.fal.run@evil.test/topaz/requests/1"} {
		if QueueURL(u) {
			t.Fatalf("accepted %s", u)
		}
	}
	if !QueueURL("https://queue.fal.run/topaz/requests/1/status") {
		t.Fatal("must accept returned queue URL")
	}
}
