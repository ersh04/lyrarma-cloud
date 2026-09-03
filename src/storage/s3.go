package storage

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// S3StoreConfig contains the connection parameters for S3.
type S3StoreConfig struct {
	Bucket          string
	Region          string
	Endpoint        string
	AccessKeyID     string
	SecretAccessKey string
	UsePathStyle    bool
}

// S3Store stores file content in an S3-compatible storage.
type S3Store struct {
	bucket                string
	client                *s3.Client
	transferManagerClient *transfermanager.Client
	presigner             *s3.PresignClient
}

// NewS3Store creates a client for an S3-compatible storage.
func NewS3Store(ctx context.Context, settings S3StoreConfig) (*S3Store, error) {
	settings.Bucket = strings.TrimSpace(settings.Bucket)
	settings.Region = strings.TrimSpace(settings.Region)
	settings.Endpoint = strings.TrimSpace(settings.Endpoint)
	settings.AccessKeyID = strings.TrimSpace(settings.AccessKeyID)
	settings.SecretAccessKey = strings.TrimSpace(settings.SecretAccessKey)

	if settings.Bucket == "" {
		return nil, fmt.Errorf("S3 bucket name is not set")
	}
	if settings.Region == "" {
		return nil, fmt.Errorf("S3 region is not set")
	}
	if (settings.AccessKeyID == "") != (settings.SecretAccessKey == "") {
		return nil, fmt.Errorf("S3 access key and secret must be set together")
	}

	options := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(settings.Region),
	}
	if settings.AccessKeyID != "" {
		options = append(options, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(
				settings.AccessKeyID,
				settings.SecretAccessKey,
				"",
			),
		))
	}

	cfg, err := awsconfig.LoadDefaultConfig(ctx, options...)
	if err != nil {
		return nil, fmt.Errorf("load S3 configuration: %w", err)
	}

	client := s3.NewFromConfig(cfg, func(options *s3.Options) {
		if settings.Endpoint != "" {
			options.BaseEndpoint = aws.String(settings.Endpoint)
		}
		options.UsePathStyle = settings.UsePathStyle
	})

	return &S3Store{
		bucket: settings.Bucket,
		client: client,
		transferManagerClient: transfermanager.New(client, func(o *transfermanager.Options) {
			o.PartSizeBytes = 32 * 1024 * 1024
			o.Concurrency = 5
		}),
		presigner: s3.NewPresignClient(client),
	}, nil
}

// UploadFile uploads file content to S3.
func (storage *S3Store) UploadFile(
	ctx context.Context,
	key string,
	body io.Reader,
	contentType string,
) error {
	_, err := storage.transferManagerClient.UploadObject(ctx, &transfermanager.UploadObjectInput{
		Bucket:      aws.String(storage.bucket),
		Key:         aws.String(key),
		Body:        body,
		ContentType: aws.String(contentType),
	})
	if err != nil {
		return fmt.Errorf("upload object %q to S3: %w", key, err)
	}
	return nil
}

// DownloadFile opens an S3 object for streaming reads.
func (storage *S3Store) DownloadFile(
	ctx context.Context,
	key string,
) (io.ReadCloser, string, error) {
	result, err := storage.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(storage.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, "", fmt.Errorf("get object %q from S3: %w", key, err)
	}

	contentType := ""
	if result.ContentType != nil {
		contentType = *result.ContentType
	}
	return result.Body, contentType, nil
}

// DeleteFile removes an object from S3.
func (storage *S3Store) DeleteFile(ctx context.Context, key string) error {
	_, err := storage.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(storage.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("delete object %q from S3: %w", key, err)
	}
	return nil
}

// CreateUploadURL creates a temporary URL for uploading an object.
func (storage *S3Store) CreateUploadURL(
	ctx context.Context,
	key string,
	contentType string,
	lifetime time.Duration,
) (string, error) {
	request, err := storage.presigner.PresignPutObject(
		ctx,
		&s3.PutObjectInput{
			Bucket:      aws.String(storage.bucket),
			Key:         aws.String(key),
			ContentType: aws.String(contentType),
		},
		func(options *s3.PresignOptions) {
			options.Expires = lifetime
		},
	)
	if err != nil {
		return "", fmt.Errorf("create upload URL for object %q: %w", key, err)
	}
	return request.URL, nil
}

// CreateDownloadURL creates a temporary URL for downloading an object.
func (storage *S3Store) CreateDownloadURL(
	ctx context.Context,
	key string,
	lifetime time.Duration,
) (string, error) {
	request, err := storage.presigner.PresignGetObject(
		ctx,
		&s3.GetObjectInput{
			Bucket: aws.String(storage.bucket),
			Key:    aws.String(key),
		},
		func(options *s3.PresignOptions) {
			options.Expires = lifetime
		},
	)
	if err != nil {
		return "", fmt.Errorf("create download URL for object %q: %w", key, err)
	}
	return request.URL, nil
}

// GetFileMetadata returns the content type and size of an object in S3.
func (storage *S3Store) GetFileMetadata(
	ctx context.Context,
	key string,
) (contentType string, contentLength int64, err error) {
	result, err := storage.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(storage.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return "", 0, fmt.Errorf("get metadata for object %q: %w", key, err)
	}

	if result.ContentType != nil {
		contentType = *result.ContentType
	}
	if result.ContentLength != nil {
		contentLength = *result.ContentLength
	}
	return contentType, contentLength, nil
}
