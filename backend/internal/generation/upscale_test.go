package generation

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"infinite-canvas/backend/internal/mediatools"
)

type upscaleResources struct {
	ResourcePort
	data []byte
	kind string
}

func (p upscaleResources) Lookup(user, id string) (ResourceInfo, error) {
	if user != "user-1" || id != "source" {
		return ResourceInfo{}, errors.New("resource not owned")
	}
	mime := "image/png"
	if p.kind == "video" {
		mime = "video/mp4"
	}
	return ResourceInfo{ID: id, UserID: user, Status: "ready", Kind: p.kind, MimeType: mime, Size: int64(len(p.data)), Width: 64, Height: 48, DurationMs: 1000}, nil
}
func (p upscaleResources) Open(user, id string) (ResourceInfo, io.ReadCloser, error) {
	info, err := p.Lookup(user, id)
	if err != nil {
		return info, nil, err
	}
	return info, io.NopCloser(bytes.NewReader(p.data)), nil
}
func (p upscaleResources) LocalMode() bool { return true }

func upscalePNG(t *testing.T, w, h int) []byte {
	t.Helper()
	var out bytes.Buffer
	if err := png.Encode(&out, image.NewRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func upscaleClip(t *testing.T, width, height int, audio bool) []byte {
	t.Helper()
	bin, err := mediatools.ResolveFFmpeg()
	if err != nil {
		t.Skip("native ffmpeg unavailable")
	}
	if _, err := mediatools.ResolveFFprobe(); err != nil {
		t.Skip("native ffprobe unavailable")
	}
	path := filepath.Join(t.TempDir(), "clip.mp4")
	args := []string{"-nostdin", "-v", "error", "-f", "lavfi", "-i", fmt.Sprintf("testsrc2=size=%dx%d:rate=24:duration=1", width, height)}
	if audio {
		args = append(args, "-f", "lavfi", "-i", "sine=frequency=440:duration=1")
	}
	args = append(args, "-c:v", "libx264", "-pix_fmt", "yuv420p")
	if audio {
		args = append(args, "-c:a", "aac", "-shortest")
	}
	args = append(args, path)
	if out, err := exec.Command(bin, args...).CombinedOutput(); err != nil {
		t.Fatalf("fixture ffmpeg: %s %v", out, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestUpscaleExecuteFourModelsUsesOwnedUploadAndOriginalResult(t *testing.T) {
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
	for _, model := range []string{"topaz-standard-v2", "topaz-wonder-3.5", "topaz-proteus", "topaz-starlight-fast-2"} {
		t.Run(model, func(t *testing.T) {
			kind := UpscaleMediaType(model)
			source, output := upscalePNG(t, 64, 48), upscalePNG(t, 128, 96)
			mime := "image/png"
			if kind == "video" {
				source, output, mime = upscaleClip(t, 64, 48, true), upscaleClip(t, 128, 96, false), "video/mp4"
			}
			var creates, uploads, downloads, estimates atomic.Int32
			var posted map[string]any
			var server *httptest.Server
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path != "/put" && r.Header.Get("Authorization") != "Bearer synthetic-upscale-key" {
					t.Error("missing gateway auth")
				}
				switch r.URL.Path {
				case "/v1/video-references/uploads":
					uploads.Add(1)
					_ = json.NewEncoder(w).Encode(BeefAPISeedanceUploadSession{Ticket: "ticket", UploadURL: server.URL + "/put", UploadMethod: "PUT", RequiredHeaders: map[string]string{"Content-Type": mime}})
				case "/put":
					data, _ := io.ReadAll(r.Body)
					if !bytes.Equal(data, source) || r.Header.Get("Authorization") != "" {
						t.Error("upload altered original bytes or leaked key")
					}
				case "/v1/video-references/uploads/complete":
					sum := sha256.Sum256(source)
					_ = json.NewEncoder(w).Encode(BeefAPISeedanceUploadComplete{URL: server.URL + "/source", Kind: kind, Mime: mime, Bytes: int64(len(source)), SHA256: hex.EncodeToString(sum[:])})
				case "/v1/media-upscale/estimate":
					estimates.Add(1)
					_ = json.NewEncoder(w).Encode(UpscaleEstimate{Model: model, Currency: "CNY", Reservation: 1.25})
				case "/v1/media-upscale/tasks":
					creates.Add(1)
					if r.Method != "POST" || r.Header.Get("Idempotency-Key") != "stable-operation" {
						t.Error("lost submission key")
					}
					_ = json.NewDecoder(r.Body).Decode(&posted)
					w.WriteHeader(202)
					_, _ = io.WriteString(w, `{"task_id":"original-task","status":"queued"}`)
				case "/v1/media-upscale/tasks/original-task":
					_ = json.NewEncoder(w).Encode(map[string]any{"task_id": "original-task", "status": "completed", "model": model, "media_type": kind, "result_url": "https://untrusted.invalid/steal"})
				case "/v1/media-upscale/tasks/original-task/content":
					downloads.Add(1)
					w.Header().Set("Content-Type", mime)
					_, _ = w.Write(output)
				default:
					t.Errorf("unexpected endpoint %s", r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			runtime := taskRuntime(func(r *Runtime) {
				r.Endpoints.BeefAPIVideoBaseURL = server.URL
				r.Resources = upscaleResources{data: source, kind: kind}
			})
			input := Input{Mode: kind, Prompt: "AI 超分", Config: Config{BaseURL: server.URL, APIKey: "synthetic-upscale-key", Model: model, InterfaceType: "newapi"}, Upscale: &UpscaleOptions{Factor: 2}}
			if kind == "image" {
				input.ReferenceImages = []Media{{StorageKey: "resource:source"}}
			} else {
				input.ReferenceVideos = []Media{{StorageKey: "resource:source"}}
			}
			ctx := WithSubmissionKey(WithRuntime(context.Background(), runtime), "stable-operation")
			quote, err := EstimateUpscale(ctx, input)
			if err != nil || quote.GenerationSubmitted || creates.Load() != 0 || quote.Reservation != 1.25 {
				t.Fatalf("estimate submitted generation: %#v %v", quote, err)
			}
			result, err := Execute(ctx, input)
			if err != nil {
				t.Fatal(err)
			}
			want := map[string]any{"model": model, "upscale_factor": float64(2)}
			if kind == "image" {
				want["image_url"] = server.URL + "/source"
				want["face_enhancement"] = false
				want["output_format"] = "png"
				if model == "topaz-wonder-3.5" {
					want["enhancement_strength"] = "low"
				}
			} else {
				want["video_url"] = server.URL + "/source"
				want["H264_output"] = true
			}
			if !reflect.DeepEqual(posted, want) {
				t.Fatalf("native payload = %#v want %#v", posted, want)
			}
			if creates.Load() != 1 || downloads.Load() != 1 || estimates.Load() != 1 {
				t.Fatalf("creates=%d downloads=%d estimates=%d", creates.Load(), downloads.Load(), estimates.Load())
			}
			if kind == "video" {
				video := result["video"].(map[string]any)
				_, data, err := DecodeProviderDataURL(video["dataUrl"].(string))
				if err != nil {
					t.Fatal(err)
				}
				dir := t.TempDir()
				path, _ := upscaleTempFile(dir, "result.mp4", data)
				facts, err := mediatools.ProbeLocal(ctx, path)
				if err != nil || !facts.HasAudio || facts.Width != 128 || facts.Height != 96 || facts.DurationMs < 950 || facts.DurationMs > 1050 {
					t.Fatalf("output metadata=%#v %v", facts, err)
				}
				if input.ReferenceVideos[0].StorageKey != "resource:source" {
					t.Fatal("upload mutated original source needed for recovery/audio")
				}
			}
			before := uploads.Load()
			runtime.Call.ProviderRequestID = "original-task"
			if _, err := Execute(WithRuntime(context.Background(), runtime), input); err != nil {
				t.Fatal(err)
			}
			if creates.Load() != 1 || uploads.Load() != before || downloads.Load() != 2 {
				t.Fatal("resume uploaded or submitted generation again")
			}
		})
	}
}

func TestUpscaleRejectsOwnerAndInvalidInputBeforePaidSubmit(t *testing.T) {
	input := Input{Mode: "image", Prompt: "AI 超分", Config: Config{BaseURL: "https://enterprise.beefapi.com/v1", Model: "topaz-standard-v2"}, Upscale: &UpscaleOptions{Factor: 2}, ReferenceImages: []Media{{StorageKey: "resource:source"}}}
	for _, factor := range []int{0, 5} {
		changed := input
		changed.Upscale = &UpscaleOptions{Factor: factor}
		if ValidateUpscaleInput(changed) == nil {
			t.Fatal("invalid factor accepted")
		}
	}
	runtime := taskRuntime(func(r *Runtime) {
		r.Call.UserID = "foreign"
		r.Resources = upscaleResources{kind: "image", data: upscalePNG(t, 64, 48)}
	})
	_, err := PrepareUpscale(WithRuntime(context.Background(), runtime), input)
	if err == nil {
		t.Fatal("foreign owner accepted")
	}
}

func TestUpscaleOriginalStatusIdentityAndCancellation(t *testing.T) {
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
	status, returnedID := "unknown", "original-task"
	var creates, cancels atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/media-upscale/tasks/original-task/cancel" {
			cancels.Add(1)
			_, _ = io.WriteString(w, `{"task_id":"original-task","cancellation_requested":true,"refunded":false}`)
			return
		}
		if r.Method == "POST" {
			creates.Add(1)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"task_id": returnedID, "status": status, "model": "topaz-standard-v2", "media_type": "image"})
	}))
	defer server.Close()
	config := Config{BaseURL: server.URL, APIKey: "synthetic", Model: "topaz-standard-v2"}
	ctx := WithRuntime(context.Background(), taskRuntime(func(r *Runtime) { r.Endpoints.BeefAPIVideoBaseURL = server.URL }))
	result, observed, err := QueryUpscaleResult(ctx, Input{Mode: "image", Config: config}, "original-task")
	if err != nil || result != nil || observed != "unknown" {
		t.Fatalf("unknown result=%v status=%s err=%v", result, observed, err)
	}
	if err := CancelUpscale(ctx, config, "original-task"); err != nil {
		t.Fatal(err)
	}
	status = "cancelling"
	if got, err := QueryUpscaleStatus(ctx, config, "original-task"); err != nil || got != "cancelling" {
		t.Fatal(got, err)
	}
	returnedID = "different-task"
	if _, err := QueryUpscaleStatus(ctx, config, "original-task"); err == nil {
		t.Fatal("mismatched receipt accepted")
	}
	if creates.Load() != 0 || cancels.Load() != 1 {
		t.Fatal("status/recovery submitted another generation")
	}
	if _, err := upscaleTaskPath("../escape"); err == nil {
		t.Fatal("unsafe id accepted")
	}
}

func TestUpscalePreservesExistingAudioBytes(t *testing.T) {
	data := upscaleClip(t, 64, 48, true)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	output, err := PreserveUpscaleAudio(ctx, data, data)
	if err != nil || !bytes.Equal(data, output) {
		t.Fatal("existing audio was unnecessarily rewritten", err)
	}
}

func TestUpscaleShortAudioDoesNotTrimVideoOrChangeFPS(t *testing.T) {
	bin, err := mediatools.ResolveFFmpeg()
	if err != nil {
		t.Skip("ffmpeg unavailable")
	}
	probe, err := mediatools.ResolveFFprobe()
	if err != nil {
		t.Skip("ffprobe unavailable")
	}
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "source.mp4")
	outputPath := filepath.Join(dir, "output.mp4")
	for _, item := range []struct {
		path  string
		audio bool
	}{{sourcePath, true}, {outputPath, false}} {
		args := []string{"-nostdin", "-v", "error", "-f", "lavfi", "-i", "testsrc2=size=64x48:rate=24:duration=2"}
		if item.audio {
			args = append(args, "-f", "lavfi", "-i", "sine=frequency=440:duration=0.5")
		}
		args = append(args, "-c:v", "libx264", "-pix_fmt", "yuv420p")
		if item.audio {
			args = append(args, "-c:a", "aac")
		}
		args = append(args, item.path)
		if data, err := exec.Command(bin, args...).CombinedOutput(); err != nil {
			t.Fatal(string(data), err)
		}
	}
	source, _ := os.ReadFile(sourcePath)
	output, _ := os.ReadFile(outputPath)
	result, err := PreserveUpscaleAudio(context.Background(), source, output)
	if err != nil {
		t.Fatal(err)
	}
	path, _ := upscaleTempFile(dir, "restored.mp4", result)
	facts, err := mediatools.ProbeLocal(context.Background(), path)
	if err != nil || !facts.HasAudio || facts.DurationMs < 1950 || facts.DurationMs > 2050 {
		t.Fatalf("short soundtrack trimmed video: %#v %v", facts, err)
	}
	fps, err := exec.Command(probe, "-v", "error", "-select_streams", "v:0", "-show_entries", "stream=r_frame_rate", "-of", "csv=p=0", path).Output()
	if err != nil || string(bytes.TrimSpace(fps)) != "24/1" {
		t.Fatal("frame rate changed", string(fps), err)
	}
}
