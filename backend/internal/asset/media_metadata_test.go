package asset

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"path/filepath"
	"testing"

	"infinite-canvas/backend/internal/mediatools"
)

func audioFixture() []byte {
	var b bytes.Buffer
	b.WriteString("RIFF")
	_ = binary.Write(&b, binary.LittleEndian, uint32(36+16000))
	b.WriteString("WAVEfmt ")
	for _, value := range []any{uint32(16), uint16(1), uint16(1), uint32(8000), uint32(16000), uint16(2), uint16(16)} {
		_ = binary.Write(&b, binary.LittleEndian, value)
	}
	b.WriteString("data")
	_ = binary.Write(&b, binary.LittleEndian, uint32(16000))
	b.Write(make([]byte, 16000))
	return b.Bytes()
}

func TestOwnedAudioMetadataRepairsLegacyAndPersists(t *testing.T) {
	if _, err := mediatools.ResolveFFprobe(); err != nil {
		t.Skip(err)
	}
	svc, repo, _ := newTestDomain(t)
	data := audioFixture()
	resource, _, err := svc.Store("owner", "audio", "voice.wav", "audio/wav", int64(len(data)), 0, 0, 0, bytes.NewReader(data), nil)
	if err != nil || resource.DurationMs != 1000 {
		t.Fatalf("ingest: %+v %v", resource, err)
	}
	resource.DurationMs = 0
	if err := repo.SaveResource(resource); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.EnsureMediaMetadata(context.Background(), "other", resource.ID); err == nil {
		t.Fatal("foreign owner accepted")
	}
	repaired, err := svc.EnsureMediaMetadata(context.Background(), "owner", resource.ID)
	if err != nil || repaired.DurationMs != 1000 {
		t.Fatalf("repair: %+v %v", repaired, err)
	}
	persisted, err := repo.ResourceForUser("owner", resource.ID)
	if err != nil || persisted.DurationMs != 1000 {
		t.Fatalf("not persisted: %+v %v", persisted, err)
	}
	t.Setenv(mediatools.FFprobePathEnv, filepath.Join(t.TempDir(), "missing"))
	if _, err := svc.EnsureMediaMetadata(context.Background(), "owner", resource.ID); err != nil {
		t.Fatalf("complete facts need no probe: %v", err)
	}
}

func TestMissingMediaFactsFailWithoutLosingBytes(t *testing.T) {
	svc, repo, _ := newTestDomain(t)
	t.Setenv(mediatools.FFprobePathEnv, filepath.Join(t.TempDir(), "missing"))
	resource, _, err := svc.Store("owner", "audio", "voice.wav", "audio/wav", 3, 0, 0, 0, bytes.NewReader([]byte("bad")), nil)
	if err != nil || resource.Status != "ready" {
		t.Fatalf("original bytes lost: %+v %v", resource, err)
	}
	if _, err := svc.EnsureMediaMetadata(context.Background(), "owner", resource.ID); err == nil {
		t.Fatal("missing tool accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := svc.EnsureMediaMetadata(ctx, "owner", resource.ID); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel = %v", err)
	}
	persisted, err := repo.ResourceForUser("owner", resource.ID)
	if err != nil || persisted.DurationMs != 0 || persisted.Status != "ready" {
		t.Fatalf("failed probe changed facts: %+v %v", persisted, err)
	}
}
