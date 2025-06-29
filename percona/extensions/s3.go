package extensions

import (
	"io"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
)

type S3 struct {
	Region string
	Bucket string
	uriStyle string
	verifyTLS bool
	svc *s3.S3
}

func NewS3(endpoint, region, bucket string, uriStyle string, verifyTLS bool) *S3 {
	cfg := aws.NewConfig().WithRegion(region)
	if uriStyle == "path" {
		cfg = cfg.WithS3ForcePathStyle(true)
	}
	if !verifyTLS {
		cfg = cfg.WithDisableSSL(true)
	}
	if endpoint != "" {
		cfg = cfg.WithEndpoint(endpoint)
	}

	sess := session.Must(session.NewSession(cfg))
	svc := s3.New(sess)

	return &S3{
		Region: region,
		Bucket: bucket,
		uriStyle: uriStyle,
		verifyTLS: verifyTLS,
		svc:    svc,
	}
}

func (s *S3) Get(key string) (io.ReadCloser, error) {
	out, err := s.svc.GetObject(&s3.GetObjectInput{
		Bucket: &s.Bucket,
		Key:    &key,
	})

	return out.Body, err
}
