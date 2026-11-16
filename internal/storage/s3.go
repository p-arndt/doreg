package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/p-arndt/doreg/internal/digest"
)

const defaultS3Region = "us-east-1"

// S3Config describes the bucket that holds blobs.
type S3Config struct {
	// Endpoint is a URL such as http://localhost:9000. Empty selects AWS S3.
	Endpoint        string
	Region          string
	Bucket          string
	AccessKeyID     string
	SecretAccessKey string
	// ForcePathStyle addresses buckets as /bucket/key instead of bucket.host/key.
	ForcePathStyle bool
	// Prefix is prepended to every object key, e.g. "doreg/".
	Prefix string
}

// s3ConfigFromEnv reads the S3 settings from the environment.
func s3ConfigFromEnv() S3Config {
	return S3Config{
		Endpoint:        os.Getenv("S3_ENDPOINT"),
		Region:          os.Getenv("S3_REGION"),
		Bucket:          os.Getenv("S3_BUCKET"),
		AccessKeyID:     os.Getenv("S3_ACCESS_KEY_ID"),
		SecretAccessKey: os.Getenv("S3_SECRET_ACCESS_KEY"),
		ForcePathStyle:  strings.EqualFold(os.Getenv("S3_FORCE_PATH_STYLE"), "true"),
		Prefix:          os.Getenv("S3_PREFIX"),
	}
}

// s3Backend stores blobs as objects under <prefix>blobs/<alg>/<xx>/<hex>.
type s3Backend struct {
	client *minio.Client
	bucket string
	prefix string
}

// newS3FS creates the upload directories below root and an FS whose blobs live
// in the configured bucket. The bucket is created if it does not exist.
func newS3FS(root string, cfg S3Config) (*FS, error) {
	b, err := newS3Backend(cfg)
	if err != nil {
		return nil, err
	}
	return newFS(root, b)
}

func newS3Backend(cfg S3Config) (*s3Backend, error) {
	if cfg.Bucket == "" {
		return nil, errors.New("S3_BUCKET is required")
	}
	if cfg.AccessKeyID == "" || cfg.SecretAccessKey == "" {
		return nil, errors.New("S3_ACCESS_KEY_ID and S3_SECRET_ACCESS_KEY are required")
	}
	region := cfg.Region
	if region == "" {
		region = defaultS3Region
	}

	host := "s3.amazonaws.com"
	secure := true
	if cfg.Endpoint != "" {
		u, err := url.Parse(cfg.Endpoint)
		if err != nil || u.Host == "" || (u.Path != "" && u.Path != "/") {
			return nil, errors.New("S3_ENDPOINT must be a URL like http://host:port")
		}
		switch u.Scheme {
		case "https":
		case "http":
			secure = false
		default:
			return nil, errors.New("S3_ENDPOINT must start with http:// or https://")
		}
		host = u.Host
	}

	opts := &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		Secure: secure,
		Region: region,
	}
	if cfg.ForcePathStyle {
		opts.BucketLookup = minio.BucketLookupPath
	}
	client, err := minio.New(host, opts)
	if err != nil {
		return nil, fmt.Errorf("s3 client: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	exists, err := client.BucketExists(ctx, cfg.Bucket)
	if err != nil {
		return nil, fmt.Errorf("s3 bucket %q: %w", cfg.Bucket, err)
	}
	if !exists {
		err := client.MakeBucket(ctx, cfg.Bucket, minio.MakeBucketOptions{Region: region})
		if err != nil && !isBucketOwned(err) {
			return nil, fmt.Errorf("s3 create bucket %q: %w", cfg.Bucket, err)
		}
	}

	prefix := strings.TrimPrefix(cfg.Prefix, "/")
	if prefix != "" && !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	return &s3Backend{client: client, bucket: cfg.Bucket, prefix: prefix}, nil
}

// isBucketOwned reports whether a MakeBucket error means the bucket already
// exists, which happens when two instances start at the same time.
func isBucketOwned(err error) bool {
	code := minio.ToErrorResponse(err).Code
	return code == "BucketAlreadyOwnedByYou" || code == "BucketAlreadyExists"
}

func (b *s3Backend) key(d digest.Digest) string {
	hex := d.Hex()
	return b.prefix + "blobs/" + d.Algorithm() + "/" + hex[:2] + "/" + hex
}

// mapS3Err translates a missing object into ErrNotFound.
func mapS3Err(err error) error {
	if minio.ToErrorResponse(err).Code == "NoSuchKey" {
		return ErrNotFound
	}
	return err
}

func (b *s3Backend) open(d digest.Digest) (io.ReadSeekCloser, error) {
	obj, err := b.client.GetObject(context.Background(), b.bucket, b.key(d), minio.GetObjectOptions{})
	if err != nil {
		return nil, mapS3Err(err)
	}
	// GetObject is lazy; Stat issues the request that reveals a missing key.
	if _, err := obj.Stat(); err != nil {
		obj.Close()
		return nil, mapS3Err(err)
	}
	return obj, nil
}

func (b *s3Backend) stat(d digest.Digest) (int64, error) {
	info, err := b.client.StatObject(context.Background(), b.bucket, b.key(d), minio.StatObjectOptions{})
	if err != nil {
		return 0, mapS3Err(err)
	}
	return info.Size, nil
}

func (b *s3Backend) delete(d digest.Digest) error {
	err := b.client.RemoveObject(context.Background(), b.bucket, b.key(d), minio.RemoveObjectOptions{})
	if minio.ToErrorResponse(err).Code == "NoSuchKey" {
		return nil
	}
	return err
}

func (b *s3Backend) put(d digest.Digest, src string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	fi, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	_, err = b.client.PutObject(context.Background(), b.bucket, b.key(d), f, fi.Size(),
		minio.PutObjectOptions{ContentType: "application/octet-stream"})
	f.Close()
	if err != nil {
		return err
	}
	return os.Remove(src)
}

// walk lists the blob layout one directory level at a time. Recursive listing
// is avoided because some S3-compatible servers do not return nested keys for
// it; delimiter listing behaves the same everywhere.
func (b *s3Backend) walk(fn func(d digest.Digest, size int64, mod time.Time) error) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	base := b.prefix + "blobs/"
	return b.list(ctx, base, func(algDir minio.ObjectInfo) error {
		if !isDirKey(algDir.Key) {
			return nil
		}
		alg := strings.TrimSuffix(strings.TrimPrefix(algDir.Key, base), "/")
		return b.list(ctx, algDir.Key, func(xxDir minio.ObjectInfo) error {
			if !isDirKey(xxDir.Key) {
				return nil
			}
			return b.list(ctx, xxDir.Key, func(obj minio.ObjectInfo) error {
				if isDirKey(obj.Key) {
					return nil
				}
				d, err := digest.Parse(alg + ":" + strings.TrimPrefix(obj.Key, xxDir.Key))
				if err != nil {
					return nil
				}
				return fn(d, obj.Size, obj.LastModified)
			})
		})
	})
}

// list calls fn for each direct child of prefix: objects and common prefixes
// (directories, whose keys end in "/").
func (b *s3Backend) list(ctx context.Context, prefix string, fn func(minio.ObjectInfo) error) error {
	for obj := range b.client.ListObjects(ctx, b.bucket, minio.ListObjectsOptions{Prefix: prefix}) {
		if obj.Err != nil {
			return obj.Err
		}
		if err := fn(obj); err != nil {
			return err
		}
	}
	return nil
}

func isDirKey(key string) bool {
	return strings.HasSuffix(key, "/")
}
