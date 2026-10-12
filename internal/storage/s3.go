package storage

import (
	"context"
	"fmt"
	"strings"

	awscfg "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type S3Options struct {
	Bucket         string
	Region         string
	Endpoint       string // empty = AWS default
	Prefix         string
	AccessKeyID    string
	SecretKey      string
	ForcePathStyle bool
}

type S3 struct {
	client *s3.Client
	bucket string
	prefix string
}

func NewS3(ctx context.Context, o S3Options) (*S3, error) {
	if o.Bucket == "" {
		return nil, fmt.Errorf("s3: bucket is required")
	}
	region := o.Region
	if region == "" {
		region = "us-east-1"
	}
	cfg, err := awscfg.LoadDefaultConfig(ctx,
		awscfg.WithRegion(region),
		awscfg.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(o.AccessKeyID, o.SecretKey, ""),
		),
	)
	if err != nil {
		return nil, err
	}
	client := s3.NewFromConfig(cfg, func(p *s3.Options) {
		if o.Endpoint != "" {
			p.BaseEndpoint = &o.Endpoint
		}
		p.UsePathStyle = o.ForcePathStyle
	})
	return &S3{client: client, bucket: o.Bucket, prefix: o.Prefix}, nil
}

func (s *S3) Name() string { return "s3" }

func (s *S3) key(k string) string {
	if s.prefix == "" {
		return k
	}
	return strings.TrimRight(s.prefix, "/") + "/" + strings.TrimLeft(k, "/")
}
