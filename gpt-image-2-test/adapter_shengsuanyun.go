package service

import (
	"context"
	"fmt"
	"strings"

	config "github.com/GMISWE/control/configs/request_queue"
	"github.com/GMISWE/control/pkg/logger"
	rqapi "github.com/GMISWE/control_api/api/requestqueue"
)

// ShengsuanyunAdapter handles the Shengsuanyun OpenAI-compatible image proxy.
// Registered under adapter key "shengsuanyun".
//
// Protocol: OpenAI images wire format (JSON body, Bearer auth).
//   - generate → POST /v1/images/generations
//   - edit     → POST /v1/images/edits  (accepts image URLs directly)
//
// Request/response logic is in provider_gpt_image.go; this adapter only
// resolves credentials and wraps the payload in a vendor HTTP request.
type ShengsuanyunAdapter struct {
	svc *RequestQueueService
	cfg *config.RequestQueueConfig
}

func NewShengsuanyunAdapter(svc *RequestQueueService, cfg *config.RequestQueueConfig) *ShengsuanyunAdapter {
	return &ShengsuanyunAdapter{svc: svc, cfg: cfg}
}

func (a *ShengsuanyunAdapter) Auth(route *rqapi.VendorRoute) (string, error) {
	return resolveRouteAPIKeyCredential(route, "shengsuanyun")
}

func (a *ShengsuanyunAdapter) BuildRequest(req *rqapi.AsyncRequest, model *rqapi.Model, route *rqapi.VendorRoute) (*VendorHTTPRequest, error) {
	token, err := a.Auth(route)
	if err != nil {
		return nil, err
	}

	baseURL := routeAPIURL(route, "https://router.shengsuanyun.com")

	var body map[string]any
	var endpoint string

	if strings.Contains(string(model.ModelID), "edit") {
		body, err = a.svc.buildGPTImageEditRequest(req)
		if err != nil {
			return nil, fmt.Errorf("shengsuanyun: %w", err)
		}
		endpoint = routeConfigString(route, "edit_api_endpoint", "/v1/images/edits")
	} else {
		body, err = a.svc.buildGPTImageGenerateRequest(req)
		if err != nil {
			return nil, fmt.Errorf("shengsuanyun: %w", err)
		}
		endpoint = routeConfigString(route, "api_endpoint", "/v1/images/generations")
	}

	bodyBytes, err := marshalJSON(body)
	if err != nil {
		return nil, fmt.Errorf("shengsuanyun: marshal failed: %v", err)
	}

	logger.L(context.Background()).Infof("[Shengsuanyun] request_id=%s url=%s%s", req.RequestID, baseURL, endpoint)

	return &VendorHTTPRequest{
		URL:  baseURL + endpoint,
		Body: bodyBytes,
		Headers: map[string]string{
			"Content-Type":  "application/json",
			"Authorization": "Bearer " + token,
		},
	}, nil
}

func (a *ShengsuanyunAdapter) HandleResponse(ctx context.Context, req *rqapi.AsyncRequest, _ *rqapi.VendorRoute, response []byte) error {
	return a.svc.HandleGPTImageResponse(ctx, req, response)
}

func (a *ShengsuanyunAdapter) IsRetryable(statusCode int, body []byte, err error) RetryDecision {
	return DefaultIsRetryable(statusCode, body, err)
}
