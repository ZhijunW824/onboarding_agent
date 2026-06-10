package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/GMISWE/control/pkg/logger"
	rqapi "github.com/GMISWE/control_api/api/requestqueue"
)

// ── Estimator ─────────────────────────────────────────────────────────────────

// gptImageEstimator estimates cost for gpt-image-2 generate and edit requests.
//
// gpt-image-2 uses token-based pricing (OpenAI published rates, verified 2026-06):
//   - Input text/image tokens: $8.00 / 1M tokens
//   - Output image tokens:    $30.00 / 1M tokens
//
// Per-image costs derived from OpenAI's image generation pricing calculator
// (output token count × $30/1M tokens), in micro-USD (1 USD = 1,000,000 micro-USD):
//
//	quality  size              cost      micro-USD
//	low      1024×1024         $0.006    6,000
//	low      1024×1536/1536×1024 $0.005  5,000
//	medium   1024×1024         $0.053    53,000
//	medium   1024×1536/1536×1024 $0.041  41,000
//	high     1024×1024         $0.211    211,000
//	high     1024×1536/1536×1024 $0.165  165,000
//
// "auto" quality defaults to medium. Input text tokens are small (~100–300 tokens
// per request) and omitted here; they can be added when precise billing is required.
type gptImageEstimator struct{}

func (e gptImageEstimator) EstimatePrice(_ context.Context, _ *rqapi.Model, payload map[string]any) (int, error) {
	size, _ := payload["size"].(string)
	quality := strings.ToLower(strings.TrimSpace(func() string {
		q, _ := payload["quality"].(string)
		return q
	}()))
	n := 1
	if nVal, ok := payload["n"]; ok {
		switch v := nVal.(type) {
		case int:
			if v > 0 {
				n = v
			}
		case float64:
			if v > 0 {
				n = int(v)
			}
		}
	}

	isNonSquare := strings.Contains(size, "1536")

	var pricePerImage int
	switch quality {
	case "low":
		if isNonSquare {
			pricePerImage = 5_000
		} else {
			pricePerImage = 6_000
		}
	case "high":
		if isNonSquare {
			pricePerImage = 165_000
		} else {
			pricePerImage = 211_000
		}
	default: // "medium", "auto", ""
		if isNonSquare {
			pricePerImage = 41_000
		} else {
			pricePerImage = 53_000
		}
	}

	return pricePerImage * n, nil
}

// ── Request builders ──────────────────────────────────────────────────────────

// buildGPTImageGenerateRequest builds the OpenAI /v1/images/generations payload
// from the GMI unified request payload. The "model" field is injected from
// model.internal_parameters by the framework before this is called.
func (s *RequestQueueService) buildGPTImageGenerateRequest(req *rqapi.AsyncRequest) (map[string]any, error) {
	payload := req.SystemPayload
	if payload == nil {
		payload = req.Payload
	}

	prompt, _ := payload["prompt"].(string)
	if strings.TrimSpace(prompt) == "" {
		return nil, fmt.Errorf("gpt-image generate: prompt is required")
	}

	body := map[string]any{
		"model":           gptImageModelName(payload),
		"prompt":          strings.TrimSpace(prompt),
		"response_format": "url",
	}
	gptImageCopyOptionalFields(body, payload,
		"n", "size", "quality", "background", "output_format",
		"output_compression", "moderation", "user")

	return body, nil
}

// buildGPTImageEditRequest builds the OpenAI /v1/images/edits payload.
// The image field may be a URL string or []string; it is passed through as-is
// because some vendors (shengsuanyun) accept URL inputs directly.
func (s *RequestQueueService) buildGPTImageEditRequest(req *rqapi.AsyncRequest) (map[string]any, error) {
	payload := req.SystemPayload
	if payload == nil {
		payload = req.Payload
	}

	prompt, _ := payload["prompt"].(string)
	if strings.TrimSpace(prompt) == "" {
		return nil, fmt.Errorf("gpt-image edit: prompt is required")
	}
	if !isNonEmptyGPTImageField(payload["image"]) {
		return nil, fmt.Errorf("gpt-image edit: image is required")
	}

	body := map[string]any{
		"model":           gptImageModelName(payload),
		"prompt":          strings.TrimSpace(prompt),
		"image":           normalizeImageField(payload["image"]),
		"response_format": "url",
	}
	if mask, ok := payload["mask"].(string); ok && strings.TrimSpace(mask) != "" {
		body["mask"] = strings.TrimSpace(mask)
	}
	gptImageCopyOptionalFields(body, payload,
		"n", "size", "quality", "background", "output_format",
		"output_compression", "moderation", "input_fidelity", "user")

	return body, nil
}

// ── Response handler ──────────────────────────────────────────────────────────

// gptImageVendorResponse is the OpenAI images response envelope returned by all
// gpt-image-2 vendors (shengsuanyun, apihub_oai, etc.).
type gptImageVendorResponse struct {
	Created int64 `json:"created"`
	Data    []struct {
		URL           string `json:"url"`
		B64JSON       string `json:"b64_json"`
		RevisedPrompt string `json:"revised_prompt"`
	} `json:"data"`
	// Usage contains the actual token counts reported by the vendor.
	// Pricing: input $8/1M tokens, output $30/1M tokens (micro-USD per token: 8 and 30).
	Usage *struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
		TotalTokens  int `json:"total_tokens"`
		InputTokensDetails *struct {
			TextTokens  int `json:"text_tokens"`
			ImageTokens int `json:"image_tokens"`
		} `json:"input_tokens_details"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    any    `json:"code"`
	} `json:"error"`
}

// HandleGPTImageResponse parses an OpenAI-format images response, downloads
// each result image (from URL or base64), uploads to GCS, and sets
// req.Outcome / req.Status. Suitable for all gpt-image-2 vendor adapters that
// speak the OpenAI images protocol.
func (s *RequestQueueService) HandleGPTImageResponse(ctx context.Context, req *rqapi.AsyncRequest, response []byte) error {
	var resp gptImageVendorResponse
	if err := json.Unmarshal(response, &resp); err != nil {
		return fmt.Errorf("gpt-image: failed to parse response: %v (body=%s)", err, truncateStr(string(response), 500))
	}

	if resp.Error != nil && resp.Error.Message != "" {
		return fmt.Errorf("gpt-image: vendor error: %s", resp.Error.Message)
	}

	if len(resp.Data) == 0 {
		return fmt.Errorf("gpt-image: response contains no image data (body=%s)", truncateStr(string(response), 500))
	}

	mediaURLs := make([]map[string]any, 0, len(resp.Data))
	revisedPrompts := make([]string, 0, len(resp.Data))

	for i, item := range resp.Data {
		gcsURL, err := s.gptImageUploadItem(ctx, item.URL, item.B64JSON, req.RequestID, i)
		if err != nil {
			logger.L(ctx).Errorf("[GPTImage] request_id=%s item=%d upload_error=%v", req.RequestID, i, err)
			return fmt.Errorf("gpt-image: failed to upload image[%d] to GCS: %v", i, err)
		}
		mediaURLs = append(mediaURLs, map[string]any{"id": fmt.Sprintf("%d", i), "url": gcsURL})
		if item.RevisedPrompt != "" {
			revisedPrompts = append(revisedPrompts, item.RevisedPrompt)
		}
	}

	if req.Outcome == nil {
		req.Outcome = make(map[string]any)
	}
	req.Outcome["media_urls"] = mediaURLs
	req.Outcome["thumbnail_image_url"] = mediaURLs[0]["url"]
	if resp.Created != 0 {
		req.Outcome["provider_created_at"] = resp.Created
	}
	if len(revisedPrompts) > 0 {
		req.Outcome["revised_prompts"] = revisedPrompts
	}

	// Store vendor-reported token usage. Actual cost in micro-USD:
	// input_tokens × $8/1M = input_tokens × 8 micro-USD
	// output_tokens × $30/1M = output_tokens × 30 micro-USD
	if resp.Usage != nil {
		actualCostMicroUSD := resp.Usage.InputTokens*8 + resp.Usage.OutputTokens*30
		usageInfo := map[string]any{
			"input_tokens":          resp.Usage.InputTokens,
			"output_tokens":         resp.Usage.OutputTokens,
			"total_tokens":          resp.Usage.TotalTokens,
			"actual_cost_micro_usd": actualCostMicroUSD,
		}
		if resp.Usage.InputTokensDetails != nil {
			usageInfo["input_tokens_details"] = map[string]any{
				"text_tokens":  resp.Usage.InputTokensDetails.TextTokens,
				"image_tokens": resp.Usage.InputTokensDetails.ImageTokens,
			}
		}
		req.Outcome["request_usage"] = usageInfo
		logger.L(ctx).Infof("[GPTImage] request_id=%s token_usage in=%d out=%d actual_cost=%d micro-USD",
			req.RequestID, resp.Usage.InputTokens, resp.Usage.OutputTokens, actualCostMicroUSD)
	}

	req.Status = rqapi.RequestStatusSuccess
	logger.L(ctx).Infof("[GPTImage] request_id=%s succeeded images=%d", req.RequestID, len(mediaURLs))
	return nil
}

// gptImageUploadItem resolves one response item (URL or base64) to a GCS URL.
func (s *RequestQueueService) gptImageUploadItem(_ context.Context, imageURL, b64JSON string, requestID rqapi.RequestID, index int) (string, error) {
	if strings.TrimSpace(imageURL) != "" {
		return s.gptImageDownloadAndUpload(imageURL, requestID)
	}
	if strings.TrimSpace(b64JSON) != "" {
		return s.gptImageDecodeAndUpload(b64JSON, requestID)
	}
	return "", fmt.Errorf("item[%d] has neither url nor b64_json", index)
}

func (s *RequestQueueService) gptImageDownloadAndUpload(imageURL string, requestID rqapi.RequestID) (string, error) {
	resp, err := http.Get(imageURL)
	if err != nil {
		return "", fmt.Errorf("download %s: %v", imageURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download %s: HTTP %d", imageURL, resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read body %s: %v", imageURL, err)
	}
	mimeType := strings.ToLower(strings.TrimSpace(strings.Split(resp.Header.Get("Content-Type"), ";")[0]))
	if !strings.HasPrefix(mimeType, "image/") {
		if detected := http.DetectContentType(data); strings.HasPrefix(detected, "image/") {
			mimeType = detected
		} else {
			mimeType = "image/png"
		}
	}
	return s.uploadImageBytesToGCS(data, mimeType, requestID)
}

func (s *RequestQueueService) gptImageDecodeAndUpload(b64Data string, requestID rqapi.RequestID) (string, error) {
	cleaned := strings.TrimSpace(b64Data)
	if idx := strings.Index(cleaned, ","); idx >= 0 {
		cleaned = cleaned[idx+1:]
	}
	imageBytes, err := base64.StdEncoding.DecodeString(cleaned)
	if err != nil {
		return "", fmt.Errorf("base64 decode: %v", err)
	}
	mimeType := http.DetectContentType(imageBytes)
	if !strings.HasPrefix(mimeType, "image/") {
		mimeType = "image/png"
	}
	return s.uploadImageBytesToGCS(imageBytes, mimeType, requestID)
}

// ── helpers ───────────────────────────────────────────────────────────────────

// gptImageModelName extracts the vendor model name from the payload.
// The framework injects model.internal_parameters (e.g. {"model": "gpt-image-2"})
// into SystemPayload before dispatching to the adapter.
func gptImageModelName(payload map[string]any) string {
	if m, ok := payload["model"].(string); ok && m != "" {
		return m
	}
	return "gpt-image-2"
}

// gptImageCopyOptionalFields copies non-nil/non-empty scalar values.
func gptImageCopyOptionalFields(dst, src map[string]any, keys ...string) {
	for _, k := range keys {
		v, ok := src[k]
		if !ok || v == nil {
			continue
		}
		if s, ok := v.(string); ok && s == "" {
			continue
		}
		dst[k] = v
	}
}
