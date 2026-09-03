package webui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/lyrarma/cloud-api/src/models"
	"github.com/valyala/fasthttp"
)

type apiResponse struct {
	status int
	body   []byte
}

func (h *Handler) callAPI(c fiber.Ctx, method, target string, body io.Reader, contentType, token string) (apiResponse, error) {
	internal, err := h.internalRequest(c, method, target, body, contentType, token)
	if err != nil {
		return apiResponse{}, err
	}
	defer internal.Request.CloseBodyStream()

	h.api.Handler()(internal)
	responseBody := append([]byte(nil), internal.Response.Body()...)
	return apiResponse{status: internal.Response.StatusCode(), body: responseBody}, nil
}

func (h *Handler) internalRequest(c fiber.Ctx, method, target string, body io.Reader, contentType, token string) (*fasthttp.RequestCtx, error) {
	parsed, err := url.ParseRequestURI(target)
	if err != nil || parsed.IsAbs() || parsed.Host != "" || !strings.HasPrefix(parsed.Path, "/api/") {
		return nil, fmt.Errorf("invalid internal API route %q", target)
	}

	internal := &fasthttp.RequestCtx{}
	internal.Init(&fasthttp.Request{}, nil, nil)
	request := &internal.Request
	c.Request().Header.CopyTo(&request.Header)
	request.Header.SetMethod(method)
	request.SetRequestURI(parsed.RequestURI())
	request.Header.Del(fiber.HeaderCookie)
	request.Header.Del(fiber.HeaderAuthorization)
	request.Header.Del(fiber.HeaderContentLength)
	if contentType != "" {
		request.Header.SetContentType(contentType)
	}
	if token != "" {
		request.Header.Set(fiber.HeaderAuthorization, "Bearer "+token)
	}
	if body != nil {
		request.SetBodyStream(body, -1)
	}
	return internal, nil
}
func (h *Handler) callJSON(c fiber.Ctx, method, target string, payload any, token string) (apiResponse, error) {
	body := bytes.NewBuffer(nil)
	if payload != nil {
		if err := json.NewEncoder(body).Encode(payload); err != nil {
			return apiResponse{}, err
		}
	}
	return h.callAPI(c, method, target, body, "application/json", token)
}

func (h *Handler) proxyAPI(c fiber.Ctx, method, target, token string) error {
	internal, err := h.internalRequest(c, method, target, nil, "", token)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString("invalid internal route")
	}
	defer internal.Request.CloseBodyStream()

	h.api.Handler()(internal)
	if internal.Response.IsBodyStream() {
		stream := internal.Response.BodyStream()
		size := internal.Response.Header.ContentLength()
		internal.Response.Header.CopyTo(&c.Response().Header)
		c.Response().SetStatusCode(internal.Response.StatusCode())
		c.Response().SetBodyStream(stream, size)
		return nil
	}
	internal.Response.CopyTo(c.Response())
	return nil
}

func (h *Handler) apiMessage(c fiber.Ctx, response apiResponse, fallbackKey string) string {
	var apiError models.ErrorResponse
	if err := json.Unmarshal(response.body, &apiError); err == nil && apiError.Error != "" {
		key := "api_error_" + apiError.Error
		if translated := h.translate(h.requestLanguage(c), key); translated != key {
			return translated
		}
	}
	return h.message(c, fallbackKey)
}
