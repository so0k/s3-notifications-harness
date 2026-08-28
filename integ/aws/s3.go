// Package aws contains small aws-sdk-go-v2 helpers for the s3-notifications-harness
// terratest suite, styled after terratest's own modules/aws package (which does not
// itself expose bucket-notification or version-aware bucket-emptying helpers).
package aws

import (
	"context"
	"fmt"
	"strings"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	terratestaws "github.com/gruntwork-io/terratest/modules/aws"
	"github.com/gruntwork-io/terratest/modules/logger"
	"github.com/gruntwork-io/terratest/modules/testing"
	"github.com/stretchr/testify/require"
)

// GetS3BucketNotificationE fetches bucketName's notification configuration.
func GetS3BucketNotificationE(t testing.TestingT, region, bucketName string) (*s3.GetBucketNotificationConfigurationOutput, error) {
	client, err := terratestaws.NewS3ClientE(t, region)
	if err != nil {
		return nil, err
	}

	return client.GetBucketNotificationConfiguration(context.Background(), &s3.GetBucketNotificationConfigurationInput{
		Bucket: awssdk.String(bucketName),
	})
}

// UploadS3File uploads body under key in bucketName, failing the test on error.
func UploadS3File(t testing.TestingT, region, bucketName, key, body string) {
	uploader, err := terratestaws.NewS3UploaderE(t, region)
	require.NoError(t, err)

	logger.Log(t, fmt.Sprintf("Uploading s3://%s/%s", bucketName, key))
	_, err = uploader.Upload(context.Background(), &s3.PutObjectInput{
		Bucket: awssdk.String(bucketName),
		Key:    awssdk.String(key),
		Body:   strings.NewReader(body),
	})
	require.NoError(t, err)
}

// EmptyBucket deletes every object version and delete marker in bucketName, so a
// versioned bucket that isn't wired for auto-delete-on-destroy (the Terraform suite's
// awscc_s3_bucket) can still be destroyed by the harness. Tolerates the bucket not
// existing (already destroyed, or its owning stack was never deployed).
func EmptyBucket(t testing.TestingT, region, bucketName string) error {
	client, err := terratestaws.NewS3ClientE(t, region)
	if err != nil {
		return err
	}

	var keyMarker, versionIDMarker *string
	for {
		out, err := client.ListObjectVersions(context.Background(), &s3.ListObjectVersionsInput{
			Bucket:          awssdk.String(bucketName),
			KeyMarker:       keyMarker,
			VersionIdMarker: versionIDMarker,
		})
		if err != nil {
			if isNoSuchBucket(err) {
				return nil
			}
			return err
		}

		toDelete := make([]types.ObjectIdentifier, 0, len(out.Versions)+len(out.DeleteMarkers))
		for _, v := range out.Versions {
			toDelete = append(toDelete, types.ObjectIdentifier{Key: v.Key, VersionId: v.VersionId})
		}
		for _, m := range out.DeleteMarkers {
			toDelete = append(toDelete, types.ObjectIdentifier{Key: m.Key, VersionId: m.VersionId})
		}

		if len(toDelete) > 0 {
			logger.Log(t, fmt.Sprintf("Deleting %d object version(s)/delete marker(s) from %s", len(toDelete), bucketName))
			if _, err := client.DeleteObjects(context.Background(), &s3.DeleteObjectsInput{
				Bucket: awssdk.String(bucketName),
				Delete: &types.Delete{Objects: toDelete, Quiet: awssdk.Bool(true)},
			}); err != nil {
				return err
			}
		}

		if !awssdk.ToBool(out.IsTruncated) {
			return nil
		}
		keyMarker = out.NextKeyMarker
		versionIDMarker = out.NextVersionIdMarker
	}
}

func isNoSuchBucket(err error) bool {
	return err != nil && strings.Contains(err.Error(), "NoSuchBucket")
}
