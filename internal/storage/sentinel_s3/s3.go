package sentinel_s3

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
	transport "github.com/aws/smithy-go/endpoints"
	"github.com/denisakp/sentinel/internal/utils"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

type MyS3Client struct {
	Client *s3.Client
	Bucket string
}

type AmazonS3Storage struct {
	Bucket    string
	Region    string
	EndPoint  string
	AccessKey string
	SecretKey string
}

type Resolver struct {
	URL *url.URL
}

func (r *Resolver) ResolveEndpoint(_ context.Context, params s3.EndpointParameters) (transport.Endpoint, error) {
	u := *r.URL
	u.Path += "/" + *params.Bucket
	return transport.Endpoint{URI: u}, nil
}

func NewS3Storage(s *AmazonS3Storage) (*MyS3Client, error) {
	endPointUrl, err := url.Parse(s.EndPoint)
	if err != nil {
		return nil, fmt.Errorf("invalid endpoint: %w", err)
	}

	s.Region = utils.DefaultValue(s.Region, "us-east-1")

	client := s3.New(s3.Options{
		UsePathStyle:       true,
		Region:             s.Region,
		EndpointResolverV2: &Resolver{URL: endPointUrl},
		Credentials: aws.CredentialsProviderFunc(func(ctx context.Context) (aws.Credentials, error) {
			return aws.Credentials{
				AccessKeyID:     s.AccessKey,
				SecretAccessKey: s.SecretKey,
			}, nil
		}),
	})

	return &MyS3Client{Client: client, Bucket: s.Bucket}, nil
}

func (clt *MyS3Client) GetBackupPath(outName string) (string, error) {
	return clt.Bucket, nil
}

func (clt *MyS3Client) WriteBackup(fileData []byte, resourcePath string) error {
	resourcePath = utils.FormatResourceValue(resourcePath)

	if err := utils.WriteData(fileData, resourcePath); err != nil {
		return err
	}

	defer utils.CleanTempDir()
	return clt.putObject(resourcePath, fileData)
}

// uploadObject uploads a single file to the specified S3 bucket.
// It uses multipart upload for large files, with a default part size of 10 MB.
// If the object already exists, it waits until the object is confirmed to be accessible.
//
// Parameters:
// - ctx: Context for request management.
// - bucketName: Name of the S3 bucket where the file will be uploaded.
// - objectKey: Key for the file in the S3 bucket, defining its location within the bucket.
// - object: Byte slice containing the file data to be uploaded.
//
// Returns an error if the upload or confirmation fails.
func (clt *MyS3Client) uploadObject(ctx context.Context, bucketName, objectKey string, object []byte) error {
	objectBuffer := bytes.NewReader(object)
	var partMiBs int64 = 10
	uploader := manager.NewUploader(clt.Client, func(u *manager.Uploader) {
		u.PartSize = partMiBs * 1024 * 1024
		u.Concurrency = 10
	})

	rst, err := uploader.Upload(ctx, &s3.PutObjectInput{
		Bucket:      &bucketName,
		Key:         &objectKey,
		Body:        objectBuffer,
		ContentType: aws.String("application/octet-stream"),
	})

	if err != nil {
		var apiErr smithy.APIError
		if errors.As(err, &apiErr) && apiErr.ErrorCode() == "EntityTootLarge" {
			return fmt.Errorf("error while uploading object to %s. The object is too large", bucketName)
		}
		return fmt.Errorf("error while uploading object to %s: %w", bucketName, err)
	}

	if err = s3.NewObjectExistsWaiter(clt.Client).Wait(ctx, &s3.HeadObjectInput{Bucket: &bucketName, Key: &objectKey}, time.Minute); err != nil {
		return fmt.Errorf("error while waiting for object to be uploaded to %s: %w", bucketName, err)
	}

	fmt.Printf("uploaded object to %s \n", rst.Location)

	return nil
}

// uploadDirectory recursively uploads all files and directories from the specified local directory
// to the S3 bucket, preserving the directory structure.
//
// Parameters:
// - localDir: Path to the local directory to be uploaded.
// - bucketPrefix: Prefix used as the root key in the S3 bucket for the uploaded directory's contents.
//
// Returns an error if reading the directory or uploading any file fails.
func (clt *MyS3Client) uploadDirectory(localDir, bucketPrefix string) error {
	entries, err := os.ReadDir(localDir)
	if err != nil {
		return fmt.Errorf("error while reading directory %s: %w", localDir, err)
	}

	for _, entry := range entries {
		localPath := filepath.Join(localDir, entry.Name())
		objectKey := filepath.Join(bucketPrefix, entry.Name())

		if entry.IsDir() {
			if err := clt.uploadDirectory(localPath, objectKey); err != nil {
				return err
			}
		} else {
			fileData, err := os.ReadFile(localPath)
			if err != nil {
				return fmt.Errorf("error while reading file data: %w", err)
			}

			if err := clt.uploadObject(context.Background(), clt.Bucket, objectKey, fileData); err != nil {
				return err
			}
		}
	}

	return nil
}

// putObject uploads a specified file or directory to the S3 bucket.
// If the resource path points to a directory, it recursively uploads
// all files and subdirectories, preserving the local structure.
// If it is a single file, it uploads the file directly.
//
// Parameters:
// - resourcePath: Path to the local file or directory to be uploaded.
// - object: Byte slice containing the file data if it’s a single file upload.
//
// Returns an error if the upload fails.
func (clt *MyS3Client) putObject(resourcePath string, object []byte) error {
	objectKey := filepath.Base(resourcePath)

	if utils.IsDirectory(resourcePath) {
		return clt.uploadDirectory(resourcePath, objectKey)
	}

	return clt.uploadObject(context.Background(), clt.Bucket, objectKey, object)
}
