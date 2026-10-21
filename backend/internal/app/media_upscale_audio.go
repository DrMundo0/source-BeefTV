package app

import (
	"context"
	"errors"
	localasset "infinite-canvas/backend/internal/asset"
	"infinite-canvas/backend/internal/mediatools"
	"math"
	"os"
	"os/exec"
	"path/filepath"
)

var errTopazAudioPreservation = errors.New("视频音轨无法安全保留：请检查原素材、音画时长及媒体工具；未交付可能丢失音轨的结果")

// topazSourceVideo opens the owned original through FileStore's traversal and
// symlink checks. No source copy or new temporary directory is needed.
func (s *Service) topazSourceVideo(ctx context.Context, user, id string) (*os.File, mediatools.MediaMetadata, error) {
	var facts mediatools.MediaMetadata
	source, e := s.Resource(user, id)
	if e != nil || source == nil || source.Provider != "local" {
		return nil, facts, errTopazAudioPreservation
	}
	file, e := localasset.NewFileStore(s.dataDir).Open(source.ObjectKey)
	if e != nil {
		return nil, facts, errTopazAudioPreservation
	}
	facts, e = mediatools.ProbeLocal(ctx, file.Name())
	if e != nil || !facts.HasVideo || facts.DurationMs <= 0 {
		file.Close()
		return nil, facts, errTopazAudioPreservation
	}
	return file, facts, nil
}

// preserveTopazVideoAudio runs before RecoverOwned commits output bytes. The
// existing artifact-downloads directory owns the short-lived intermediate files.
// MP4-incompatible audio is refused rather than silently dropped or re-encoded.
func (s *Service) preserveTopazVideoAudio(ctx context.Context, user, sourceID, identity string, data []byte) ([]byte, error) {
	source, sourceFacts, e := s.topazSourceVideo(ctx, user, sourceID)
	if e != nil {
		return nil, e
	}
	defer source.Close()
	dir := filepath.Dir((generationDeliveryMediaAdapter{s}).artifactDownloadPartialPath(user, identity))
	return remuxTopazVideoAudio(ctx, source, sourceFacts, dir, data)
}
func remuxTopazVideoAudio(ctx context.Context, source *os.File, sourceFacts mediatools.MediaMetadata, dir string, data []byte) ([]byte, error) {
	if !sourceFacts.HasAudio {
		return data, nil
	}
	cloud, e := os.CreateTemp(dir, ".topaz-cloud-*.mp4")
	if e != nil {
		return nil, errTopazAudioPreservation
	}
	defer os.Remove(cloud.Name())
	defer cloud.Close()
	if _, e = cloud.Write(data); e != nil {
		return nil, errTopazAudioPreservation
	}
	if e = cloud.Close(); e != nil {
		return nil, errTopazAudioPreservation
	}
	facts, e := mediatools.ProbeLocal(ctx, cloud.Name())
	if e != nil || !facts.HasVideo {
		return nil, errTopazAudioPreservation
	}
	if facts.HasAudio {
		return data, nil
	}
	if !topazAudioDurationsMatch(sourceFacts.DurationMs, facts.DurationMs) {
		return nil, errTopazAudioPreservation
	}
	output, e := os.CreateTemp(dir, ".topaz-remux-*.mp4")
	if e != nil {
		return nil, errTopazAudioPreservation
	}
	name := output.Name()
	output.Close()
	defer os.Remove(name)
	bin, e := mediatools.ResolveFFmpeg()
	if e != nil {
		return nil, errTopazAudioPreservation
	}
	cmd := exec.CommandContext(ctx, bin, "-hide_banner", "-loglevel", "error", "-nostdin", "-y", "-protocol_whitelist", "file,pipe", "-i", cloud.Name(), "-protocol_whitelist", "file,pipe", "-i", source.Name(), "-map", "0:v:0", "-map", "1:a", "-c", "copy", "-movflags", "+faststart", name)
	mediatools.HideConsole(cmd)
	if cmd.Run() != nil {
		return nil, errTopazAudioPreservation
	}
	combined, e := mediatools.ProbeLocal(ctx, name)
	if e != nil || !combined.HasVideo || !combined.HasAudio || combined.Width != facts.Width || combined.Height != facts.Height || !topazAudioDurationsMatch(facts.DurationMs, combined.DurationMs) {
		return nil, errTopazAudioPreservation
	}
	result, e := os.ReadFile(name)
	if e != nil {
		return nil, errTopazAudioPreservation
	}
	return result, nil
}
func topazAudioDurationsMatch(source, result int64) bool {
	return source > 0 && result > 0 && math.Abs(float64(source-result)) <= math.Max(100, float64(source)*0.02)
}
