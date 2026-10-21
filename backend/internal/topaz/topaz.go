// Package topaz implements the narrow fal Topaz request contract.
package topaz

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Request struct {
	ResourceID          string  `json:"resourceId"`
	ChannelID           string  `json:"channelId"`
	Mode                string  `json:"mode"`
	Model               string  `json:"model"`
	Scale               float64 `json:"scale"`
	OutputFormat        string  `json:"outputFormat,omitempty"`
	EnhancementStrength string  `json:"enhancementStrength,omitempty"`
	ClientOperationID   string  `json:"clientOperationId,omitempty"`
	ProjectID           string  `json:"projectId,omitempty"`
	NodeID              string  `json:"nodeId,omitempty"`
}
type Mode struct {
	MediaType     string   `json:"mediaType"`
	Mode          string   `json:"mode"`
	Models        []string `json:"models"`
	Scales        []int    `json:"scales"`
	OutputFormats []string `json:"outputFormats"`
	FixedScale    int      `json:"fixedScale,omitempty"`
}

func Modes() []Mode {
	return []Mode{
		{"image", "precision", []string{"Standard V2", "High Fidelity V3", "Low Resolution V2"}, []int{1, 2, 3, 4}, []string{"png", "jpeg"}, 0},
		{"image", "generative", []string{"Wonder 3.5"}, []int{1, 2, 3, 4}, []string{"png", "jpeg"}, 0},
		{"image", "transparent", []string{"Transparent"}, []int{4}, []string{"png"}, 4},
		{"video", "precision", []string{"Proteus"}, []int{1, 2, 3, 4}, []string{"mp4"}, 0},
		{"video", "generative", []string{"Starlight Fast 2", "Starlight Precise 2.6"}, []int{1, 2, 3, 4}, []string{"mp4"}, 0},
	}
}
func Validate(r Request, kind string) error {
	if r.ResourceID == "" || r.ChannelID == "" || math.IsNaN(r.Scale) || math.IsInf(r.Scale, 0) || r.Scale < 1 || r.Scale > 4 {
		return errors.New("请选择素材、渠道及 1–4 倍倍率")
	}
	for _, m := range Modes() {
		if m.MediaType != kind || m.Mode != r.Mode {
			continue
		}
		valid := false
		for _, v := range m.Models {
			valid = valid || v == r.Model
		}
		if !valid {
			break
		}
		if m.FixedScale > 0 && r.Scale != 4 {
			return errors.New("透明图片固定放大 4 倍")
		}
		if r.OutputFormat != "" {
			valid = false
			for _, f := range m.OutputFormats {
				valid = valid || f == r.OutputFormat
			}
			if !valid {
				return errors.New("输出格式不受支持")
			}
		}
		if r.EnhancementStrength != "" && (kind != "image" || r.Mode != "generative" || (r.EnhancementStrength != "low" && r.EnhancementStrength != "medium" && r.EnhancementStrength != "high")) {
			return errors.New("增强强度参数无效")
		}
		return nil
	}
	return errors.New("此素材的模式或模型不受支持")
}
func Payload(r Request, kind, source string) map[string]any {
	p := map[string]any{kind + "_url": source}
	if r.Mode != "transparent" {
		p["model"] = r.Model
		p["upscale_factor"] = r.Scale
	}
	if kind == "video" {
		p["H264_output"] = true
		return p
	}
	if r.Mode != "transparent" {
		f := r.OutputFormat
		if f == "" {
			f = "png"
		}
		p["output_format"] = f
	}
	if r.Mode == "precision" {
		p["face_enhancement"] = false
	}
	if r.Mode == "generative" {
		v := r.EnhancementStrength
		if v == "" {
			v = "low"
		}
		p["enhancement_strength"] = v
		p["face_enhancement"] = false
	}
	return p
}

type Receipt struct {
	RequestID   string `json:"request_id"`
	StatusURL   string `json:"status_url"`
	ResponseURL string `json:"response_url"`
	CancelURL   string `json:"cancel_url"`
}

func QueueURL(raw string) bool {
	u, e := url.Parse(raw)
	return e == nil && u.Scheme == "https" && u.Host == "queue.fal.run" && u.User == nil && strings.HasPrefix(u.Path, "/topaz/")
}
func PublicURL(raw string) bool {
	u, e := url.Parse(raw)
	return e == nil && u.Scheme == "https" && u.User == nil && u.Host != ""
}

// HTTPError carries only the status, never upstream bodies, URLs or credentials.
type HTTPError struct{ StatusCode int }

func (e *HTTPError) Error() string { return fmt.Sprintf("Topaz 上游返回 HTTP %d", e.StatusCode) }
func (e *HTTPError) Temporary() bool {
	return e.StatusCode == 408 || e.StatusCode == 429 || e.StatusCode >= 500 && e.StatusCode <= 599
}

type NetworkError struct{}

func (*NetworkError) Error() string   { return "Topaz 网络请求未确认" }
func (*NetworkError) Temporary() bool { return true }

// Client never exposes upstream bodies or transport errors, which may contain signed URLs.
type Client struct {
	Key  string
	HTTP *http.Client
}

func (c Client) Do(ctx context.Context, method, endpoint string, payload any, out any, auth bool) error {
	var body io.Reader
	if payload != nil {
		b, e := json.Marshal(payload)
		if e != nil {
			return e
		}
		body = bytes.NewReader(b)
	}
	req, e := http.NewRequestWithContext(ctx, method, endpoint, body)
	if e != nil {
		return errors.New("Topaz 请求地址无效")
	}
	req.Header.Set("Content-Type", "application/json")
	if auth {
		req.Header.Set("Authorization", "Key "+c.Key)
	}
	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: 25 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	resp, e := client.Do(req)
	if e != nil {
		return &NetworkError{}
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &HTTPError{StatusCode: resp.StatusCode}
	}
	if out == nil {
		return nil
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(out) != nil {
		return errors.New("Topaz 响应格式无效")
	}
	return nil
}
func (c Client) Upload(ctx context.Context, body io.Reader, size int64, mime, name string) (string, error) {
	var init struct {
		UploadURL string `json:"upload_url"`
		FileURL   string `json:"file_url"`
	}
	if e := c.Do(ctx, "POST", "https://rest.fal.ai/storage/upload/initiate?storage_type=fal-cdn-v3", map[string]any{"content_type": mime, "file_name": name}, &init, true); e != nil {
		return "", e
	}
	if !PublicURL(init.UploadURL) || !PublicURL(init.FileURL) {
		return "", errors.New("fal 上传地址无效")
	}
	req, e := http.NewRequestWithContext(ctx, "PUT", init.UploadURL, body)
	if e != nil {
		return "", errors.New("fal 上传地址无效")
	}
	req.ContentLength = size
	req.Header.Set("Content-Type", mime)
	client := &http.Client{Timeout: 5 * time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, e := client.Do(req)
	if e != nil {
		return "", errors.New("上传素材失败")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", errors.New("上传素材失败")
	}
	return init.FileURL, nil
}
