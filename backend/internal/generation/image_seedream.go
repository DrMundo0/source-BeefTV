package generation

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"infinite-canvas/backend/internal/providerpreset"
)

func seedreamFamily(model string) string {
	return providerpreset.SeedreamFamily(model)
}

func seedreamImageBody(input Input) (map[string]any, error) {
	family := seedreamFamily(input.Config.Model)
	modern := family == "pro" || family == "flash"
	if input.Mask != nil {
		return nil, errors.New("Seedream 请用标注参考图或在提示词中指定坐标编辑，暂不支持蒙版文件")
	}
	if len(input.ReferenceVideos)+len(input.ReferenceAudios) > 0 {
		return nil, errors.New("Seedream 不支持参考视频或音频")
	}
	refs := make([]string, 0, len(input.ReferenceImages))
	for _, ref := range input.ReferenceImages {
		if ref.DataURL != "" {
			refs = append(refs, ref.DataURL)
		} else if ref.URL != "" {
			refs = append(refs, ref.URL)
		} else {
			return nil, errors.New("参考图片数据为空")
		}
	}
	maxRefs := 14
	if modern {
		maxRefs = 10
	}
	if len(refs) > maxRefs {
		return nil, fmt.Errorf("当前 Seedream 模型最多支持 %d 张参考图", maxRefs)
	}
	layers := input.Config.SeedreamMode == "layers"
	if layers && (!modern || len(refs) != 1) {
		return nil, errors.New("图层拆分需要 Pro 或 Flash，并连接一张图片")
	}
	size := input.Config.Size
	if size == "" || size == "1:1" {
		size = "2K"
	}
	size = strings.ToUpper(size)
	if size == "AUTO" {
		size = "auto"
	}
	body := map[string]any{"model": input.Config.Model, "prompt": WithSystemPrompt(input.Config, input.Prompt), "size": size, "response_format": "b64_json", "watermark": input.Config.SeedreamWatermark == "true"}
	if len(refs) == 1 {
		body["image"] = refs[0]
	} else if len(refs) > 1 {
		body["image"] = refs
	}
	if layers {
		body["layer_decomposition"] = true
	}
	if !modern {
		n, _ := strconv.Atoi(input.Config.SeedreamMaxImages)
		if n <= 0 {
			n = 1
		}
		if n > 15-len(refs) {
			return nil, errors.New("参考图片与组图数量合计不能超过 15 张")
		}
		if n > 1 {
			body["sequential_image_generation"] = "auto"
			body["sequential_image_generation_options"] = map[string]any{"max_images": n}
		}
	}
	if modern || family == "lite" {
		format := input.Config.SeedreamOutputFormat
		if format == "" {
			format = "png"
		}
		if format != "png" && format != "jpeg" {
			return nil, errors.New("图片格式请选择 PNG 或 JPEG")
		}
		body["output_format"] = format
	}
	if input.Config.SeedreamOptimize != "" {
		body["optimize_prompt_options"] = map[string]any{"mode": input.Config.SeedreamOptimize}
	}
	if input.Config.TransparentBackground == "true" {
		if !modern || layers || len(refs) != 1 {
			return nil, errors.New("透明背景需要 Pro 或 Flash，并连接一张透明图片")
		}
		body["background"] = "transparent"
		body["output_format"] = "png"
	}
	return body, nil
}

func runSeedreamImageTask(ctx context.Context, input Input) (map[string]interface{}, error) {
	body, err := seedreamImageBody(input)
	if err != nil {
		return nil, err
	}
	// The managed connection uses BeefAPI's public Images endpoint. Direct Ark
	// services share the same JSON body and their configured /api/v3 base.
	if !providerpreset.IsBeefAPIEndpoint(input.Config.BaseURL) && !strings.Contains(input.Config.BaseURL, "/api/") {
		input.Config.BaseURL = strings.TrimRight(input.Config.BaseURL, "/") + "/api/v3"
	}
	var payload ImageResponse
	if err = PostJSON(ctx, input.Config, "/images/generations", body, &payload); err != nil {
		return nil, err
	}
	for _, item := range payload.Data {
		if encoded, ok := item["b64_json"].(string); ok && encoded != "" {
			data, e := base64.StdEncoding.DecodeString(encoded)
			if e != nil {
				return nil, errors.New("图片结果的编码无效")
			}
			mime := http.DetectContentType(data)
			if mime != "image/png" && mime != "image/jpeg" {
				return nil, errors.New("图片结果不是 PNG 或 JPEG")
			}
			item["url"] = DataURL(mime, data)
			delete(item, "b64_json")
		}
	}
	images, err := MaterializeImageDataURLs(ctx, input.Config, payload)
	if err != nil {
		return nil, err
	}
	// Resource materialization keeps normal image assets. Preserve layer placement
	// separately in the result so an exported or reopened task can recompose it.
	layers := make([]map[string]any, 0, len(payload.Data))
	for i, item := range payload.Data {
		layer := map[string]any{"imageIndex": i}
		for _, key := range []string{"z_index", "bounding_box", "name", "description", "size", "output_format"} {
			if value, ok := item[key]; ok {
				layer[key] = value
			}
		}
		layers = append(layers, layer)
	}
	result := map[string]interface{}{"mode": "image", "images": images}
	if input.Config.SeedreamMode == "layers" {
		result["imageLayers"] = layers
	}
	return result, nil
}
