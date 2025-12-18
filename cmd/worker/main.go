package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/spf13/cobra"
)

var (
	// Global flags
	useRole        bool
	useCurrentRole bool
	roleName       string
	awsAccessKeyID string
	awsSecretKey   string
	endpointURL    string
	region         string
	maxRetries     int
	retryDelay     time.Duration
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "worker",
		Short: "Kubernetes Images Sync Worker",
		Long:  "Worker for backing up container images to S3 or local storage",
	}

	// Add global flags
	rootCmd.PersistentFlags().BoolVar(&useRole, "use_role", false, "Use IAM role for authentication")
	rootCmd.PersistentFlags().BoolVar(&useCurrentRole, "use_current_role", false, "Use current AWS role (e.g., from Kubernetes service account)")
	rootCmd.PersistentFlags().StringVar(&roleName, "role_name", "", "The name of the IAM role to assume (only when --use_role is set)")
	rootCmd.PersistentFlags().StringVar(&awsAccessKeyID, "aws_access_key_id", "", "AWS access key ID")
	rootCmd.PersistentFlags().StringVar(&awsSecretKey, "aws_secret_access_key", "", "AWS secret access key")
	rootCmd.PersistentFlags().StringVar(&endpointURL, "endpoint_url", "", "S3-compatible endpoint URL")
	rootCmd.PersistentFlags().StringVar(&region, "region", "", "AWS region")
	rootCmd.PersistentFlags().IntVar(&maxRetries, "max_retries", 5, "Maximum number of retries")
	rootCmd.PersistentFlags().DurationVar(&retryDelay, "retry_delay", 5*time.Second, "Delay between retries")

	// Add commands
	rootCmd.AddCommand(exportCmd())
	rootCmd.AddCommand(cleanupCmd())

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func exportCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "export <source> <destination>",
		Short: "Export a file to S3 or local destination",
		Long:  "Transfer a file from a local source to either a local destination or an S3 bucket",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			source := args[0]
			destination := args[1]
			return runExport(source, destination)
		},
	}
}

func cleanupCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "cleanup <destination>",
		Short: "Remove a directory from S3 or local filesystem",
		Long:  "Remove a directory recursively, either local or in an S3 bucket",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			destination := args[0]
			return runCleanup(destination)
		},
	}
}

func runExport(source, destination string) error {
	// Check if source file exists
	if _, err := os.Stat(source); os.IsNotExist(err) {
		return fmt.Errorf("source file '%s' does not exist", source)
	}

	var lastErr error
	for attempt := 1; attempt <= maxRetries; attempt++ {
		if attempt > 1 {
			fmt.Printf("Retry attempt %d/%d after %v\n", attempt, maxRetries, retryDelay)
			time.Sleep(retryDelay)
		}

		var err error
		if strings.HasPrefix(destination, "s3://") {
			err = uploadToS3(source, destination)
		} else {
			err = copyLocal(source, destination)
		}

		if err == nil {
			fmt.Printf("Transfer completed successfully: %s -> %s\n", source, destination)
			return nil
		}
		lastErr = err
		fmt.Printf("Attempt %d failed: %v\n", attempt, err)
	}

	return fmt.Errorf("transfer failed after %d attempts: %w", maxRetries, lastErr)
}

func runCleanup(destination string) error {
	var lastErr error
	for attempt := 1; attempt <= maxRetries; attempt++ {
		if attempt > 1 {
			fmt.Printf("Retry attempt %d/%d after %v\n", attempt, maxRetries, retryDelay)
			time.Sleep(retryDelay)
		}

		var err error
		if strings.HasPrefix(destination, "s3://") {
			err = deleteFromS3(destination)
		} else {
			err = deleteLocal(destination)
		}

		if err == nil {
			fmt.Printf("Cleanup completed successfully: %s\n", destination)
			return nil
		}
		lastErr = err
		fmt.Printf("Attempt %d failed: %v\n", attempt, err)
	}

	return fmt.Errorf("cleanup failed after %d attempts: %w", maxRetries, lastErr)
}

func getS3Client(ctx context.Context) (*s3.Client, error) {
	var cfg aws.Config
	var err error

	// Determine region
	awsRegion := region
	if awsRegion == "" {
		awsRegion = os.Getenv("AWS_REGION")
	}
	if awsRegion == "" {
		awsRegion = os.Getenv("AWS_DEFAULT_REGION")
	}

	// Build config options
	optFns := []func(*config.LoadOptions) error{}

	if awsRegion != "" {
		optFns = append(optFns, config.WithRegion(awsRegion))
	}

	// Load base config
	cfg, err = config.LoadDefaultConfig(ctx, optFns...)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	// Handle authentication methods
	if awsAccessKeyID != "" && awsSecretKey != "" {
		// Use explicit credentials
		fmt.Println("Using explicit AWS credentials")
		cfg.Credentials = credentials.NewStaticCredentialsProvider(awsAccessKeyID, awsSecretKey, "")
	} else if useRole && roleName != "" {
		// Assume specific role
		fmt.Printf("Attempting to assume role: %s\n", roleName)
		stsClient := sts.NewFromConfig(cfg)

		// Get account ID for role ARN
		identity, err := stsClient.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
		if err != nil {
			return nil, fmt.Errorf("failed to get caller identity: %w", err)
		}

		roleARN := fmt.Sprintf("arn:aws:iam::%s:role/%s", *identity.Account, roleName)
		cfg.Credentials = stscreds.NewAssumeRoleProvider(stsClient, roleARN)
	} else if useCurrentRole {
		// Use current role (default credential chain handles this)
		fmt.Println("Using current role from environment")
		// The default config already uses the credential chain which includes
		// web identity token if AWS_WEB_IDENTITY_TOKEN_FILE is set
	} else {
		fmt.Println("Using default credential provider chain")
	}

	// Create S3 client options
	s3Opts := []func(*s3.Options){}
	if endpointURL != "" {
		s3Opts = append(s3Opts, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(endpointURL)
			o.UsePathStyle = true // Required for most S3-compatible services
		})
	}

	return s3.NewFromConfig(cfg, s3Opts...), nil
}

func parseS3Path(s3Path string) (bucket, key string) {
	path := strings.TrimPrefix(s3Path, "s3://")
	parts := strings.SplitN(path, "/", 2)
	bucket = parts[0]
	if len(parts) > 1 {
		key = parts[1]
	}
	return
}

func uploadToS3(source, destination string) error {
	ctx := context.Background()

	client, err := getS3Client(ctx)
	if err != nil {
		return fmt.Errorf("failed to create S3 client: %w", err)
	}

	bucket, key := parseS3Path(destination)

	file, err := os.Open(source) // #nosec G304 -- source path is provided by operator via CLI args
	if err != nil {
		return fmt.Errorf("failed to open source file: %w", err)
	}
	defer file.Close()

	fmt.Printf("Uploading %s to s3://%s/%s\n", source, bucket, key)

	_, err = client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
		Body:   file,
	})
	if err != nil {
		return fmt.Errorf("failed to upload to S3: %w", err)
	}

	return nil
}

func copyLocal(source, destination string) error {
	// Create destination directory if it doesn't exist
	destDir := filepath.Dir(destination)
	if err := os.MkdirAll(destDir, 0750); err != nil { // #nosec G301 -- restricted permissions for backup directory
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	// Open source file
	srcFile, err := os.Open(source) // #nosec G304 -- source path is provided by operator via CLI args
	if err != nil {
		return fmt.Errorf("failed to open source file: %w", err)
	}
	defer srcFile.Close()

	// Get source file info for permissions
	srcInfo, err := srcFile.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat source file: %w", err)
	}

	// Create destination file
	dstFile, err := os.OpenFile(destination, os.O_RDWR|os.O_CREATE|os.O_TRUNC, srcInfo.Mode()) // #nosec G304 -- destination path is provided by operator via CLI args
	if err != nil {
		return fmt.Errorf("failed to create destination file: %w", err)
	}
	defer dstFile.Close()

	// Copy content
	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return fmt.Errorf("failed to copy file content: %w", err)
	}

	fmt.Printf("Copied %s to %s\n", source, destination)
	return nil
}

func deleteFromS3(destination string) error {
	ctx := context.Background()

	client, err := getS3Client(ctx)
	if err != nil {
		return fmt.Errorf("failed to create S3 client: %w", err)
	}

	bucket, prefix := parseS3Path(destination)

	fmt.Printf("Deleting objects from s3://%s/%s\n", bucket, prefix)

	// List and delete objects
	paginator := s3.NewListObjectsV2Paginator(client, &s3.ListObjectsV2Input{
		Bucket: aws.String(bucket),
		Prefix: aws.String(prefix),
	})

	totalDeleted := 0
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("failed to list objects: %w", err)
		}

		if len(page.Contents) == 0 {
			continue
		}

		// Build list of objects to delete
		var objectsToDelete []string
		for _, obj := range page.Contents {
			objectsToDelete = append(objectsToDelete, *obj.Key)
		}

		// Delete objects in batches of 1000 (S3 limit)
		for i := 0; i < len(objectsToDelete); i += 1000 {
			end := i + 1000
			if end > len(objectsToDelete) {
				end = len(objectsToDelete)
			}

			batch := objectsToDelete[i:end]
			deleteObjects := make([]types.ObjectIdentifier, len(batch))
			for j, key := range batch {
				deleteObjects[j] = types.ObjectIdentifier{Key: aws.String(key)}
			}

			_, err := client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
				Bucket: aws.String(bucket),
				Delete: &types.Delete{
					Objects: deleteObjects,
					Quiet:   aws.Bool(true),
				},
			})
			if err != nil {
				return fmt.Errorf("failed to delete objects: %w", err)
			}

			totalDeleted += len(batch)
		}
	}

	fmt.Printf("Deleted %d objects from s3://%s/%s\n", totalDeleted, bucket, prefix)
	return nil
}

func deleteLocal(destination string) error {
	// Check if path exists
	if _, err := os.Stat(destination); os.IsNotExist(err) {
		fmt.Printf("Directory %s does not exist, nothing to delete\n", destination)
		return nil
	}

	// Remove directory recursively
	if err := os.RemoveAll(destination); err != nil {
		return fmt.Errorf("failed to remove directory: %w", err)
	}

	fmt.Printf("Deleted directory %s\n", destination)
	return nil
}
