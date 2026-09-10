package storage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type s3API interface {
	PutObject(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	GetObject(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	DeleteObject(ctx context.Context, params *s3.DeleteObjectInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
}

type s3Store struct {
	client s3API
	bucket string
}

func newS3(cfg S3Settings) (Store, error) {
	bucket := strings.TrimSpace(cfg.GetS3Bucket())
	if bucket == "" {
		return nil, fmt.Errorf("storage: S3_BUCKET is required for the s3 driver")
	}
	accessKey := strings.TrimSpace(cfg.GetS3AccessKeyID())
	secretKey := strings.TrimSpace(cfg.GetS3SecretAccessKey())
	if accessKey == "" || secretKey == "" {
		return nil, fmt.Errorf("storage: S3_ACCESS_KEY_ID and S3_SECRET_ACCESS_KEY are required for the s3 driver")
	}

	endpoint := strings.TrimSpace(cfg.GetS3Endpoint())
	region := strings.TrimSpace(cfg.GetS3Region())
	if region == "" {
		region = "auto"
	}

	credProvider := credentials.NewStaticCredentialsProvider(accessKey, secretKey, "")
	awsCfg, err := awsconfig.LoadDefaultConfig(context.Background(),
		awsconfig.WithRegion(region),
		awsconfig.WithCredentialsProvider(credProvider),
	)
	if err != nil {
		return nil, fmt.Errorf("storage: load aws config: %w", err)
	}

	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if endpoint != "" {
			o.BaseEndpoint = aws.String(endpoint)
		}
		o.UsePathStyle = true
	})

	return &s3Store{
		client: client,
		bucket: bucket,
	}, nil
}

func cleanS3Key(key string) (string, error) {
	clean := filepath.ToSlash(filepath.Clean(strings.TrimPrefix(key, "/")))
	if clean == "" || clean == "." || strings.HasPrefix(clean, "..") {
		return "", fmt.Errorf("storage: invalid key %q", key)
	}
	return clean, nil
}

func (s *s3Store) Save(ctx context.Context, key string, r io.Reader, contentType string) (string, error) {
	k, err := cleanS3Key(key)
	if err != nil {
		return "", err
	}

	var body io.Reader
	var contentLength *int64

	if rs, ok := r.(io.ReadSeeker); ok {
		if size, err := rs.Seek(0, io.SeekEnd); err == nil {
			contentLength = aws.Int64(size)
			_, _ = rs.Seek(0, io.SeekStart)
		}
		body = rs
	} else {
		data, err := io.ReadAll(r)
		if err != nil {
			return "", fmt.Errorf("storage: read data: %w", err)
		}
		contentLength = aws.Int64(int64(len(data)))
		body = bytes.NewReader(data)
	}

	if contentType == "" {
		contentType = "application/octet-stream"
	}

	input := &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(k),
		Body:        body,
		ContentType: aws.String(contentType),
	}
	if contentLength != nil {
		input.ContentLength = contentLength
	}

	if _, err := s.client.PutObject(ctx, input); err != nil {
		return "", fmt.Errorf("storage: s3 put %s: %w", k, err)
	}
	return k, nil
}

func (s *s3Store) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	k, err := cleanS3Key(key)
	if err != nil {
		return nil, err
	}

	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(k),
	})
	if err != nil {
		return nil, fmt.Errorf("storage: s3 get %s: %w", k, err)
	}
	return out.Body, nil
}

func (s *s3Store) Delete(ctx context.Context, key string) error {
	k, err := cleanS3Key(key)
	if err != nil {
		return err
	}

	_, err = s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(k),
	})
	if err != nil {
		return fmt.Errorf("storage: s3 delete %s: %w", k, err)
	}
	return nil
}
