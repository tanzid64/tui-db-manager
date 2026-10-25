package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscfg "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

var _ Backend = (*S3)(nil)

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
	endpoint := NormalizeEndpoint(o.Endpoint)
	region := o.Region
	if region == "" {
		region = "us-east-1"
		if strings.Contains(endpoint, "r2.cloudflarestorage.com") {
			region = "auto"
		}
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
		p.UsePathStyle = o.ForcePathStyle
		if endpoint != "" {
			// S3-compatible providers (R2, MinIO, B2, Wasabi, Spaces) all accept
			// path-style URLs, and several reject the newer default checksum
			// headers, so only send checksums when the API requires them.
			p.BaseEndpoint = aws.String(endpoint)
			p.UsePathStyle = true
			p.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
			p.ResponseChecksumValidation = aws.ResponseChecksumValidationWhenRequired
		}
	})
	return &S3{client: client, bucket: o.Bucket, prefix: o.Prefix}, nil
}

func (s *S3) Name() string { return "s3" }

// NormalizeEndpoint trims an S3 endpoint and adds "https://" when no scheme
// is given (e.g. "<account>.r2.cloudflarestorage.com").
func NormalizeEndpoint(e string) string {
	e = strings.TrimRight(strings.TrimSpace(e), "/")
	if e == "" {
		return ""
	}
	if !strings.Contains(e, "://") {
		e = "https://" + e
	}
	return e
}

func (s *S3) key(k string) string {
	if s.prefix == "" {
		return k
	}
	return strings.TrimRight(s.prefix, "/") + "/" + strings.TrimLeft(k, "/")
}

func (s *S3) Put(ctx context.Context, key string, r io.Reader, size int64) error {
	uploader := manager.NewUploader(s.client)
	_, err := uploader.Upload(ctx, &s3.PutObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(s.key(key)),
		Body:   r,
	})
	return err
}

func (s *S3) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket), Key: aws.String(s.key(key)),
	})
	if err != nil {
		return nil, err
	}
	return out.Body, nil
}

func (s *S3) List(ctx context.Context, prefix string) ([]Object, error) {
	full := s.key(prefix)
	p := s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(s.bucket), Prefix: aws.String(full),
	})
	var out []Object
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, o := range page.Contents {
			k := aws.ToString(o.Key)
			if s.prefix != "" {
				k = strings.TrimPrefix(k, strings.TrimRight(s.prefix, "/")+"/")
			}
			var mt time.Time
			if o.LastModified != nil {
				mt = *o.LastModified
			}
			out = append(out, Object{Key: k, Size: aws.ToInt64(o.Size), ModTime: mt})
		}
	}
	return out, nil
}

func (s *S3) Delete(ctx context.Context, key string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket), Key: aws.String(s.key(key)),
	})
	return err
}

func (s *S3) Stat(ctx context.Context, key string) (Object, error) {
	out, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket), Key: aws.String(s.key(key)),
	})
	if err != nil {
		return Object{}, err
	}
	var mt time.Time
	if out.LastModified != nil {
		mt = *out.LastModified
	}
	return Object{Key: key, Size: aws.ToInt64(out.ContentLength), ModTime: mt}, nil
}

func (s *S3) Test(ctx context.Context) error {
	_, err := s.client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(s.bucket)})
	var nf *s3types.NotFound
	if errors.As(err, &nf) {
		return fmt.Errorf("s3: bucket %q not found or not accessible", s.bucket)
	}
	return err
}
