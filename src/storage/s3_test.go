package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

func TestPromoteQuarantineFile(t *testing.T) {
	const manifestMessage = "Copying manifest objects is not supported."
	tests := []struct {
		name         string
		copyStatus   int
		copyCode     string
		copyMessage  string
		downloadFail bool
		uploadFail   bool
		readFail     bool
		canceled     bool
		wantError    bool
		wantClosed   bool
		wantCalls    []string
	}{
		{name: "server-side copy", copyStatus: 200, wantCalls: []string{"copy"}},
		{name: "manifest fallback", copyStatus: 400, copyCode: "InvalidRequest", copyMessage: manifestMessage,
			wantClosed: true, wantCalls: []string{"copy", "download", "upload"}},
		{name: "copy access denied", copyStatus: 403, copyCode: "AccessDenied", copyMessage: "Access denied",
			wantError: true, wantCalls: []string{"copy"}},
		{name: "unrelated invalid request", copyStatus: 400, copyCode: "InvalidRequest", copyMessage: "Invalid metadata",
			wantError: true, wantCalls: []string{"copy"}},
		{name: "download failure", copyStatus: 400, copyCode: "InvalidRequest", copyMessage: manifestMessage,
			downloadFail: true, wantError: true, wantCalls: []string{"copy", "download"}},
		{name: "upload failure", copyStatus: 400, copyCode: "InvalidRequest", copyMessage: manifestMessage,
			uploadFail: true, wantError: true, wantClosed: true, wantCalls: []string{"copy", "download", "upload"}},
		{name: "interrupted source", copyStatus: 400, copyCode: "InvalidRequest", copyMessage: manifestMessage,
			readFail: true, wantError: true, wantClosed: true, wantCalls: []string{"copy", "download"}},
		{name: "canceled context", canceled: true, wantError: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			const quarantineKey = "staged/video.mp4"
			const fileKey = "approved/video.mp4"
			const contentType = "video/mp4"
			const payload = "verified video content"
			var source io.Reader = strings.NewReader(payload)
			readError := errors.New("interrupted source")
			if test.readFail {
				source = promotionErrorReader{err: readError}
			}
			body := &promotionReadCloser{Reader: source}
			var calls []string
			var callsMu sync.Mutex
			record := func(operation string) {
				callsMu.Lock()
				defer callsMu.Unlock()
				calls = append(calls, operation)
			}
			client := s3.NewFromConfig(aws.Config{
				Region:      "test-region",
				Credentials: credentials.NewStaticCredentialsProvider("test-key", "test-secret", ""),
				HTTPClient: &http.Client{Transport: promotionTransport(func(request *http.Request) (*http.Response, error) {
					response := func(status int, data string) *http.Response {
						return &http.Response{
							StatusCode: status, Header: make(http.Header), Request: request,
							Body: io.NopCloser(strings.NewReader(data)), ContentLength: int64(len(data)),
						}
					}
					apiError := func(status int, code, message string) *http.Response {
						return response(status, fmt.Sprintf("<Error><Code>%s</Code><Message>%s</Message></Error>", code, message))
					}
					switch {
					case request.Method == http.MethodPut && request.Header.Get("X-Amz-Copy-Source") != "":
						record("copy")
						copySource, err := url.PathUnescape(request.Header.Get("X-Amz-Copy-Source"))
						if err != nil || copySource != "quarantine/"+quarantineKey || request.URL.Path != "/primary/"+fileKey {
							t.Errorf("incorrect copy paths: %q -> %q", copySource, request.URL.Path)
						}
						if test.copyStatus != http.StatusOK {
							return apiError(test.copyStatus, test.copyCode, test.copyMessage), nil
						}
						return response(200, "<CopyObjectResult><ETag>copied</ETag></CopyObjectResult>"), nil
					case request.Method == http.MethodGet && request.URL.Path == "/quarantine/"+quarantineKey:
						record("download")
						if test.downloadFail {
							return apiError(404, "NoSuchKey", "Source missing"), nil
						}
						result := response(200, payload)
						result.Body = body
						result.Header.Set("Content-Type", "application/octet-stream")
						return result, nil
					case request.Method == http.MethodPut && request.URL.Path == "/primary/"+fileKey:
						record("upload")
						var uploadedBody io.Reader = request.Body
						if strings.Contains(request.Header.Get("Content-Encoding"), "aws-chunked") {
							uploadedBody = httputil.NewChunkedReader(request.Body)
						}
						data, err := io.ReadAll(uploadedBody)
						if err != nil || string(data) != payload {
							t.Errorf("incorrect uploaded content: %q, error: %v", data, err)
						}
						if request.Header.Get("Content-Type") != contentType {
							t.Errorf("detected content type was not preserved: %q", request.Header.Get("Content-Type"))
						}
						if test.uploadFail {
							return apiError(403, "AccessDenied", "Upload denied"), nil
						}
						return response(200, ""), nil
					default:
						t.Errorf("unexpected S3 request: %s %s", request.Method, request.URL)
						return apiError(400, "InvalidRequest", "Unexpected request"), nil
					}
				})},
			}, func(options *s3.Options) {
				options.BaseEndpoint = aws.String("https://s3.test")
				options.UsePathStyle = true
				options.RetryMaxAttempts = 1
				options.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
				options.ResponseChecksumValidation = aws.ResponseChecksumValidationWhenRequired
			})
			store := &S3Store{
				bucket: "primary", quarantineBucket: "quarantine", client: client,
				transferManagerClient: transfermanager.New(client),
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if test.canceled {
				cancel()
			}
			err := store.PromoteQuarantineFile(ctx, quarantineKey, fileKey, contentType)
			if (err != nil) != test.wantError {
				t.Fatalf("promotion error = %v, want error = %t", err, test.wantError)
			}
			if test.canceled && !errors.Is(err, context.Canceled) {
				t.Errorf("cancellation was not preserved: %v", err)
			}
			if test.readFail && !errors.Is(err, readError) {
				t.Errorf("read error was not preserved: %v", err)
			}
			if body.closed.Load() != test.wantClosed {
				t.Errorf("source stream closed = %t, want %t", body.closed.Load(), test.wantClosed)
			}
			if !reflect.DeepEqual(calls, test.wantCalls) {
				t.Errorf("S3 operations = %v, want %v", calls, test.wantCalls)
			}
		})
	}
}

type promotionTransport func(*http.Request) (*http.Response, error)

func (transport promotionTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

type promotionReadCloser struct {
	io.Reader
	closed atomic.Bool
}

func (body *promotionReadCloser) Close() error {
	body.closed.Store(true)
	return nil
}

type promotionErrorReader struct {
	err error
}

func (reader promotionErrorReader) Read([]byte) (int, error) {
	return 0, reader.err
}
