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
	"fmt"
	"io"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	storagev1 "github.com-p/ld-singh/backup-operator/api/v1"
)

// RestoreReconciler reconciles a Restore object
type RestoreReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=storage.my.domain,resources=restores,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=storage.my.domain,resources=restores/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=storage.my.domain,resources=restores/finalizers,verbs=update

// Reconcile performs the restore process.
func (r *RestoreReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the Restore resource
	restore := &storagev1.Restore{}
	if err := r.Get(ctx, req.NamespacedName, restore); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Deep copy the restore object to compare the original status later
	originalRestore := restore.DeepCopy()

	// Check if restore is already in progress
	if restore.Status.RestoreState == "InProgress" {
		logger.Info("A restore is already in progress, requeuing for later.")
		return ctrl.Result{RequeueAfter: time.Minute * 10}, nil
	}

	// Set restore status to "InProgress"
	if restore.Status.RestoreState != "InProgress" {
		logger.Info("Setting restore status to InProgress")
		restore.Status.RestoreState = "InProgress"
		if err := r.Status().Update(ctx, restore); err != nil {
			logger.Error(err, "Failed to update restore status to InProgress")
			return ctrl.Result{}, err
		}
	}

	// Perform the actual restore process
	if err := r.performRestore(ctx, restore); err != nil {
		restore.Status.RestoreState = "Failed"
	} else {
		logger.Info("Restore completed successfully")
		restore.Status.RestoreState = "Completed"
		restore.Status.LastRestoreTime = &metav1.Time{Time: time.Now()}
	}

	// Compare the original and current status, only update if there are changes
	if !equality.Semantic.DeepEqual(originalRestore.Status, restore.Status) {
		logger.Info("Updating restore status to reflect changes")
		if err := r.Status().Update(ctx, restore); err != nil {
			logger.Error(err, "Failed to update restore status")
			return ctrl.Result{}, err
		}
	} else {
		logger.Info("No status change detected, skipping update")
	}

	// Requeue if required
	return ctrl.Result{}, nil
}

// performRestore handles downloading the backup file from S3Mock and restoring it
func (r *RestoreReconciler) performRestore(ctx context.Context, restore *storagev1.Restore) error {
	logger := log.FromContext(ctx)

	// Load AWS configuration for S3Mock
	conf, err := config.LoadDefaultConfig(ctx,
		config.WithRegion("us-east-1"),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("dummy", "dummy", "")),
		config.WithEndpointResolver(aws.EndpointResolverFunc(
			func(service, region string) (aws.Endpoint, error) {
				return aws.Endpoint{URL: "http://localhost:9090"}, nil
			},
		)),
		config.WithHTTPClient(s3mockHTTPClient()), // Reusing the shared function
	)
	if err != nil {
		logger.Error(err, "failed to load AWS config for S3Mock")
		return err
	}

	// Create an S3 client using the configuration with path-style addressing enabled
	s3Client := s3.NewFromConfig(conf, func(o *s3.Options) {
		o.UsePathStyle = true
	})

	// Bucket and file details from the Restore resource
	bucket := restore.Spec.StorageLocation
	backupFile := restore.Spec.BackupFile

	// Create a temporary file to store the downloaded backup
	file, err := os.Create(fmt.Sprintf("/tmp/%s", backupFile)) // Store the backup temporarily
	if err != nil {
		logger.Error(err, "failed to create file for restore")
		return err
	}
	defer file.Close()

	// Download the backup file from S3Mock
	output, err := s3Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(backupFile),
	})
	if err != nil {
		logger.Error(err, "failed to download backup from S3Mock")
		return err
	}
	defer output.Body.Close()

	// Copy the data from the S3Mock object to the file
	if _, err = io.Copy(file, output.Body); err != nil {
		logger.Error(err, "failed to write backup data to file")
		return err
	}

	logger.Info("Downloaded backup file successfully", "file", backupFile)

	// Simulate restore process (in real cases, you'd restore your application data from the file)
	logger.Info("Restoring application from backup", "application", restore.Spec.ApplicationName)

	// Delete the backup file from S3Mock after the restore
	_, err = s3Client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(backupFile),
	})
	if err != nil {
		logger.Error(err, "failed to delete backup file from S3Mock after restore")
		return err
	}
	logger.Info("Backup file deleted from S3Mock", "file", backupFile)

	return nil
}

// SetupWithManager sets up the controller with the Manager and adds a predicate to filter out status updates
func (r *RestoreReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&storagev1.Restore{}).
		WithEventFilter(predicate.Funcs{
			UpdateFunc: func(e event.UpdateEvent) bool {
				// Only reconcile if the spec has changed, not the status
				oldRestore := e.ObjectOld.(*storagev1.Restore)
				newRestore := e.ObjectNew.(*storagev1.Restore)

				// Ignore updates where only the status is changed
				return oldRestore.Generation != newRestore.Generation
			},
		}).
		Complete(r)
}
