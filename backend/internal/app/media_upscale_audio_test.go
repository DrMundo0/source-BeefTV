package app

import (
	"context"
	"errors"
	"infinite-canvas/backend/internal/mediatools"
	"os"
	"os/exec"
	"testing"
)

func TestTopazAudioDurationSafety(t *testing.T) {
	for _, tc := range []struct {
		source, result int64
		valid          bool
	}{{10000, 10000, true}, {10000, 10100, true}, {10000, 12000, false}, {300000, 305000, true}, {300000, 310000, false}, {0, 1000, false}, {1000, 0, false}} {
		if got := topazAudioDurationsMatch(tc.source, tc.result); got != tc.valid {
			t.Fatalf("%d/%d matched=%v", tc.source, tc.result, got)
		}
	}
	if topazRecoveryRetryable(errTopazAudioPreservation) {
		t.Fatal("unsafe audio must not retry indefinitely")
	}
}

// Native verification uses unique files in the existing OS temp directory;
// every file is removed and no directory or app artifact is created.
func TestTopazAudioNativeRemux(t *testing.T) {
	bin, e := mediatools.ResolveFFmpeg()
	if e != nil {
		t.Skip(e)
	}
	if _, e = mediatools.ResolveFFprobe(); e != nil {
		t.Skip(e)
	}
	tempFile := func() *os.File {
		f, e := os.CreateTemp(os.TempDir(), "beeftv-topaz-audio-check-*.mp4")
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { f.Close(); os.Remove(f.Name()) })
		return f
	}
	source := tempFile()
	source.Close()
	cloud := tempFile()
	cloud.Close()
	run := func(args ...string) {
		cmd := exec.Command(bin, args...)
		mediatools.HideConsole(cmd)
		if out, e := cmd.CombinedOutput(); e != nil {
			t.Fatalf("ffmpeg %v: %s", e, out)
		}
	}
	run("-v", "error", "-y", "-f", "lavfi", "-i", "color=c=red:s=64x48:r=5:d=1", "-f", "lavfi", "-i", "sine=frequency=440:duration=1", "-c:v", "libx264", "-c:a", "aac", "-shortest", source.Name())
	run("-v", "error", "-y", "-i", source.Name(), "-an", "-c:v", "copy", cloud.Name())
	facts, e := mediatools.ProbeLocal(context.Background(), source.Name())
	if e != nil || !facts.HasAudio {
		t.Fatalf("source facts %+v %v", facts, e)
	}
	data, e := os.ReadFile(cloud.Name())
	if e != nil {
		t.Fatal(e)
	}
	result, e := remuxTopazVideoAudio(context.Background(), source, facts, os.TempDir(), data)
	if e != nil {
		t.Fatal(e)
	}
	output := tempFile()
	if _, e = output.Write(result); e != nil {
		t.Fatal(e)
	}
	output.Close()
	got, e := mediatools.ProbeLocal(context.Background(), output.Name())
	if e != nil || !got.HasAudio || !got.HasVideo || got.Width != 64 || got.Height != 48 {
		t.Fatalf("remux facts %+v %v", got, e)
	}
	facts.DurationMs = 10000
	if _, e = remuxTopazVideoAudio(context.Background(), source, facts, os.TempDir(), data); !errors.Is(e, errTopazAudioPreservation) {
		t.Fatalf("unsafe duration accepted %v", e)
	}
}
