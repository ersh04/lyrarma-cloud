package config

import (
	"bytes"
	"errors"
	"io"
	"mime/multipart"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/valyala/fasthttp"
)

func TestUnlimitedMultipartRequest(t *testing.T) {
	const fileSize = 5 * 1024 * 1024
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "large.bin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(part, bytes.NewReader(make([]byte, fileSize))); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []struct {
		limit  int64
		stream bool
	}{{-1, false}, {mebibyte, false}, {-1, true}, {mebibyte, true}} {
		limit := mode.limit
		set, err := NewUserLimitSet(-1, limit, "user", map[string]UserLimits{"user": {maxStorageBytes: -1, maxFileBytes: -1}})
		if err != nil {
			t.Fatal(err)
		}
		app := fiber.New(fiber.Config{BodyLimit: set.RequestBodyLimit(), StreamRequestBody: mode.stream, DisablePreParseMultipartForm: true})
		app.Post("/upload", func(c fiber.Ctx) error {
			header, err := c.FormFile("file")
			if err != nil {
				if errors.Is(err, fasthttp.ErrBodyTooLarge) {
					return c.SendStatus(fiber.StatusRequestEntityTooLarge)
				}
				return err
			}
			if header.Size != fileSize {
				t.Errorf("unexpected uploaded size: %d", header.Size)
			}
			return c.SendStatus(fiber.StatusOK)
		})
		request := httptest.NewRequest("POST", "/upload", bytes.NewReader(body.Bytes()))
		request.Header.Set("Content-Type", writer.FormDataContentType())
		request.Header.Set("Connection", "close")
		response, err := app.Test(request)
		if err != nil {
			if limit != -1 && errors.Is(err, fasthttp.ErrBodyTooLarge) {
				continue
			}
			t.Fatalf("limit=%d stream=%t: %v", limit, mode.stream, err)
		}
		response.Body.Close()
		want := fiber.StatusOK
		if limit != -1 {
			want = fiber.StatusRequestEntityTooLarge
		}
		if response.StatusCode != want {
			t.Errorf("limit %d: status=%d, want %d", limit, response.StatusCode, want)
		}
	}
}
