package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	localasset "infinite-canvas/backend/internal/asset"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/outbound"
	"infinite-canvas/backend/internal/taskruntime"
	"infinite-canvas/backend/internal/topaz"
	"io"
	"math"
	"net"
	"net/http"
	"strings"
	"time"
)

type MediaUpscaleRequest = topaz.Request

func (s *Service) MediaUpscaleCapabilities() map[string]any {
	return map[string]any{"modes": topaz.Modes(), "cloudProcessing": true, "maxVideoDurationMs": 300000}
}
func (s *Service) topazKey(channel string) (string, error) {
	raw, e := s.ReadLocalModelConfig()
	if e != nil {
		return "", errors.New("无法读取本地渠道配置")
	}
	var cfg struct {
		Channels []struct {
			ID      string `json:"id"`
			BaseURL string `json:"baseUrl"`
			APIKey  string `json:"apiKey"`
			Enabled bool   `json:"enabled"`
		} `json:"channels"`
	}
	if json.Unmarshal(raw, &cfg) != nil {
		return "", errors.New("渠道配置无效")
	}
	for _, c := range cfg.Channels {
		if c.ID == channel && c.Enabled && strings.TrimRight(c.BaseURL, "/") == "https://queue.fal.run" && strings.TrimSpace(c.APIKey) != "" {
			return c.APIKey, nil
		}
	}
	return "", BadAuthRequest("请选择已启用且配置密钥的官方 fal 渠道")
}
func (s *Service) validateUpscale(ctx context.Context, user string, r topaz.Request) (*model.Resource, error) {
	v, e := s.EnsureResourceMediaMetadata(ctx, user, r.ResourceID)
	if e != nil || v == nil || v.Status != model.ResourceStatusReady {
		return nil, BadAuthRequest("输入素材不存在或未就绪")
	}
	if e = topaz.Validate(r, v.Kind); e != nil {
		return nil, BadAuthRequest(e.Error())
	}
	if v.Width <= 0 || v.Height <= 0 {
		return nil, BadAuthRequest("无法读取素材尺寸")
	}
	if v.Kind == "video" && (v.DurationMs <= 0 || v.DurationMs > 300000) {
		return nil, BadAuthRequest("视频时长必须可识别且不超过 5 分钟")
	}
	allowed := v.MimeType == "image/png" || v.MimeType == "image/jpeg" || v.MimeType == "image/webp" || v.MimeType == "video/mp4" || v.MimeType == "video/quicktime"
	if !allowed {
		return nil, BadAuthRequest("首版支持 JPEG、PNG、WebP 图片及 MP4、MOV 视频")
	}
	if _, e = s.topazKey(r.ChannelID); e != nil {
		return nil, e
	}
	return v, nil
}
func (s *Service) EstimateMediaUpscale(ctx context.Context, user string, r topaz.Request) (map[string]any, error) {
	v, e := s.validateUpscale(ctx, user, r)
	if e != nil {
		return nil, e
	}
	w, h := int(math.Round(float64(v.Width)*r.Scale)), int(math.Round(float64(v.Height)*r.Scale))
	var cost any
	reason := "视频计量规则尚未核定，费用以 fal 最终账单为准"
	if v.Kind == "image" {
		block := 24.0
		if r.Mode == "generative" {
			block = 8
		}
		cost = 0.08 * math.Ceil(float64(w)*float64(h)/1e6/block)
		reason = "公开价格估算，按输出百万像素向上取整；以 fal 最终账单为准"
	}
	return map[string]any{"mediaType": v.Kind, "source": map[string]any{"width": v.Width, "height": v.Height, "durationMs": v.DurationMs}, "output": map[string]any{"width": w, "height": h, "durationMs": v.DurationMs}, "estimatedCostUSD": cost, "costReason": reason, "priceCheckedAt": "2026-10-20", "cloudProcessing": true}, nil
}
func (s *Service) CreateMediaUpscale(ctx context.Context, user string, r topaz.Request) (*model.Task, error) {
	if strings.TrimSpace(r.ClientOperationID) == "" {
		return nil, BadAuthRequest("缺少本次确认的操作 ID")
	}
	v, e := s.validateUpscale(ctx, user, r)
	if e != nil {
		return nil, e
	}
	raw, _ := json.Marshal(r)
	var input map[string]any
	_ = json.Unmarshal(raw, &input)
	input["mediaType"] = v.Kind
	input["metadata"] = map[string]any{"clientOperationId": r.ClientOperationID, "nodeId": r.NodeID, "source": "canvas", "operation": "media.upscale.topaz"}
	return s.taskDomain().CreateMediaUpscale(user, r.ProjectID, r.Model, input)
}

type topazState struct {
	topaz.Request
	MediaType         string         `json:"mediaType"`
	Metadata          map[string]any `json:"metadata"`
	Submitted         bool           `json:"submitted,omitempty"`
	Receipt           topaz.Receipt  `json:"receipt,omitempty"`
	RecoveryFailures  int            `json:"recoveryFailures,omitempty"`
	RecoveryStartedAt *time.Time     `json:"recoveryStartedAt,omitempty"`
}

func (s *Service) saveTopaz(task *model.Task, state topazState) error {
	b, e := json.Marshal(state)
	if e != nil {
		return e
	}
	task.InputJSON = string(b)
	task.ProviderRequestID = state.Receipt.RequestID
	return s.repo.SaveMediaUpscaleState(task)
}
func (w *taskWorkerCoordinator) processTopaz(session taskruntime.Session) taskruntime.Outcome {
	task, ctx, s := session.Task(), session.Context(), w.service
	fail := func(msg string) taskruntime.Outcome {
		stage := "超分失败"
		if strings.Contains(msg, "音轨无法安全保留") {
			stage = "音轨保留失败：未交付静音结果"
		}
		if strings.Contains(msg, "提交结果未确认") {
			stage = "submission_unknown"
		}
		e := w.failTimelineTask(task, stage, msg)
		return taskruntime.Outcome{Kind: taskruntime.KindFailed, Applied: e == nil, Err: e}
	}
	var state topazState
	if json.Unmarshal([]byte(task.InputJSON), &state) != nil {
		return fail("超分参数无效")
	}
	key, e := s.topazKey(state.ChannelID)
	if e != nil {
		return fail("fal 渠道配置不可用")
	}
	client := topaz.Client{Key: key}
	if state.RecoveryStartedAt == nil {
		now := time.Now()
		state.RecoveryStartedAt = &now
		if e = s.saveTopaz(task, state); e != nil {
			return taskruntime.Outcome{Kind: taskruntime.KindFailed, Err: e}
		}
	}
	if time.Since(*state.RecoveryStartedAt) > 24*time.Hour {
		return fail("Topaz 原任务恢复超过 24 小时，已停止自动回查；请核对 fal 原任务，勿重复提交")
	}

	deferPoll := func() taskruntime.Outcome {
		e := s.repo.DeferRunningTaskForProviderPoll(task.ID, task.LeaseOwner, "等待 Topaz 云端结果", 15*time.Second)
		return taskruntime.Outcome{Kind: taskruntime.KindSuspended, Applied: e == nil, Err: e, ProviderAccepted: state.Receipt.RequestID != ""}
	}
	recoveryError := func(err error, phase string) taskruntime.Outcome {
		httpStatus := 0
		var providerHTTP *topaz.HTTPError
		var downloadHTTP *outbound.HTTPStatusError
		if errors.As(err, &providerHTTP) {
			httpStatus = providerHTTP.StatusCode
		}
		if errors.As(err, &downloadHTTP) {
			httpStatus = downloadHTTP.StatusCode
		}
		task.FailureDiagnostics = &model.TaskFailureDiagnostics{Version: "1", Source: "fal-topaz", Summary: "Topaz " + phase + "失败", HTTPStatus: httpStatus, ProviderTaskID: state.Receipt.RequestID, CapturedAt: time.Now().UTC().Format(time.RFC3339)}

		if errors.Is(err, errTopazAudioPreservation) {
			return fail(errTopazAudioPreservation.Error())
		}
		if !topazRecoveryRetryable(err) {
			return fail("Topaz " + phase + "失败；原任务不会重新提交，请核对素材、存储配额和 fal 任务状态")
		}
		state.RecoveryFailures++
		if state.RecoveryFailures >= 8 {
			return fail("Topaz 原任务恢复连续失败 8 次，已停止自动回查；请核对 fal 原任务和网络，勿重复提交")
		}
		if e := s.saveTopaz(task, state); e != nil {
			return taskruntime.Outcome{Kind: taskruntime.KindFailed, Err: e, ProviderAccepted: state.Receipt.RequestID != ""}
		}
		return deferPoll()
	}
	if state.Receipt.RequestID == "" {
		if state.Submitted {
			return fail("提交结果未确认；为避免重复扣费，已停止自动提交。请先在 fal 控制台核对任务。")
		}
		if state.MediaType == "video" {
			original, _, audioErr := s.topazSourceVideo(ctx, task.UserID, state.ResourceID)
			if audioErr != nil {
				return fail(errTopazAudioPreservation.Error())
			}
			original.Close()
		}
		source, e := s.validateUpscale(ctx, task.UserID, state.Request)
		if e != nil {
			return fail("源素材或参数校验失败")
		}
		_, body, e := s.OpenResource(task.UserID, state.ResourceID)
		if e != nil {
			return fail("无法读取源素材")
		}
		uploaded, e := client.Upload(ctx, body, source.Size, source.MimeType, "beeftv-source."+extensionFromMimeType(source.MimeType))
		_ = body.Close()
		if e != nil {
			return fail("无法上传素材到 fal")
		}
		state.Submitted = true
		if e = s.saveTopaz(task, state); e != nil {
			return taskruntime.Outcome{Kind: taskruntime.KindFailed, Err: e}
		}
		submittedInput := task.InputJSON
		if ctx.Err() != nil {
			return taskruntime.Outcome{Kind: taskruntime.KindCancelled, Err: ctx.Err()}
		}
		// Keep the short submission acknowledgement alive after local cancel so
		// its receipt can still be saved and cancelled upstream.
		submitCtx, submitCancel := context.WithTimeout(context.WithoutCancel(ctx), 25*time.Second)
		defer submitCancel()
		endpoint := "https://queue.fal.run/topaz/upscale/" + state.MediaType + "/" + state.Mode
		if e = client.Do(submitCtx, http.MethodPost, endpoint, topaz.Payload(state.Request, state.MediaType, uploaded), &state.Receipt, true); e != nil {
			return fail("提交结果未确认；为避免重复扣费，请先在 fal 控制台核对任务。")
		}
		if state.Receipt.RequestID == "" || !topaz.QueueURL(state.Receipt.StatusURL) || !topaz.QueueURL(state.Receipt.ResponseURL) || !topaz.QueueURL(state.Receipt.CancelURL) {
			return fail("fal 返回的队列回执无效，请先在控制台核对任务")
		}
		if e = s.saveTopaz(task, state); e != nil {
			latest, lookupErr := s.repo.Task(task.ID)
			if lookupErr == nil && latest.Status == model.TaskStatusCancelled {
				if saveErr := s.repo.SaveCancelledMediaUpscaleReceipt(task, submittedInput); saveErr == nil {
					latest.InputJSON = task.InputJSON
					latest.ProviderRequestID = task.ProviderRequestID
					cancelCtx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
					cancelErr := s.cancelTopaz(cancelCtx, latest)
					cancel()
					return taskruntime.Outcome{Kind: taskruntime.KindCancelled, Applied: cancelErr == nil, Err: cancelErr, ProviderAccepted: true}
				}
			}
			return taskruntime.Outcome{Kind: taskruntime.KindUncertain, Err: e, ProviderAccepted: true}
		}
	}
	var status struct {
		Status string `json:"status"`
	}
	if !topaz.QueueURL(state.Receipt.StatusURL) || !topaz.QueueURL(state.Receipt.ResponseURL) {
		return fail("队列查询地址无效")
	}
	if e = client.Do(ctx, "GET", state.Receipt.StatusURL, nil, &status, true); e != nil {
		return recoveryError(e, "状态查询")
	}
	if status.Status != "COMPLETED" {
		if status.Status == "FAILED" || status.Status == "CANCELLED" {
			return fail("Topaz 云端处理失败或已取消")
		}
		if status.Status != "IN_QUEUE" && status.Status != "IN_PROGRESS" {
			return fail("Topaz 返回了无法识别的任务状态")
		}
		if state.RecoveryFailures > 0 {
			state.RecoveryFailures = 0
			if e = s.saveTopaz(task, state); e != nil {
				return taskruntime.Outcome{Kind: taskruntime.KindFailed, Err: e}
			}
		}
		return deferPoll()
	}
	var output struct {
		Image struct {
			URL string `json:"url"`
		} `json:"image"`
		Video struct {
			URL string `json:"url"`
		} `json:"video"`
	}
	if e = client.Do(ctx, "GET", state.Receipt.ResponseURL, nil, &output, true); e != nil {
		return recoveryError(e, "结果查询")
	}
	url := output.Image.URL
	if state.MediaType == "video" {
		url = output.Video.URL
	}
	if !topaz.PublicURL(url) {
		return fail("Topaz 未返回有效的输出素材")
	}
	resource, e := s.resourceDomain().RecoverOwned(task.UserID, task.ID+":0", func() (localasset.RecoveredArtifact, error) {
		kind, data, mime, name, width, height, duration, err := (generationDeliveryMediaAdapter{s}).decodeGenerationArtifact(task.UserID, task.ID+":0", state.MediaType, url)
		if err != nil {
			return localasset.RecoveredArtifact{}, err
		}
		if kind == "video" {
			data, err = s.preserveTopazVideoAudio(ctx, task.UserID, state.ResourceID, task.ID+":0", data)
			if err != nil {
				return localasset.RecoveredArtifact{}, err
			}
			width, height, duration = probeGeneratedVideoMedia(data)
		}
		if kind != state.MediaType || width <= 0 || height <= 0 || (kind == "video" && duration <= 0) {
			return localasset.RecoveredArtifact{}, errors.New("输出媒体探测失败")
		}
		if ctx.Err() != nil {
			return localasset.RecoveredArtifact{}, ctx.Err()
		}
		return localasset.RecoveredArtifact{Kind: kind, FileName: name, MimeType: mime, Size: int64(len(data)), Width: width, Height: height, DurationMs: duration, Body: bytes.NewReader(data)}, nil
	})
	if e != nil {
		return recoveryError(e, "结果取回")
	}
	if resource.Width <= 0 || resource.Height <= 0 || resource.Kind != state.MediaType {
		return fail("输出媒体探测失败")
	}
	task.ResultJSON = mustJSON(map[string]any{state.MediaType: map[string]any{"resourceId": resource.ID, "mimeType": resource.MimeType, "width": resource.Width, "height": resource.Height, "durationMs": resource.DurationMs}, "resourceId": resource.ID})
	task.Status = model.TaskStatusSucceeded
	task.Stage = "超分完成"
	task.Progress = 100
	now := time.Now()
	task.CompletedAt = &now
	payload := mustJSON(map[string]any{"resourceId": resource.ID, "mimeType": resource.MimeType, "width": resource.Width, "height": resource.Height, "durationMs": resource.DurationMs})
	results := []model.Result{{ID: newID(), UserID: task.UserID, TaskID: task.ID, Kind: state.MediaType, Payload: payload, CreatedAt: now}}
	e = s.repo.SaveTaskCompletion(task, model.TaskStatusRunning, results)
	if e == nil {
		_ = s.DeliverSucceededTask(*task)
	}
	return taskruntime.Outcome{Kind: taskruntime.KindCompleted, Applied: e == nil, Err: e, ProviderAccepted: true}
}

func (s *Service) cancelTopaz(ctx context.Context, task *model.Task) error {
	if e := s.repo.ClaimTaskProviderCancellation(task.UserID, task.ID, time.Now()); e != nil {
		return e
	}
	var state topazState
	if json.Unmarshal([]byte(task.InputJSON), &state) != nil || !topaz.QueueURL(state.Receipt.CancelURL) {
		return s.markProviderCancellationUncertain(task, "缺少有效的 fal 取消回执，请在 fal 控制台核对")
	}
	key, e := s.topazKey(state.ChannelID)
	if e != nil {
		return s.markProviderCancellationUncertain(task, "fal 渠道配置不可用")
	}
	if e = (topaz.Client{Key: key}).Do(ctx, "PUT", state.Receipt.CancelURL, nil, nil, true); e != nil {
		return s.markProviderCancellationUncertain(task, "fal 取消结果未确认，请在控制台核对；取消不保证退款")
	}
	next := time.Now().Add(15 * time.Second)
	return s.repo.UpdateTaskProviderCancellation(task.ID, model.ProviderCancelStatusRequested, model.ProviderCancelStatusRequested, "", &next, nil)
}
func (s *Service) reconcileTopazCancellation(ctx context.Context, task *model.Task) error {
	var state topazState
	if json.Unmarshal([]byte(task.InputJSON), &state) != nil || !topaz.QueueURL(state.Receipt.StatusURL) {
		return s.markProviderCancellationUncertain(task, "fal 取消回执无效")
	}
	key, e := s.topazKey(state.ChannelID)
	if e != nil {
		return s.markProviderCancellationUncertain(task, "fal 渠道配置不可用")
	}
	var status struct {
		Status string `json:"status"`
	}
	e = (topaz.Client{Key: key}).Do(ctx, "GET", state.Receipt.StatusURL, nil, &status, true)
	if e == nil && status.Status == "CANCELLED" {
		now := time.Now()
		return s.repo.UpdateTaskProviderCancellation(task.ID, model.ProviderCancelStatusRequested, model.ProviderCancelStatusConfirmed, "", nil, &now)
	}
	if e == nil && (status.Status == "COMPLETED" || status.Status == "FAILED") {
		return s.markProviderCancellationUncertain(task, "fal 任务已完成或失败，取消未被确认；请核对账单")
	}
	if task.ProviderCancelAttempts >= providerCancellationMaxAttempts {
		return s.markProviderCancellationUncertain(task, "fal 取消状态长时间未确认")
	}
	return s.deferProviderCancellation(task, "")
}

// Only known transient transport failures may retry; validation, quota, decode,
// probe and local storage failures require intervention rather than endless polls.
func topazRecoveryRetryable(err error) bool {
	var temporary interface{ Temporary() bool }
	if errors.As(err, &temporary) {
		return temporary.Temporary()
	}
	var network net.Error
	if errors.As(err, &network) {
		return true
	}
	return errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) || errors.Is(err, context.DeadlineExceeded)
}

// RecoverMediaUpscale resumes retrieval of an already accepted request. It never
// clears the receipt or submitted marker and cannot submit a fresh inference.
func (s *Service) RecoverMediaUpscale(ctx context.Context, user, id string) (*model.Task, error) {
	task, e := s.repo.TaskForUser(user, id)
	if e != nil {
		return nil, e
	}
	if task.Type != "media_upscale" || task.Status != model.TaskStatusFailed {
		return nil, BadAuthRequest("仅可恢复已失败的 Topaz 原任务结果")
	}
	var state topazState
	if json.Unmarshal([]byte(task.InputJSON), &state) != nil || !state.Submitted || state.Receipt.RequestID == "" || state.Receipt.RequestID != task.ProviderRequestID || !topaz.QueueURL(state.Receipt.StatusURL) || !topaz.QueueURL(state.Receipt.ResponseURL) {
		return nil, BadAuthRequest("缺少已确认的 fal 原任务回执，无法安全恢复；请到 fal 控制台核对")
	}
	if _, e = s.topazKey(state.ChannelID); e != nil {
		return nil, e
	}
	if s.IsDraining() {
		return nil, BadAuthRequest("应用正在退出，请稍后恢复")
	}
	policy, e := s.RuntimePolicy()
	if e != nil {
		return nil, e
	}
	expected := task.InputJSON
	state.RecoveryFailures = 0
	now := time.Now()
	state.RecoveryStartedAt = &now
	raw, e := json.Marshal(state)
	if e != nil {
		return nil, e
	}
	task.InputJSON = string(raw)
	// Publish runnable state and reset its download deadline under the same
	// lock used by beginGenerationDownload, so a worker cannot reuse expiry.
	s.deliveryControlMu.Lock()
	e = s.repo.RecoverMediaUpscaleTask(task, expected, policy.Task.ActiveTaskLimit)
	if e == nil {
		delete(s.deliveryDeadlines, task.ID+":0")
	}
	s.deliveryControlMu.Unlock()
	if e != nil {
		return nil, e
	}
	task, e = s.repo.TaskForUser(user, id)
	if e != nil {
		return nil, e
	}
	_ = s.log(user, id, "info", "恢复 Topaz 原任务结果，不重新提交推理", "")
	return taskForOutput(*task), nil
}
