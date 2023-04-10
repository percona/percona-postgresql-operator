package controller

import (
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
)

// jobCompleted returns "true" if the Job provided completed successfully.  Otherwise it returns
// "false".
func JobCompleted(job *batchv1.Job) bool {
	conditions := job.Status.Conditions
	for i := range conditions {
		if conditions[i].Type == batchv1.JobComplete {
			return conditions[i].Status == corev1.ConditionTrue
		}
	}
	return false
}

// jobFailed returns "true" if the Job provided has failed.  Otherwise it returns "false".
func JobFailed(job *batchv1.Job) bool {
	conditions := job.Status.Conditions
	for i := range conditions {
		if conditions[i].Type == batchv1.JobFailed {
			return conditions[i].Status == corev1.ConditionTrue
		}
	}
	return false
}
