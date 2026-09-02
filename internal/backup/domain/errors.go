package domain

import "errors"

var (
	// ErrManualBackupConflict indicates that an active manual backup job is already pending or running on the target resource.
	ErrManualBackupConflict = errors.New("a manual backup is already pending or running for this resource")

	// ErrInvalidTargetSpec indicates that the target specification fails domain constraints.
	ErrInvalidTargetSpec = errors.New("invalid backup target specification")

	// ErrPlanNotFound indicates that the requested backup plan does not exist in the tenant organization.
	ErrPlanNotFound = errors.New("backup plan not found")

	// ErrPlanNotActive indicates that the backup plan is paused or archived.
	ErrPlanNotActive = errors.New("backup plan is not active")

	// ErrJobNotFound indicates that the requested backup job does not exist in the tenant organization.
	ErrJobNotFound = errors.New("backup job not found")

	// ErrRunNotFound indicates that no backup run was found for the job.
	ErrRunNotFound = errors.New("backup run not found")

	// ErrInvalidRunFilter indicates invalid query filter parameters for backup runs.
	ErrInvalidRunFilter = errors.New("invalid backup run filter parameters")

	// ErrArtifactNotFound indicates that the requested backup artifact does not exist in the tenant organization.
	ErrArtifactNotFound = errors.New("backup artifact not found")

	// ErrArtifactDeleted indicates that the requested artifact has been deleted and is no longer available.
	ErrArtifactDeleted = errors.New("backup artifact has been deleted")

	// ErrArtifactDeleteFailed indicates physical or metadata deletion failure.
	ErrArtifactDeleteFailed = errors.New("backup artifact deletion failed")

	// ErrRunNoLongerActive indicates that the backup run is no longer active (already failed/completed/reaped).
	ErrRunNoLongerActive = errors.New("backup run is no longer active")

	// ErrArtifactChainMismatch indicates that the artifact does not match the run, job, or storage target hierarchy.
	ErrArtifactChainMismatch = errors.New("artifact chain mismatch or run is not active")

	// ErrUnsupportedBackupType indicates that the requested backup type is not supported in V1.
	ErrUnsupportedBackupType = errors.New("unsupported backup type")

	// ErrUnsupportedResourceType indicates that the resource type is not supported for backup execution in V1 (e.g. cPanel).
	ErrUnsupportedResourceType = errors.New("unsupported resource type for backup execution")

	// ErrResourceNotFound indicates that the target resource was not found in the organization.
	ErrResourceNotFound = errors.New("resource not found")

	// ErrResourceNotActive indicates that the target resource is disabled or archived.
	ErrResourceNotActive = errors.New("resource is not active")

	// ErrResourceDisabled indicates that the target resource is disabled.
	ErrResourceDisabled = errors.New("resource is disabled")

	// ErrResourceArchived indicates that the target resource is archived.
	ErrResourceArchived = errors.New("resource is archived")

	// ErrStorageTargetNotSupported indicates that the resolved storage target is unsupported or disabled.
	ErrStorageTargetNotSupported = errors.New("unsupported or disabled storage target")

	// ErrVerificationFailed indicates that the generated artifact failed integrity or format verification.
	ErrVerificationFailed = errors.New("backup verification failed")

	// ErrStorageWriteFailed indicates an unrecoverable failure during artifact persistence.
	ErrStorageWriteFailed = errors.New("storage write failed")

	// ErrBackupExecutionFailed indicates a failure during database backup stream execution.
	ErrBackupExecutionFailed = errors.New("database backup execution failed")

	// ErrBackupServiceUnavailable indicates an internal infrastructure or dependency failure.
	ErrBackupServiceUnavailable = errors.New("backup service is temporarily unavailable")

	// ErrPlanAlreadyArchived indicates that an update was attempted on an already archived plan.
	ErrPlanAlreadyArchived = errors.New("backup plan is already archived")

	// ErrInvalidPlanName indicates that the plan name is empty, invalid UTF-8, too long, or contains control characters.
	ErrInvalidPlanName = errors.New("invalid backup plan name")

	// ErrInvalidRetentionPolicy indicates that retention counts or days are zero or negative.
	ErrInvalidRetentionPolicy = errors.New("invalid retention policy")

	// ErrInvalidCronExpression indicates an invalid or unsupported cron expression.
	ErrInvalidCronExpression = errors.New("invalid 5-field cron expression")

	// ErrInvalidTimezone indicates an invalid IANA timezone location.
	ErrInvalidTimezone = errors.New("invalid IANA timezone")

	// ErrPlanResourceImmutable indicates an attempt to change the resource ID of an existing plan.
	ErrPlanResourceImmutable = errors.New("backup plan resource cannot be modified")

	// ErrPlanBackupTypeImmutable indicates an attempt to change the backup type of an existing plan.
	ErrPlanBackupTypeImmutable = errors.New("backup plan type cannot be modified")

	// ErrDuplicateScheduledPendingJob indicates that a pending scheduled job already exists for the plan.
	ErrDuplicateScheduledPendingJob = errors.New("duplicate pending scheduled job already exists for plan")

	// ErrUnauthorizedRole indicates that the user role is not permitted for the requested backup operation.
	ErrUnauthorizedRole = errors.New("unauthorized role for backup operation")

	// ErrNoVerifiableArtifacts indicates that no active, non-deleted artifacts exist for the requested backup run.
	ErrNoVerifiableArtifacts = errors.New("no verifiable backup artifacts found for run")

	// ErrUnsupportedEngineType indicates that the requested backup engine type is unsupported.
	ErrUnsupportedEngineType = errors.New("unsupported backup engine type")

	// ErrStorageTargetNotFound indicates that the storage target does not exist in the organization.
	ErrStorageTargetNotFound = errors.New("storage target not found")

	// ErrStorageTargetNotActive indicates that the storage target is disabled or archived.
	ErrStorageTargetNotActive = errors.New("storage target is not active")

	// ErrStorageTargetLocationImmutable indicates that S3 location fields cannot be changed once artifacts exist.
	ErrStorageTargetLocationImmutable = errors.New("storage target location cannot be modified while historical artifacts exist")

	// ErrCannotDeleteDefaultStorageTarget indicates that the organization default storage target cannot be deleted or archived.
	ErrCannotDeleteDefaultStorageTarget = errors.New("cannot delete or archive default storage target")

	// ErrStorageTargetInUse indicates that the storage target cannot be deleted because it is in use.
	ErrStorageTargetInUse = errors.New("storage target is referenced by backup plans or historical artifacts")

	// ErrPlanOverrideForbidden indicates that a manual job referencing a plan cannot override engine or storage target.
	ErrPlanOverrideForbidden = errors.New("cannot override engine_type or storage_target_id when triggering a plan-based job")

	// ErrInvalidStorageTargetName indicates that the target name is invalid.
	ErrInvalidStorageTargetName = errors.New("storage target name must be between 1 and 100 characters")

	// ErrInvalidStorageTargetConfig indicates that the target configuration is invalid.
	ErrInvalidStorageTargetConfig = errors.New("invalid storage target configuration")

	// ErrStorageTargetCredentialRequired indicates that credentials are required for S3 storage targets.
	ErrStorageTargetCredentialRequired = errors.New("credential is required for s3 storage target")

	// ErrIncompatibleEngineStorage indicates that the selected engine cannot store artifacts to the selected target type.
	ErrIncompatibleEngineStorage = errors.New("incompatible engine and storage target type")
)
