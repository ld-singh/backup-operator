/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	storagev1 "github.com-p/ld-singh/backup-operator/api/v1"
)

// BackupReconciler reconciles a Backup object
type BackupReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (r *BackupReconciler) performBackup(ctx context.Context, backup *storagev1.Backup) error {
	// Get logger from context
	logger := log.FromContext(ctx)

	// Load AWS configuration for S3Mock
	conf, err := config.LoadDefaultConfig(ctx,
		config.WithRegion("us-east-1"), // Region is required, though S3Mock doesn't use it
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("dummy", "dummy", "")), // Use static credentials for S3Mock
		config.WithEndpointResolver(aws.EndpointResolverFunc(
			func(service, region string) (aws.Endpoint, error) {
				return aws.Endpoint{URL: "http://localhost:9090"}, nil // S3Mock URL
			},
		)),
		config.WithHTTPClient(s3mockHTTPClient()), // Optional: Custom HTTP client to disable SSL
	)
	if err != nil {
		logger.Error(err, "failed to load AWS config for S3Mock")
		return err
	}

	// Create an S3 client using the loaded configuration with path-style addressing enabled
	s3Client := s3.NewFromConfig(conf, func(o *s3.Options) {
		o.UsePathStyle = true // Enable path-style addressing for S3Mock
	})

	// Use storageLocation from the CRD as the bucket name and generate the backup file name
	bucket := backup.Spec.StorageLocation
	backupFileName := fmt.Sprintf("%s-backup-%d.tar.gz", backup.Spec.ApplicationName, time.Now().Unix()) // Generate backup file name

	// Create the backup file (replace this with your actual backup creation logic)
	fileReader, err := createBackupFile(backup.Spec.ApplicationName)
	if err != nil {
		logger.Error(err, "failed to create backup file")
		return err
	}
	defer fileReader.Close() // Ensure we close the file reader

	// Prepare the input for the S3 PutObject operation
	putObjectInput := &s3.PutObjectInput{
		Bucket: aws.String(bucket),         // The S3Mock bucket name
		Key:    aws.String(backupFileName), // The backup file name (the key in S3Mock)
		Body:   fileReader,                 // The file reader containing the backup data
	}

	// Perform the S3 PutObject operation to upload the backup file to S3Mock
	putObjectResult, err := s3Client.PutObject(ctx, putObjectInput) // Capture both the result and error
	if err != nil {
		logger.Error(err, "failed to upload backup to S3Mock")
		return err
	}

	// Optionally log the result of the PutObject operation
	logger.Info("Backup successfully uploaded to S3Mock", "result", putObjectResult)

	return nil
}

// s3mockHTTPClient provides a custom HTTP client to disable SSL (if needed)
func s3mockHTTPClient() *http.Client {
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true, // Disable SSL verification since S3Mock doesn't use HTTPS
		},
	}
	return &http.Client{Transport: tr}
}

// Example function to create a backup file (replace this with real logic)
func createBackupFile(applicationName string) (io.ReadCloser, error) {
	// For simplicity, create a dummy file with some content (replace this with real backup logic)
	fileName := fmt.Sprintf("%s-backup-file.txt", applicationName)
	file, err := os.Create(fileName)
	if err != nil {
		return nil, err
	}

	// Write some content to the file
	_, err = file.WriteString(fmt.Sprintf("Backup data for application: %s\n", applicationName))
	if err != nil {
		return nil, err
	}

	// Close and reopen the file for reading
	file.Close()
	return os.Open(fileName) // Return the file reader
}

// +kubebuilder:rbac:groups=storage.my.domain,resources=backups,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=storage.my.domain,resources=backups/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=storage.my.domain,resources=backups/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=endpoints,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Backup object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.19.0/pkg/reconcile
func (r *BackupReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	// Fetch the Backup resource
	backup := &storagev1.Backup{}                 // Create an empty Backup object
	err := r.Get(ctx, req.NamespacedName, backup) // Fetch the Backup from the cluster
	if err != nil {
		if client.IgnoreNotFound(err) != nil {
			return ctrl.Result{}, err // If error other than NotFound, requeue with an error
		}
		// Backup resource not found. Return and don't requeue
		return ctrl.Result{}, nil
	}

	// Check if a backup is already in progress
	if backup.Status.BackupState == "InProgress" {
		log.Info("A back up is already in Progress. Requeing for later.")
		return ctrl.Result{RequeueAfter: time.Minute * 10}, nil
	}

	// Set backup status to "InProgress"
	backup.Status.BackupState = "InProgress"
	if err := r.Status().Update(ctx, backup); err != nil {
		log.Error(err, "Failed to update backup status to InProgress")
		return ctrl.Result{}, err
	}

	// Perform the actual backups
	if err := r.performBackup(ctx, backup); err != nil {
		backup.Status.BackupState = "Failed"
		if err := r.Status().Update(ctx, backup); err != nil {
			log.Error(err, "Failed to update backup status to Failed")
		}
		return ctrl.Result{}, err
	}

	// Update the status to "Completed" if the backup succeeds
	backup.Status.LastBackupTime = metav1.Now()
	backup.Status.BackupState = "Completed"
	if err := r.Status().Update(ctx, backup); err != nil {
		log.Error(err, "Failed to update status to Completed")
		return ctrl.Result{}, err
	}

	// Requeue for the next backup
	return ctrl.Result{RequeueAfter: time.Hour * 24}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *BackupReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&storagev1.Backup{}).
		Complete(r)
}
