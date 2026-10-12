package generation

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"infinite-canvas/backend/internal/outbound"
)

type fakeDeliveryPort struct {
	mu      sync.Mutex
	reports []outbound.ResumableProgress
	cleared int32
	taskIDs []string
}

func (f *fakeDeliveryPort) ReportDelivery(taskID string, progress outbound.ResumableProgress) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.taskIDs = append(f.taskIDs, taskID)
	f.reports = append(f.reports, progress)
}

func (f *fakeDeliveryPort) ClearDelivery(string) { atomic.AddInt32(&f.cleared, 1) }

func (f *fakeDeliveryPort) snapshot() []outbound.ResumableProgress {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]outbound.ResumableProgress(nil), f.reports...)
}

func downloadFrom(url string) func(context.Context) ([]byte, string, error) {
	return func(ctx context.Context) ([]byte, string, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, "", err
		}
		return DoBinary(req)
	}
}

func fastPolicy(tries int, sleep func(context.Context, time.Duration) error) VideoPollPolicy {
	return VideoPollPolicy{Interval: time.Millisecond, MaxDownloadTries: tries, Sleep: sleep}
}

func TestRunVideoDownloadReportsStreamedProgressAndClearsOnce(t *testing.T) {
	t.Setenv("CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS", "127.0.0.1")
	body := bytes.Repeat([]byte("v"), 128<<10)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "video/mp4")
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		for offset := 0; offset < len(body); offset += 32 << 10 {
			_, _ = w.Write(body[offset : offset+32<<10])
			w.(http.Flusher).Flush()
			time.Sleep(30 * time.Millisecond)
		}
	}))
	defer server.Close()
	port := &fakeDeliveryPort{}
	ctx := WithRuntime(context.Background(), Runtime{Delivery: port})

	data, mimeType, err := RunVideoDownload(ctx, "task-1", fastPolicy(1, SleepContext), downloadFrom(server.URL))
	if err != nil {
		t.Fatalf("download failed: %v", err)
	}
	if !bytes.Equal(data, body) || mimeType != "video/mp4" {
		t.Fatalf("unexpected payload: %d bytes, mime %q", len(data), mimeType)
	}
	reports := port.snapshot()
	if len(reports) < 3 {
		t.Fatalf("expected start, transfer and final reports, got %+v", reports)
	}
	first := reports[0]
	if first.State != "downloading" || first.DownloadedBytes != 0 || first.TotalBytes != -1 || first.Attempt != 1 {
		t.Fatalf("first report should announce the attempt, got %+v", first)
	}
	var previous int64
	for _, report := range reports[1:] {
		if report.State != "downloading" || report.Attempt != 1 {
			t.Fatalf("transfer report has wrong state: %+v", report)
		}
		if report.DownloadedBytes < previous {
			t.Fatalf("downloaded bytes went backwards: %+v", reports)
		}
		previous = report.DownloadedBytes
	}
	last := reports[len(reports)-1]
	if last.DownloadedBytes != int64(len(body)) || last.TotalBytes != int64(len(body)) {
		t.Fatalf("final report should show the complete body, got %+v", last)
	}
	if got := atomic.LoadInt32(&port.cleared); got != 1 {
		t.Fatalf("expected one clear, got %d", got)
	}
	for _, taskID := range port.taskIDs {
		if taskID != "task-1" {
			t.Fatalf("progress reported for %q", taskID)
		}
	}
}

func TestRunVideoDownloadReportsWaitingBeforeRetrySleep(t *testing.T) {
	t.Setenv("CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS", "127.0.0.1")
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			http.Error(w, "object not ready", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "video/mp4")
		_, _ = w.Write([]byte("ready"))
	}))
	defer server.Close()
	port := &fakeDeliveryPort{}
	ctx := WithRuntime(context.Background(), Runtime{Delivery: port})
	sleptAfter := -1
	sleep := func(context.Context, time.Duration) error {
		sleptAfter = len(port.snapshot())
		return nil
	}

	data, _, err := RunVideoDownload(ctx, "task-2", fastPolicy(3, sleep), downloadFrom(server.URL))
	if err != nil || string(data) != "ready" {
		t.Fatalf("expected retry to succeed, got %q, %v", data, err)
	}
	reports := port.snapshot()
	waiting := -1
	for index, report := range reports {
		if report.State == "waiting" {
			waiting = index
			if report.Attempt != 2 || report.DownloadedBytes != 0 || report.TotalBytes != -1 {
				t.Fatalf("waiting report should target attempt 2 with no bytes, got %+v", report)
			}
			break
		}
	}
	if waiting < 0 {
		t.Fatalf("no waiting report before retry: %+v", reports)
	}
	if sleptAfter != waiting+1 {
		t.Fatalf("waiting report must precede the retry sleep: waiting=%d sleptAfter=%d", waiting, sleptAfter)
	}
	secondStart := false
	for _, report := range reports[waiting+1:] {
		if report.State == "downloading" && report.Attempt == 2 {
			secondStart = true
		}
	}
	if !secondStart {
		t.Fatalf("second attempt did not announce itself: %+v", reports)
	}
	if got := atomic.LoadInt32(&port.cleared); got != 1 {
		t.Fatalf("expected one clear, got %d", got)
	}
}

func TestRunVideoDownloadClearsOnFinalFailure(t *testing.T) {
	t.Setenv("CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS", "127.0.0.1")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "gone for good", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	port := &fakeDeliveryPort{}
	ctx := WithRuntime(context.Background(), Runtime{Delivery: port})

	if _, _, err := RunVideoDownload(ctx, "task-3", fastPolicy(2, SleepContext), downloadFrom(server.URL)); err == nil {
		t.Fatal("expected final failure")
	}
	if got := atomic.LoadInt32(&port.cleared); got != 1 {
		t.Fatalf("expected one clear after final failure, got %d", got)
	}
}

func TestDoBinaryWithoutObserverMatchesObservedRead(t *testing.T) {
	t.Setenv("CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS", "127.0.0.1")
	body := bytes.Repeat([]byte("x"), 70<<10)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(body)
	}))
	defer server.Close()
	request := func(ctx context.Context) []byte {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
		if err != nil {
			t.Fatal(err)
		}
		data, mimeType, err := DoBinary(req)
		if err != nil || mimeType != "image/png" {
			t.Fatalf("read failed: %v (mime %q)", err, mimeType)
		}
		return data
	}
	plain := request(context.Background())
	var observed int
	observedData := request(withDownloadObserver(context.Background(), func(int64, int64) { observed++ }))
	if !bytes.Equal(plain, body) || !bytes.Equal(observedData, body) {
		t.Fatal("observed and plain reads must return the same bytes")
	}
	if observed == 0 {
		t.Fatal("observer was never called")
	}
}

// DoBinary reads through io.LimitReader(body, limit+1) and keeps only limit bytes;
// the observer must never report the overflow byte as retained progress.
func TestProgressReaderNeverReportsMoreThanRetainedBytes(t *testing.T) {
	const keep = 4096
	body := bytes.Repeat([]byte("o"), keep+1)
	var observed []int64
	reader := io.LimitReader(newProgressReader(bytes.NewReader(body), int64(len(body)), keep, func(downloaded, _ int64) {
		observed = append(observed, downloaded)
	}), keep+1)
	data, err := io.ReadAll(reader)
	if err != nil || len(data) != keep+1 {
		t.Fatalf("read %d bytes, err %v", len(data), err)
	}
	if len(observed) == 0 {
		t.Fatal("overflow read was not observed")
	}
	for _, count := range observed {
		if count > keep {
			t.Fatalf("observed %d bytes, retained limit is %d", count, keep)
		}
	}
	if last := observed[len(observed)-1]; last != keep {
		t.Fatalf("final observed count = %d, want retained length %d", last, keep)
	}
}

// A count that drops within one attempt means the body restarted (the proxy
// retry after a failed direct read); the speed must be recomputed from scratch.
func TestVideoDownloadReporterResetsSpeedWhenBodyRestarts(t *testing.T) {
	port := &fakeDeliveryPort{}
	reporter := newVideoDownloadReporter(port, "task-restart")
	reporter.attempt = 1
	reporter.report("downloading", 1000, -1)
	time.Sleep(100 * time.Millisecond)
	reporter.report("downloading", 1010, -1)
	reporter.report("downloading", 1, -1) // restart from zero
	time.Sleep(150 * time.Millisecond)
	reporter.report("downloading", 2001, -1)

	reports := port.snapshot()
	last := reports[len(reports)-1]
	if last.DownloadedBytes != 2001 {
		t.Fatalf("unexpected last report %+v", last)
	}
	// Measured only from the restart: about 2000 bytes over 150 ms. Keeping the
	// pre-restart baseline would give about 1000 bytes over 300 ms, far lower.
	if last.BytesPerSecond < 10_000 {
		t.Fatalf("speed was not restarted after the body reset: %+v", last)
	}
}
