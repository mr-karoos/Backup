package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"backup-platform/internal/backup/domain"
	"backup-platform/internal/platform/database"
	"backup-platform/pkg/uuid"
)

// PostgresBackupRepository implements BackupRepository using a PostgreSQL database with database.TxManager.
type PostgresBackupRepository struct {
	txManager database.TxManager
}

// NewPostgresBackupRepository constructs a new PostgresBackupRepository.
func NewPostgresBackupRepository(txManager database.TxManager) *PostgresBackupRepository {
	return &PostgresBackupRepository{txManager: txManager}
}

// EnsureDefaultLocalStorageTarget returns the existing default storage target for the organization,
// or idempotently creates a default local storage target if one does not exist.
func (r *PostgresBackupRepository) EnsureDefaultLocalStorageTarget(ctx context.Context, orgID uuid.UUID) (*domain.StorageTarget, error) {
	q := r.txManager.Querier()

	// 1. Try to find existing default
	query := `
		SELECT id, organization_id, name, type, status, is_default, credential_id, config, created_at, updated_at
		FROM storage_targets
		WHERE organization_id = $1 AND is_default = true
		LIMIT 1;
	`
	row := q.QueryRow(ctx, query, orgID)
	target, err := scanStorageTarget(row)
	if err == nil {
		return target, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("failed querying default storage target: %w", err)
	}

	// 2. Insert new default local target
	newID := uuid.New()
	insertQuery := `
		INSERT INTO storage_targets (
			id, organization_id, name, type, status, is_default, credential_id, config, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, NOW(), NOW()
		)
		ON CONFLICT (organization_id) WHERE is_default = true
		DO NOTHING
		RETURNING id, organization_id, name, type, status, is_default, credential_id, config, created_at, updated_at;
	`
	insertRow := q.QueryRow(
		ctx,
		insertQuery,
		newID,
		orgID,
		"Default Local Storage",
		domain.StorageTargetTypeLocal,
		domain.StorageTargetStatusActive,
		true,
		nil,
		[]byte("{}"),
	)
	insertedTarget, err := scanStorageTarget(insertRow)
	if err == nil {
		return insertedTarget, nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		// Conflict occurred, query the one that was inserted concurrently
		return r.EnsureDefaultLocalStorageTarget(ctx, orgID)
	}

	return nil, fmt.Errorf("failed inserting default storage target: %w", err)
}

// GetStorageTargetByID retrieves a storage target by ID within an organization.
func (r *PostgresBackupRepository) GetStorageTargetByID(ctx context.Context, orgID, targetID uuid.UUID) (*domain.StorageTarget, error) {
	q := r.txManager.Querier()
	query := `
		SELECT id, organization_id, name, type, status, is_default, credential_id, config, created_at, updated_at
		FROM storage_targets
		WHERE organization_id = $1 AND id = $2;
	`
	row := q.QueryRow(ctx, query, orgID, targetID)
	target, err := scanStorageTarget(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrStorageTargetNotSupported
		}
		return nil, fmt.Errorf("failed retrieving storage target: %w", err)
	}
	return target, nil
}

// GetPlanByID retrieves a backup plan by ID within an organization.
func (r *PostgresBackupRepository) GetPlanByID(ctx context.Context, orgID, planID uuid.UUID) (*domain.BackupPlan, error) {
	q := r.txManager.Querier()
	query := `
		SELECT id, organization_id, resource_id, name, backup_type, target_spec,
		       schedule_cron, schedule_timezone, is_schedule_enabled, retention_count,
		       retention_days, status, next_run_at, created_at, updated_at
		FROM backup_plans
		WHERE organization_id = $1 AND id = $2;
	`
	row := q.QueryRow(ctx, query, orgID, planID)

	var p domain.BackupPlan
	var targetSpecBytes []byte
	var scheduleCron *string
	var retentionCount *int
	var retentionDays *int
	var nextRunAt *time.Time

	err := row.Scan(
		&p.ID,
		&p.OrganizationID,
		&p.ResourceID,
		&p.Name,
		&p.BackupType,
		&targetSpecBytes,
		&scheduleCron,
		&p.ScheduleTimezone,
		&p.IsScheduleEnabled,
		&retentionCount,
		&retentionDays,
		&p.Status,
		&nextRunAt,
		&p.CreatedAt,
		&p.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrPlanNotFound
		}
		return nil, fmt.Errorf("failed querying backup plan: %w", err)
	}

	spec, err := domain.DecodeStrictTargetSpec(p.BackupType, targetSpecBytes)
	if err != nil {
		return nil, fmt.Errorf("corrupt plan target_spec: %w", err)
	}
	p.TargetSpec = *spec
	p.ScheduleCron = scheduleCron
	p.RetentionCount = retentionCount
	p.RetentionDays = retentionDays
	p.NextRunAt = nextRunAt

	return &p, nil
}

// CreatePlan inserts a new backup plan record into the database.
func (r *PostgresBackupRepository) CreatePlan(ctx context.Context, plan *domain.BackupPlan) (*domain.BackupPlan, error) {
	q := r.txManager.Querier()
	targetSpecBytes, err := json.Marshal(plan.TargetSpec)
	if err != nil {
		return nil, fmt.Errorf("failed marshaling plan target_spec: %w", err)
	}

	query := `
		INSERT INTO backup_plans (
			id, organization_id, resource_id, name, backup_type, target_spec,
			schedule_cron, schedule_timezone, is_schedule_enabled, retention_count,
			retention_days, status, next_run_at, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, NOW(), NOW()
		)
		RETURNING created_at, updated_at;
	`
	row := q.QueryRow(
		ctx,
		query,
		plan.ID,
		plan.OrganizationID,
		plan.ResourceID,
		plan.Name,
		plan.BackupType,
		targetSpecBytes,
		plan.ScheduleCron,
		plan.ScheduleTimezone,
		plan.IsScheduleEnabled,
		plan.RetentionCount,
		plan.RetentionDays,
		plan.Status,
		plan.NextRunAt,
	)
	if err := row.Scan(&plan.CreatedAt, &plan.UpdatedAt); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			return nil, domain.ErrResourceNotFound
		}
		return nil, fmt.Errorf("failed inserting backup plan: %w", err)
	}
	return plan, nil
}

// GetPlanWithResourceByID retrieves a backup plan by ID joined with its resource name.
func (r *PostgresBackupRepository) GetPlanWithResourceByID(ctx context.Context, orgID, planID uuid.UUID) (*domain.BackupPlanWithResource, error) {
	q := r.txManager.Querier()
	query := `
		SELECT p.id, p.organization_id, p.resource_id, p.name, p.backup_type, p.target_spec,
		       p.schedule_cron, p.schedule_timezone, p.is_schedule_enabled, p.retention_count,
		       p.retention_days, p.status, p.next_run_at, p.created_at, p.updated_at,
		       r.name AS resource_name
		FROM backup_plans p
		JOIN resources r ON r.id = p.resource_id AND r.organization_id = p.organization_id
		WHERE p.organization_id = $1 AND p.id = $2;
	`
	row := q.QueryRow(ctx, query, orgID, planID)

	var p domain.BackupPlan
	var resourceName string
	var targetSpecBytes []byte
	var scheduleCron *string
	var retentionCount *int
	var retentionDays *int
	var nextRunAt *time.Time

	err := row.Scan(
		&p.ID,
		&p.OrganizationID,
		&p.ResourceID,
		&p.Name,
		&p.BackupType,
		&targetSpecBytes,
		&scheduleCron,
		&p.ScheduleTimezone,
		&p.IsScheduleEnabled,
		&retentionCount,
		&retentionDays,
		&p.Status,
		&nextRunAt,
		&p.CreatedAt,
		&p.UpdatedAt,
		&resourceName,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrPlanNotFound
		}
		return nil, fmt.Errorf("failed querying backup plan with resource: %w", err)
	}

	spec, err := domain.DecodeStrictTargetSpec(p.BackupType, targetSpecBytes)
	if err != nil {
		return nil, fmt.Errorf("corrupt plan target_spec: %w", err)
	}
	p.TargetSpec = *spec
	p.ScheduleCron = scheduleCron
	p.RetentionCount = retentionCount
	p.RetentionDays = retentionDays
	p.NextRunAt = nextRunAt

	return &domain.BackupPlanWithResource{
		Plan:         p,
		ResourceName: resourceName,
	}, nil
}

// ListPlans returns all plans matching the filter for an organization, ordered by created_at DESC, id DESC.
func (r *PostgresBackupRepository) ListPlans(ctx context.Context, orgID uuid.UUID, filter domain.PlanFilter) ([]*domain.BackupPlanWithResource, error) {
	q := r.txManager.Querier()

	var args []any
	args = append(args, orgID)

	query := `
		SELECT p.id, p.organization_id, p.resource_id, p.name, p.backup_type, p.target_spec,
		       p.schedule_cron, p.schedule_timezone, p.is_schedule_enabled, p.retention_count,
		       p.retention_days, p.status, p.next_run_at, p.created_at, p.updated_at,
		       r.name AS resource_name
		FROM backup_plans p
		JOIN resources r ON r.id = p.resource_id AND r.organization_id = p.organization_id
		WHERE p.organization_id = $1
	`

	if filter.ResourceID != nil {
		args = append(args, *filter.ResourceID)
		query += fmt.Sprintf(" AND p.resource_id = $%d", len(args))
	}

	if filter.Status != nil {
		args = append(args, *filter.Status)
		query += fmt.Sprintf(" AND p.status = $%d", len(args))
	} else {
		// Default: active and paused (exclude archived)
		query += " AND p.status IN ('active', 'paused')"
	}

	query += " ORDER BY p.created_at DESC, p.id DESC;"

	rows, err := q.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed listing backup plans: %w", err)
	}
	defer rows.Close()

	var results []*domain.BackupPlanWithResource
	for rows.Next() {
		var p domain.BackupPlan
		var resourceName string
		var targetSpecBytes []byte
		var scheduleCron *string
		var retentionCount *int
		var retentionDays *int
		var nextRunAt *time.Time

		err := rows.Scan(
			&p.ID,
			&p.OrganizationID,
			&p.ResourceID,
			&p.Name,
			&p.BackupType,
			&targetSpecBytes,
			&scheduleCron,
			&p.ScheduleTimezone,
			&p.IsScheduleEnabled,
			&retentionCount,
			&retentionDays,
			&p.Status,
			&nextRunAt,
			&p.CreatedAt,
			&p.UpdatedAt,
			&resourceName,
		)
		if err != nil {
			return nil, fmt.Errorf("failed scanning backup plan row: %w", err)
		}

		spec, err := domain.DecodeStrictTargetSpec(p.BackupType, targetSpecBytes)
		if err != nil {
			return nil, fmt.Errorf("corrupt plan target_spec: %w", err)
		}
		p.TargetSpec = *spec
		p.ScheduleCron = scheduleCron
		p.RetentionCount = retentionCount
		p.RetentionDays = retentionDays
		p.NextRunAt = nextRunAt

		results = append(results, &domain.BackupPlanWithResource{
			Plan:         p,
			ResourceName: resourceName,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed iterating backup plan rows: %w", err)
	}

	return results, nil
}

// UpdatePlan updates an active or paused backup plan. Returns ErrPlanAlreadyArchived if already archived.
func (r *PostgresBackupRepository) UpdatePlan(ctx context.Context, plan *domain.BackupPlan) (*domain.BackupPlan, error) {
	q := r.txManager.Querier()
	targetSpecBytes, err := json.Marshal(plan.TargetSpec)
	if err != nil {
		return nil, fmt.Errorf("failed marshaling plan target_spec: %w", err)
	}

	query := `
		UPDATE backup_plans
		SET name = $3,
		    target_spec = $4,
		    schedule_cron = $5,
		    schedule_timezone = $6,
		    is_schedule_enabled = $7,
		    retention_count = $8,
		    retention_days = $9,
		    status = $10,
		    next_run_at = $11,
		    updated_at = NOW()
		WHERE organization_id = $1 AND id = $2 AND status <> 'archived'
		RETURNING updated_at, created_at;
	`
	row := q.QueryRow(
		ctx,
		query,
		plan.OrganizationID,
		plan.ID,
		plan.Name,
		targetSpecBytes,
		plan.ScheduleCron,
		plan.ScheduleTimezone,
		plan.IsScheduleEnabled,
		plan.RetentionCount,
		plan.RetentionDays,
		plan.Status,
		plan.NextRunAt,
	)
	if err := row.Scan(&plan.UpdatedAt, &plan.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			existing, getErr := r.GetPlanByID(ctx, plan.OrganizationID, plan.ID)
			if getErr == nil && existing.Status == domain.PlanStatusArchived {
				return nil, domain.ErrPlanAlreadyArchived
			}
			return nil, domain.ErrPlanNotFound
		}
		return nil, fmt.Errorf("failed updating backup plan: %w", err)
	}
	return plan, nil
}

// ArchivePlan marks a backup plan as archived and clears its next_run_at.
func (r *PostgresBackupRepository) ArchivePlan(ctx context.Context, orgID, planID uuid.UUID) error {
	q := r.txManager.Querier()
	query := `
		UPDATE backup_plans
		SET status = 'archived',
		    next_run_at = NULL,
		    updated_at = NOW()
		WHERE organization_id = $1 AND id = $2;
	`
	tag, err := q.Exec(ctx, query, orgID, planID)
	if err != nil {
		return fmt.Errorf("failed archiving backup plan: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrPlanNotFound
	}
	return nil
}

// FindDuePlans queries active, schedule-enabled plans across all organizations with next_run_at <= now.
func (r *PostgresBackupRepository) FindDuePlans(ctx context.Context, now time.Time, limit int, afterNextRunAt *time.Time, afterID *uuid.UUID) ([]*domain.BackupPlan, error) {
	q := r.txManager.Querier()
	if limit <= 0 {
		limit = 100
	}

	query := `
		SELECT id, organization_id, resource_id, name, backup_type, target_spec,
		       schedule_cron, schedule_timezone, is_schedule_enabled, retention_count,
		       retention_days, status, next_run_at, created_at, updated_at
		FROM backup_plans
		WHERE status = 'active'
		  AND is_schedule_enabled = true
		  AND next_run_at IS NOT NULL
		  AND next_run_at <= $1
		  AND (
		      $3::timestamptz IS NULL
		      OR (next_run_at > $3)
		      OR (next_run_at = $3 AND id > $4)
		  )
		ORDER BY next_run_at ASC, id ASC
		LIMIT $2;
	`
	rows, err := q.Query(ctx, query, now, limit, afterNextRunAt, afterID)
	if err != nil {
		return nil, fmt.Errorf("failed querying due backup plans: %w", err)
	}
	defer rows.Close()

	var plans []*domain.BackupPlan
	for rows.Next() {
		var p domain.BackupPlan
		var targetSpecBytes []byte
		var scheduleCron *string
		var retentionCount *int
		var retentionDays *int
		var nextRunAt *time.Time

		err := rows.Scan(
			&p.ID,
			&p.OrganizationID,
			&p.ResourceID,
			&p.Name,
			&p.BackupType,
			&targetSpecBytes,
			&scheduleCron,
			&p.ScheduleTimezone,
			&p.IsScheduleEnabled,
			&retentionCount,
			&retentionDays,
			&p.Status,
			&nextRunAt,
			&p.CreatedAt,
			&p.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed scanning due plan row: %w", err)
		}

		spec, err := domain.DecodeStrictTargetSpec(p.BackupType, targetSpecBytes)
		if err != nil {
			continue // Skip corrupt target_spec safely
		}
		p.TargetSpec = *spec
		p.ScheduleCron = scheduleCron
		p.RetentionCount = retentionCount
		p.RetentionDays = retentionDays
		p.NextRunAt = nextRunAt

		plans = append(plans, &p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed iterating due plan rows: %w", err)
	}

	return plans, nil
}

// EnqueueScheduledJobAndAdvanceNextRun transactionally creates a scheduled pending job and updates next_run_at.
func (r *PostgresBackupRepository) EnqueueScheduledJobAndAdvanceNextRun(
	ctx context.Context,
	planID uuid.UUID,
	expectedUpdatedAt time.Time,
	jobToInsert *domain.BackupJob,
	newNextRunAt *time.Time,
) (*domain.BackupJob, error) {
	var enqueuedJob *domain.BackupJob

	err := r.txManager.WithinTx(ctx, func(q database.Querier) error {
		// 1. Lock and revalidate plan row
		lockQuery := `
			SELECT id, organization_id, resource_id, backup_type, target_spec, status,
			       is_schedule_enabled, next_run_at, updated_at
			FROM backup_plans
			WHERE id = $1
			FOR UPDATE;
		`
		row := q.QueryRow(ctx, lockQuery, planID)

		var currentID, currentOrgID, currentResID uuid.UUID
		var currentBackupType domain.BackupType
		var targetSpecBytes []byte
		var currentStatus domain.PlanStatus
		var currentEnabled bool
		var currentNextRunAt *time.Time
		var currentUpdatedAt time.Time

		err := row.Scan(
			&currentID,
			&currentOrgID,
			&currentResID,
			&currentBackupType,
			&targetSpecBytes,
			&currentStatus,
			&currentEnabled,
			&currentNextRunAt,
			&currentUpdatedAt,
		)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil // Plan no longer exists
			}
			return fmt.Errorf("failed locking plan row: %w", err)
		}

		// Revalidate conditions
		if currentStatus != domain.PlanStatusActive || !currentEnabled || currentNextRunAt == nil {
			return nil // Plan is no longer active/enabled
		}
		if !currentUpdatedAt.Truncate(time.Microsecond).Equal(expectedUpdatedAt.Truncate(time.Microsecond)) {
			return nil // Plan modified concurrently, skip this tick
		}

		// 2. Check if a pending scheduled job already exists for this plan
		checkQuery := `
			SELECT id FROM backup_jobs
			WHERE organization_id = $1 AND backup_plan_id = $2
			  AND trigger_type = 'scheduled' AND status = 'pending'
			LIMIT 1;
		`
		var existingPendingID uuid.UUID
		checkErr := q.QueryRow(ctx, checkQuery, currentOrgID, currentID).Scan(&existingPendingID)
		hasPending := (checkErr == nil)

		if !hasPending && jobToInsert != nil {
			// 3. Insert new scheduled BackupJob
			targetBytes, mErr := json.Marshal(jobToInsert.TargetSpec)
			if mErr != nil {
				return fmt.Errorf("failed marshaling job target spec: %w", mErr)
			}

			insertQuery := `
				INSERT INTO backup_jobs (
					id, organization_id, resource_id, backup_plan_id, trigger_type,
					created_by_user_id, backup_type, target_spec, status, created_at, updated_at
				) VALUES (
					$1, $2, $3, $4, 'scheduled', NULL, $5, $6, 'pending', NOW(), NOW()
				)
				ON CONFLICT (organization_id, backup_plan_id)
				WHERE trigger_type = 'scheduled' AND status = 'pending' AND backup_plan_id IS NOT NULL
				DO NOTHING
				RETURNING id, organization_id, resource_id, backup_plan_id, trigger_type,
				          created_by_user_id, backup_type, target_spec, status, created_at, updated_at;
			`
			inserted := &domain.BackupJob{}
			var specBytes []byte
			insRow := q.QueryRow(
				ctx,
				insertQuery,
				jobToInsert.ID,
				currentOrgID,
				currentResID,
				currentID,
				currentBackupType,
				targetBytes,
			)
			if scanErr := insRow.Scan(
				&inserted.ID,
				&inserted.OrganizationID,
				&inserted.ResourceID,
				&inserted.BackupPlanID,
				&inserted.TriggerType,
				&inserted.CreatedByUserID,
				&inserted.BackupType,
				&specBytes,
				&inserted.Status,
				&inserted.CreatedAt,
				&inserted.UpdatedAt,
			); scanErr != nil {
				if errors.Is(scanErr, pgx.ErrNoRows) {
					// Handled race condition: ON CONFLICT DO NOTHING skipped insert safely without aborting transaction
					hasPending = true
				} else {
					return fmt.Errorf("failed inserting scheduled backup job: %w", scanErr)
				}
			} else {
				spec, decErr := domain.DecodeStrictTargetSpec(inserted.BackupType, specBytes)
				if decErr != nil {
					return fmt.Errorf("failed decoding inserted job target spec: %w", decErr)
				}
				inserted.TargetSpec = *spec
				enqueuedJob = inserted
			}
		}

		// 4. Advance next_run_at
		advanceQuery := `
			UPDATE backup_plans
			SET next_run_at = $1,
			    updated_at = NOW()
			WHERE id = $2;
		`
		if _, advErr := q.Exec(ctx, advanceQuery, newNextRunAt, currentID); advErr != nil {
			return fmt.Errorf("failed advancing next_run_at: %w", advErr)
		}

		return nil
	})

	if err != nil {
		return nil, err
	}
	return enqueuedJob, nil
}

// CreateJob inserts a new BackupJob in 'pending' status, mapping exact partial index conflicts to ErrManualBackupConflict.
func (r *PostgresBackupRepository) CreateJob(ctx context.Context, job *domain.BackupJob) (*domain.BackupJob, error) {
	if job == nil {
		return nil, errors.New("job cannot be nil")
	}

	targetSpecJSON, err := json.Marshal(job.TargetSpec)
	if err != nil {
		return nil, fmt.Errorf("failed marshaling target_spec: %w", err)
	}

	q := r.txManager.Querier()
	query := `
		INSERT INTO backup_jobs (
			id, organization_id, resource_id, backup_plan_id, trigger_type,
			created_by_user_id, backup_type, target_spec, status, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5,
			$6, $7, $8, $9, NOW(), NOW()
		)
		RETURNING id, organization_id, resource_id, backup_plan_id, trigger_type,
		          created_by_user_id, backup_type, target_spec, status, created_at, updated_at;
	`
	row := q.QueryRow(
		ctx,
		query,
		job.ID,
		job.OrganizationID,
		job.ResourceID,
		job.BackupPlanID,
		job.TriggerType,
		job.CreatedByUserID,
		job.BackupType,
		targetSpecJSON,
		domain.JobStatusPending,
	)

	created, err := scanBackupJob(row)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			if pgErr.Code == "23505" && pgErr.ConstraintName == "uq_backup_jobs_manual_active_resource" {
				return nil, domain.ErrManualBackupConflict
			}
		}
		return nil, fmt.Errorf("failed creating backup job: %w", err)
	}

	return created, nil
}

// GetJobByID retrieves a backup job by ID within an organization.
func (r *PostgresBackupRepository) GetJobByID(ctx context.Context, orgID, jobID uuid.UUID) (*domain.BackupJob, error) {
	q := r.txManager.Querier()
	query := `
		SELECT id, organization_id, resource_id, backup_plan_id, trigger_type,
		       created_by_user_id, backup_type, target_spec, status, created_at, updated_at
		FROM backup_jobs
		WHERE organization_id = $1 AND id = $2;
	`
	row := q.QueryRow(ctx, query, orgID, jobID)
	job, err := scanBackupJob(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrJobNotFound
		}
		return nil, fmt.Errorf("failed retrieving backup job: %w", err)
	}
	return job, nil
}

// GetActiveManualJobForResource checks if there is an active (pending or running) manual backup job for the resource.
func (r *PostgresBackupRepository) GetActiveManualJobForResource(ctx context.Context, orgID, resourceID uuid.UUID) (*domain.BackupJob, error) {
	q := r.txManager.Querier()
	query := `
		SELECT id, organization_id, resource_id, backup_plan_id, trigger_type,
		       created_by_user_id, backup_type, target_spec, status, created_at, updated_at
		FROM backup_jobs
		WHERE organization_id = $1 AND resource_id = $2 AND trigger_type = 'manual' AND status IN ('pending', 'running')
		LIMIT 1;
	`
	row := q.QueryRow(ctx, query, orgID, resourceID)
	job, err := scanBackupJob(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil // No active manual job
		}
		return nil, fmt.Errorf("failed checking active manual job: %w", err)
	}
	return job, nil
}

// GetActiveJobConflictForResource checks if there is any running job (manual or scheduled) OR active manual job (pending or running).
func (r *PostgresBackupRepository) GetActiveJobConflictForResource(ctx context.Context, orgID, resourceID uuid.UUID) (*domain.BackupJob, error) {
	q := r.txManager.Querier()
	query := `
		SELECT id, organization_id, resource_id, backup_plan_id, trigger_type,
		       created_by_user_id, backup_type, target_spec, status, created_at, updated_at
		FROM backup_jobs
		WHERE organization_id = $1 AND resource_id = $2
		  AND (
		      status = 'running'
		      OR (trigger_type = 'manual' AND status = 'pending')
		  )
		ORDER BY created_at DESC
		LIMIT 1;
	`
	row := q.QueryRow(ctx, query, orgID, resourceID)
	job, err := scanBackupJob(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil // No conflict
		}
		return nil, fmt.Errorf("failed checking active job conflict: %w", err)
	}
	return job, nil
}

// FindPendingJobs returns pending backup jobs ordered deterministically by creation time using keyset cursor pagination.
func (r *PostgresBackupRepository) FindPendingJobs(ctx context.Context, limit int, afterCreatedAt *time.Time, afterID *uuid.UUID) ([]*domain.BackupJob, error) {
	if limit <= 0 {
		limit = 50
	}

	q := r.txManager.Querier()
	var rows pgx.Rows
	var err error

	if afterCreatedAt == nil || afterID == nil {
		query := `
			SELECT id, organization_id, resource_id, backup_plan_id, trigger_type,
			       created_by_user_id, backup_type, target_spec, status, created_at, updated_at
			FROM backup_jobs
			WHERE status = 'pending'
			ORDER BY created_at ASC, id ASC
			LIMIT $1;
		`
		rows, err = q.Query(ctx, query, limit)
	} else {
		query := `
			SELECT id, organization_id, resource_id, backup_plan_id, trigger_type,
			       created_by_user_id, backup_type, target_spec, status, created_at, updated_at
			FROM backup_jobs
			WHERE status = 'pending'
			  AND (created_at > $2 OR (created_at = $2 AND id > $3))
			ORDER BY created_at ASC, id ASC
			LIMIT $1;
		`
		rows, err = q.Query(ctx, query, limit, *afterCreatedAt, *afterID)
	}

	if err != nil {
		return nil, fmt.Errorf("failed querying pending jobs: %w", err)
	}
	defer rows.Close()

	var jobs []*domain.BackupJob
	for rows.Next() {
		job, err := scanBackupJob(rows)
		if err != nil {
			return nil, fmt.Errorf("failed scanning pending job: %w", err)
		}
		jobs = append(jobs, job)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error during pending jobs iteration: %w", err)
	}

	return jobs, nil
}

// TransactionalClaimJob atomically claims a pending job, transitions it to 'running',
// determines the next attempt number, and inserts a new BackupRun record.
func (r *PostgresBackupRepository) TransactionalClaimJob(ctx context.Context, orgID, jobID uuid.UUID) (*domain.BackupRun, *domain.BackupJob, error) {
	var run *domain.BackupRun
	var claimedJob *domain.BackupJob

	err := r.txManager.WithinTx(ctx, func(tx database.Querier) error {
		// 1. Conditional update to claim the job
		updateJobQuery := `
			UPDATE backup_jobs
			SET status = 'running', updated_at = NOW()
			WHERE id = $1 AND organization_id = $2 AND status = 'pending'
			RETURNING id, organization_id, resource_id, backup_plan_id, trigger_type,
			          created_by_user_id, backup_type, target_spec, status, created_at, updated_at;
		`
		jobRow := tx.QueryRow(ctx, updateJobQuery, jobID, orgID)
		var scanErr error
		claimedJob, scanErr = scanBackupJob(jobRow)
		if scanErr != nil {
			if errors.Is(scanErr, pgx.ErrNoRows) {
				return errors.New("job is no longer pending or does not exist")
			}
			return fmt.Errorf("failed claiming job: %w", scanErr)
		}

		// 2. Determine next attempt number
		var count int
		countQuery := `SELECT COUNT(*) FROM backup_runs WHERE job_id = $1 AND organization_id = $2;`
		if err := tx.QueryRow(ctx, countQuery, jobID, orgID).Scan(&count); err != nil {
			return fmt.Errorf("failed counting existing runs: %w", err)
		}
		attemptNumber := count + 1

		// 3. Create BackupRun record
		runID := uuid.New()
		insertRunQuery := `
			INSERT INTO backup_runs (
				id, organization_id, job_id, attempt_number, status,
				started_at, ended_at, heartbeat_at, lease_until, error_message, logs_summary,
				created_at, updated_at
			) VALUES (
				$1, $2, $3, $4, $5,
				NOW(), NULL, NOW(), NOW() + INTERVAL '2 minutes', NULL, '[]'::jsonb,
				NOW(), NOW()
			)
			RETURNING id, organization_id, job_id, attempt_number, status,
			          started_at, ended_at, heartbeat_at, lease_until, error_message, logs_summary,
			          created_at, updated_at;
		`
		runRow := tx.QueryRow(ctx, insertRunQuery, runID, orgID, jobID, attemptNumber, domain.RunStatusRunning)
		run, scanErr = scanBackupRun(runRow)
		if scanErr != nil {
			return fmt.Errorf("failed creating backup run: %w", scanErr)
		}

		return nil
	})

	if err != nil {
		return nil, nil, err
	}

	return run, claimedJob, nil
}

// GetRunByID retrieves a BackupRun by ID within an organization.
func (r *PostgresBackupRepository) GetRunByID(ctx context.Context, orgID, runID uuid.UUID) (*domain.BackupRun, error) {
	q := r.txManager.Querier()
	query := `
		SELECT id, organization_id, job_id, attempt_number, status,
		       started_at, ended_at, heartbeat_at, lease_until, error_message, logs_summary,
		       created_at, updated_at
		FROM backup_runs
		WHERE organization_id = $1 AND id = $2;
	`
	row := q.QueryRow(ctx, query, orgID, runID)
	run, err := scanBackupRun(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrRunNotFound
		}
		return nil, fmt.Errorf("failed querying backup run: %w", err)
	}
	return run, nil
}

// GetRunDetail returns a single BackupRunWithStats scoped strictly by organization ID and run ID.
func (r *PostgresBackupRepository) GetRunDetail(ctx context.Context, orgID, runID uuid.UUID) (*domain.BackupRunWithStats, error) {
	q := r.txManager.Querier()
	query := `
		SELECT 
			r.id, r.organization_id, r.job_id, r.attempt_number, r.status,
			r.started_at, r.ended_at, r.heartbeat_at, r.lease_until, r.error_message,
			r.logs_summary, r.created_at, r.updated_at,
			j.resource_id,
			COALESCE(SUM(a.size_bytes), 0)::bigint AS total_artifact_size_bytes,
			COUNT(a.id)::int AS artifacts_count
		FROM backup_runs r
		JOIN backup_jobs j ON r.job_id = j.id AND r.organization_id = j.organization_id
		LEFT JOIN backup_artifacts a ON r.id = a.run_id AND r.organization_id = a.organization_id
		WHERE r.organization_id = $1 AND r.id = $2
		GROUP BY r.id, j.resource_id;
	`
	row := q.QueryRow(ctx, query, orgID, runID)
	stat, err := scanBackupRunWithStats(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrRunNotFound
		}
		return nil, fmt.Errorf("failed querying backup run detail: %w", err)
	}
	return stat, nil
}

// ListRuns returns a list of BackupRunWithStats scoped by organization ID and filtered by parameters.
func (r *PostgresBackupRepository) ListRuns(ctx context.Context, orgID uuid.UUID, filter domain.RunFilter) ([]*domain.BackupRunWithStats, error) {
	q := r.txManager.Querier()

	var queryBuilder strings.Builder
	queryBuilder.WriteString(`
		SELECT 
			r.id, r.organization_id, r.job_id, r.attempt_number, r.status,
			r.started_at, r.ended_at, r.heartbeat_at, r.lease_until, r.error_message,
			r.logs_summary, r.created_at, r.updated_at,
			j.resource_id,
			COALESCE(SUM(a.size_bytes), 0)::bigint AS total_artifact_size_bytes,
			COUNT(a.id)::int AS artifacts_count
		FROM backup_runs r
		JOIN backup_jobs j ON r.job_id = j.id AND r.organization_id = j.organization_id
		LEFT JOIN backup_artifacts a ON r.id = a.run_id AND r.organization_id = a.organization_id
		WHERE r.organization_id = $1
	`)

	args := []any{orgID}
	argIdx := 2

	if filter.ResourceID != nil && *filter.ResourceID != uuid.Nil {
		queryBuilder.WriteString(fmt.Sprintf(" AND j.resource_id = $%d", argIdx))
		args = append(args, *filter.ResourceID)
		argIdx++
	}

	if filter.JobID != nil && *filter.JobID != uuid.Nil {
		queryBuilder.WriteString(fmt.Sprintf(" AND r.job_id = $%d", argIdx))
		args = append(args, *filter.JobID)
		argIdx++
	}

	if filter.Status != nil && *filter.Status != "" {
		queryBuilder.WriteString(fmt.Sprintf(" AND r.status = $%d", argIdx))
		args = append(args, string(*filter.Status))
		argIdx++
	}

	if filter.FromDate != nil {
		queryBuilder.WriteString(fmt.Sprintf(" AND r.created_at >= $%d", argIdx))
		args = append(args, *filter.FromDate)
		argIdx++
	}

	if filter.ToDate != nil {
		queryBuilder.WriteString(fmt.Sprintf(" AND r.created_at <= $%d", argIdx))
		args = append(args, *filter.ToDate)
		argIdx++
	}

	queryBuilder.WriteString(" GROUP BY r.id, j.resource_id ORDER BY r.started_at DESC, r.id DESC;")

	rows, err := q.Query(ctx, queryBuilder.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("failed listing backup runs: %w", err)
	}
	defer rows.Close()

	var runs []*domain.BackupRunWithStats
	for rows.Next() {
		stat, err := scanBackupRunWithStats(rows)
		if err != nil {
			return nil, fmt.Errorf("failed scanning backup run with stats: %w", err)
		}
		runs = append(runs, stat)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed reading backup run rows: %w", err)
	}

	return runs, nil
}

// GetLatestRunForJob retrieves the most recent BackupRun for a given job.
func (r *PostgresBackupRepository) GetLatestRunForJob(ctx context.Context, orgID, jobID uuid.UUID) (*domain.BackupRun, error) {
	q := r.txManager.Querier()
	query := `
		SELECT id, organization_id, job_id, attempt_number, status,
		       started_at, ended_at, heartbeat_at, lease_until, error_message, logs_summary,
		       created_at, updated_at
		FROM backup_runs
		WHERE organization_id = $1 AND job_id = $2
		ORDER BY attempt_number DESC
		LIMIT 1;
	`
	row := q.QueryRow(ctx, query, orgID, jobID)
	run, err := scanBackupRun(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrRunNotFound
		}
		return nil, fmt.Errorf("failed querying latest run: %w", err)
	}
	return run, nil
}

// UpdateHeartbeat advances the heartbeat_at timestamp and extends lease_until by 2 minutes for an active run.
func (r *PostgresBackupRepository) UpdateHeartbeat(ctx context.Context, orgID, runID uuid.UUID) error {
	q := r.txManager.Querier()
	query := `
		UPDATE backup_runs
		SET heartbeat_at = NOW(),
		    lease_until = NOW() + INTERVAL '2 minutes',
		    updated_at = NOW()
		WHERE id = $1 AND organization_id = $2 AND status = 'running';
	`
	tag, err := q.Exec(ctx, query, runID, orgID)
	if err != nil {
		return fmt.Errorf("failed updating heartbeat: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrRunNoLongerActive
	}
	return nil
}

// FinalizeRunAndJob updates the status of the run and its parent job atomically, enforcing running state guard.
func (r *PostgresBackupRepository) FinalizeRunAndJob(
	ctx context.Context,
	orgID, runID, jobID uuid.UUID,
	runStatus domain.RunStatus,
	jobStatus domain.JobStatus,
	errMsg *string,
	logsSummary []byte,
) error {
	if len(logsSummary) == 0 {
		logsSummary = []byte("[]")
	}

	return r.txManager.WithinTx(ctx, func(tx database.Querier) error {
		updateRunQuery := `
			UPDATE backup_runs
			SET status = $1,
			    ended_at = NOW(),
			    error_message = $2,
			    logs_summary = $3,
			    updated_at = NOW()
			WHERE id = $4 AND organization_id = $5 AND job_id = $6 AND status = 'running';
		`
		tag, err := tx.Exec(ctx, updateRunQuery, runStatus, errMsg, logsSummary, runID, orgID, jobID)
		if err != nil {
			return fmt.Errorf("failed updating run status: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return domain.ErrRunNoLongerActive
		}

		updateJobQuery := `
			UPDATE backup_jobs
			SET status = $1,
			    updated_at = NOW()
			WHERE id = $2 AND organization_id = $3 AND status = 'running';
		`
		jobTag, err := tx.Exec(ctx, updateJobQuery, jobStatus, jobID, orgID)
		if err != nil {
			return fmt.Errorf("failed updating job status: %w", err)
		}
		if jobTag.RowsAffected() == 0 {
			return domain.ErrRunNoLongerActive
		}

		return nil
	})
}

// CreateArtifact inserts an initial BackupArtifact record enforcing strict resource-chain and running-state invariants.
func (r *PostgresBackupRepository) CreateArtifact(ctx context.Context, artifact *domain.BackupArtifact) (*domain.BackupArtifact, error) {
	if artifact == nil {
		return nil, errors.New("artifact cannot be nil")
	}

	q := r.txManager.Querier()
	query := `
		INSERT INTO backup_artifacts (
			id, organization_id, run_id, resource_id, storage_target_id,
			artifact_type, format, target_name, storage_reference, size_bytes,
			checksum_algorithm, checksum_hash, verification_status, verified_at,
			verification_details, is_deleted, deleted_at, created_at, updated_at
		)
		SELECT
			$1, r.organization_id, r.id, j.resource_id, st.id,
			$6, $7, $8, $9, $10,
			'sha256', $11, 'unverified', NULL,
			NULL, false, NULL, NOW(), NOW()
		FROM backup_runs r
		JOIN backup_jobs j ON j.id = r.job_id AND j.organization_id = r.organization_id
		JOIN storage_targets st ON st.id = $5 AND st.organization_id = r.organization_id AND st.type = 'local' AND st.status = 'active'
		WHERE r.id = $3
		  AND r.organization_id = $2
		  AND j.resource_id = $4
		  AND r.status = 'running'
		RETURNING id, organization_id, run_id, resource_id, storage_target_id,
		          artifact_type, format, target_name, storage_reference, size_bytes,
		          checksum_algorithm, checksum_hash, verification_status, verified_at,
		          verification_details, is_deleted, deleted_at, created_at, updated_at;
	`
	row := q.QueryRow(
		ctx,
		query,
		artifact.ID,
		artifact.OrganizationID,
		artifact.RunID,
		artifact.ResourceID,
		artifact.StorageTargetID,
		artifact.ArtifactType,
		artifact.Format,
		artifact.TargetName,
		artifact.StorageReference,
		artifact.SizeBytes,
		artifact.ChecksumHash,
	)

	created, err := scanBackupArtifact(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrArtifactChainMismatch
		}
		return nil, fmt.Errorf("failed inserting backup artifact: %w", err)
	}
	return created, nil
}

// UpdateArtifactVerification updates verification status and details for an artifact.
func (r *PostgresBackupRepository) UpdateArtifactVerification(
	ctx context.Context,
	orgID, artifactID uuid.UUID,
	status domain.VerificationStatus,
	details *string,
) error {
	q := r.txManager.Querier()
	query := `
		UPDATE backup_artifacts
		SET verification_status = $1,
		    verified_at = NOW(),
		    verification_details = $2,
		    updated_at = NOW()
		WHERE id = $3 AND organization_id = $4;
	`
	tag, err := q.Exec(ctx, query, status, details, artifactID, orgID)
	if err != nil {
		return fmt.Errorf("failed updating artifact verification: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return errors.New("artifact not found or not in organization")
	}
	return nil
}

// TombstoneArtifact marks an artifact as deleted.
func (r *PostgresBackupRepository) TombstoneArtifact(ctx context.Context, orgID, artifactID uuid.UUID) error {
	q := r.txManager.Querier()
	query := `
		UPDATE backup_artifacts
		SET is_deleted = true,
		    deleted_at = NOW(),
		    updated_at = NOW()
		WHERE id = $1 AND organization_id = $2;
	`
	tag, err := q.Exec(ctx, query, artifactID, orgID)
	if err != nil {
		return fmt.Errorf("failed tombstoning artifact: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrArtifactNotFound
	}
	return nil
}

// GetArtifactByID retrieves a single backup artifact by organization ID and artifact ID.
func (r *PostgresBackupRepository) GetArtifactByID(ctx context.Context, orgID, artifactID uuid.UUID) (*domain.BackupArtifact, error) {
	q := r.txManager.Querier()
	query := `
		SELECT 
			id, organization_id, run_id, resource_id, storage_target_id,
			artifact_type, format, target_name, storage_reference, size_bytes,
			checksum_algorithm, checksum_hash, verification_status, verified_at,
			verification_details, is_deleted, deleted_at, created_at, updated_at
		FROM backup_artifacts
		WHERE organization_id = $1 AND id = $2;
	`
	row := q.QueryRow(ctx, query, orgID, artifactID)
	art, err := scanBackupArtifact(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrArtifactNotFound
		}
		return nil, fmt.Errorf("failed querying backup artifact: %w", err)
	}
	return art, nil
}

// ListArtifacts retrieves all non-deleted backup artifacts for an organization.
func (r *PostgresBackupRepository) ListArtifacts(ctx context.Context, orgID uuid.UUID) ([]*domain.BackupArtifact, error) {
	q := r.txManager.Querier()
	query := `
		SELECT 
			id, organization_id, run_id, resource_id, storage_target_id,
			artifact_type, format, target_name, storage_reference, size_bytes,
			checksum_algorithm, checksum_hash, verification_status, verified_at,
			verification_details, is_deleted, deleted_at, created_at, updated_at
		FROM backup_artifacts
		WHERE organization_id = $1 AND is_deleted = false
		ORDER BY created_at DESC, id DESC;
	`
	rows, err := q.Query(ctx, query, orgID)
	if err != nil {
		return nil, fmt.Errorf("failed listing backup artifacts: %w", err)
	}
	defer rows.Close()

	var artifacts []*domain.BackupArtifact
	for rows.Next() {
		art, err := scanBackupArtifact(rows)
		if err != nil {
			return nil, fmt.Errorf("failed scanning backup artifact: %w", err)
		}
		artifacts = append(artifacts, art)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed reading backup artifact rows: %w", err)
	}

	return artifacts, nil
}

// GetRunArtifacts retrieves all artifacts associated with a run.
func (r *PostgresBackupRepository) GetRunArtifacts(ctx context.Context, orgID, runID uuid.UUID) ([]*domain.BackupArtifact, error) {
	q := r.txManager.Querier()
	query := `
		SELECT id, organization_id, run_id, resource_id, storage_target_id,
		       artifact_type, format, target_name, storage_reference, size_bytes,
		       checksum_algorithm, checksum_hash, verification_status, verified_at,
		       verification_details, is_deleted, deleted_at, created_at, updated_at
		FROM backup_artifacts
		WHERE organization_id = $1 AND run_id = $2;
	`
	rows, err := q.Query(ctx, query, orgID, runID)
	if err != nil {
		return nil, fmt.Errorf("failed querying run artifacts: %w", err)
	}
	defer rows.Close()

	var artifacts []*domain.BackupArtifact
	for rows.Next() {
		art, err := scanBackupArtifact(rows)
		if err != nil {
			return nil, fmt.Errorf("failed scanning run artifact: %w", err)
		}
		artifacts = append(artifacts, art)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error during run artifacts iteration: %w", err)
	}

	return artifacts, nil
}

// RecoverInterruptedRuns detects running runs upon system startup, transitions them to failed,
// and resets their parent jobs to pending (if attempt < 3) or failed (if attempt >= 3).
func (r *PostgresBackupRepository) RecoverInterruptedRuns(ctx context.Context) (int, error) {
	q := r.txManager.Querier()
	query := `
		SELECT id, organization_id, job_id, attempt_number
		FROM backup_runs
		WHERE status = 'running';
	`
	rows, err := q.Query(ctx, query)
	if err != nil {
		return 0, fmt.Errorf("failed querying interrupted runs: %w", err)
	}
	defer rows.Close()

	type interruptedRun struct {
		id            uuid.UUID
		orgID         uuid.UUID
		jobID         uuid.UUID
		attemptNumber int
	}

	var toRecover []interruptedRun
	for rows.Next() {
		var ir interruptedRun
		if err := rows.Scan(&ir.id, &ir.orgID, &ir.jobID, &ir.attemptNumber); err != nil {
			return 0, fmt.Errorf("failed scanning interrupted run: %w", err)
		}
		toRecover = append(toRecover, ir)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("error during interrupted runs iteration: %w", err)
	}

	recoveredCount := 0
	for _, ir := range toRecover {
		var didTransition bool
		err := r.txManager.WithinTx(ctx, func(tx database.Querier) error {
			errMsg := "worker process restarted before completion"
			updateRun := `
				UPDATE backup_runs
				SET status = 'failed',
				    ended_at = NOW(),
				    error_message = $1,
				    updated_at = NOW()
				WHERE id = $2 AND organization_id = $3 AND job_id = $4 AND status = 'running';
			`
			tag, err := tx.Exec(ctx, updateRun, errMsg, ir.id, ir.orgID, ir.jobID)
			if err != nil {
				return err
			}
			if tag.RowsAffected() == 0 {
				return nil
			}

			nextJobStatus := domain.JobStatusPending
			if ir.attemptNumber >= 3 {
				nextJobStatus = domain.JobStatusFailed
			}

			updateJob := `
				UPDATE backup_jobs
				SET status = $1,
				    updated_at = NOW()
				WHERE id = $2 AND organization_id = $3 AND status = 'running';
			`
			jobTag, err := tx.Exec(ctx, updateJob, nextJobStatus, ir.jobID, ir.orgID)
			if err != nil {
				return err
			}
			if jobTag.RowsAffected() != 1 {
				return errors.New("cannot recover run: parent job is not in running state")
			}

			didTransition = true
			return nil
		})

		if err != nil {
			return 0, fmt.Errorf("failed recovering interrupted run %s: %w", ir.id, err)
		}
		if didTransition {
			recoveredCount++
		}
	}

	return recoveredCount, nil
}

// ReapStaleRuns finds active runs whose lease_until timestamp has expired, marks them failed,
// and resets their parent jobs to pending (if attempt < 3) or failed (if attempt >= 3).
func (r *PostgresBackupRepository) ReapStaleRuns(ctx context.Context) (int, error) {
	q := r.txManager.Querier()
	query := `
		SELECT id, organization_id, job_id, attempt_number
		FROM backup_runs
		WHERE status = 'running' AND lease_until < NOW();
	`
	rows, err := q.Query(ctx, query)
	if err != nil {
		return 0, fmt.Errorf("failed querying stale runs: %w", err)
	}
	defer rows.Close()

	type staleRun struct {
		id            uuid.UUID
		orgID         uuid.UUID
		jobID         uuid.UUID
		attemptNumber int
	}

	var toReap []staleRun
	for rows.Next() {
		var sr staleRun
		if err := rows.Scan(&sr.id, &sr.orgID, &sr.jobID, &sr.attemptNumber); err != nil {
			return 0, fmt.Errorf("failed scanning stale run: %w", err)
		}
		toReap = append(toReap, sr)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("error during stale runs iteration: %w", err)
	}

	reapedCount := 0
	for _, sr := range toReap {
		var didTransition bool
		err := r.txManager.WithinTx(ctx, func(tx database.Querier) error {
			errMsg := "worker lease expired"
			updateRun := `
				UPDATE backup_runs
				SET status = 'failed',
				    ended_at = NOW(),
				    error_message = $1,
				    updated_at = NOW()
				WHERE id = $2 AND organization_id = $3 AND job_id = $4 AND status = 'running' AND lease_until < NOW();
			`
			tag, err := tx.Exec(ctx, updateRun, errMsg, sr.id, sr.orgID, sr.jobID)
			if err != nil {
				return err
			}
			if tag.RowsAffected() == 0 {
				return nil
			}

			nextJobStatus := domain.JobStatusPending
			if sr.attemptNumber >= 3 {
				nextJobStatus = domain.JobStatusFailed
			}

			updateJob := `
				UPDATE backup_jobs
				SET status = $1,
				    updated_at = NOW()
				WHERE id = $2 AND organization_id = $3 AND status = 'running';
			`
			jobTag, err := tx.Exec(ctx, updateJob, nextJobStatus, sr.jobID, sr.orgID)
			if err != nil {
				return err
			}
			if jobTag.RowsAffected() != 1 {
				return errors.New("cannot reap stale run: parent job is not in running state")
			}

			didTransition = true
			return nil
		})

		if err != nil {
			return 0, fmt.Errorf("failed reaping stale run %s: %w", sr.id, err)
		}
		if didTransition {
			reapedCount++
		}
	}

	return reapedCount, nil
}

// Helper Scanners

type scannable interface {
	Scan(dest ...any) error
}

func scanStorageTarget(s scannable) (*domain.StorageTarget, error) {
	var t domain.StorageTarget
	var credID *uuid.UUID

	err := s.Scan(
		&t.ID,
		&t.OrganizationID,
		&t.Name,
		&t.Type,
		&t.Status,
		&t.IsDefault,
		&credID,
		&t.Config,
		&t.CreatedAt,
		&t.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	t.CredentialID = credID
	return &t, nil
}

func scanBackupJob(s scannable) (*domain.BackupJob, error) {
	var j domain.BackupJob
	var rawPlanID *uuid.UUID
	var rawUserID *uuid.UUID
	var targetSpecBytes []byte

	err := s.Scan(
		&j.ID,
		&j.OrganizationID,
		&j.ResourceID,
		&rawPlanID,
		&j.TriggerType,
		&rawUserID,
		&j.BackupType,
		&targetSpecBytes,
		&j.Status,
		&j.CreatedAt,
		&j.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	j.BackupPlanID = rawPlanID
	j.CreatedByUserID = rawUserID

	spec, err := domain.DecodeStrictTargetSpec(j.BackupType, targetSpecBytes)
	if err != nil {
		return nil, fmt.Errorf("corrupt job target_spec: %w", err)
	}
	j.TargetSpec = *spec

	return &j, nil
}

func scanBackupRun(s scannable) (*domain.BackupRun, error) {
	var r domain.BackupRun
	var endedAt *time.Time
	var errMsg *string

	err := s.Scan(
		&r.ID,
		&r.OrganizationID,
		&r.JobID,
		&r.AttemptNumber,
		&r.Status,
		&r.StartedAt,
		&endedAt,
		&r.HeartbeatAt,
		&r.LeaseUntil,
		&errMsg,
		&r.LogsSummary,
		&r.CreatedAt,
		&r.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	r.EndedAt = endedAt
	r.ErrorMessage = errMsg

	return &r, nil
}

func scanBackupArtifact(s scannable) (*domain.BackupArtifact, error) {
	var a domain.BackupArtifact
	var verifiedAt *time.Time
	var verDetails *string
	var deletedAt *time.Time

	err := s.Scan(
		&a.ID,
		&a.OrganizationID,
		&a.RunID,
		&a.ResourceID,
		&a.StorageTargetID,
		&a.ArtifactType,
		&a.Format,
		&a.TargetName,
		&a.StorageReference,
		&a.SizeBytes,
		&a.ChecksumAlgorithm,
		&a.ChecksumHash,
		&a.VerificationStatus,
		&verifiedAt,
		&verDetails,
		&a.IsDeleted,
		&deletedAt,
		&a.CreatedAt,
		&a.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	a.VerifiedAt = verifiedAt
	a.VerificationDetails = verDetails
	a.DeletedAt = deletedAt

	return &a, nil
}

func scanBackupRunWithStats(s scannable) (*domain.BackupRunWithStats, error) {
	var r domain.BackupRun
	var endedAt *time.Time
	var errMsg *string
	var resID uuid.UUID
	var totalSizeBytes int64
	var artifactsCount int

	err := s.Scan(
		&r.ID,
		&r.OrganizationID,
		&r.JobID,
		&r.AttemptNumber,
		&r.Status,
		&r.StartedAt,
		&endedAt,
		&r.HeartbeatAt,
		&r.LeaseUntil,
		&errMsg,
		&r.LogsSummary,
		&r.CreatedAt,
		&r.UpdatedAt,
		&resID,
		&totalSizeBytes,
		&artifactsCount,
	)
	if err != nil {
		return nil, err
	}

	r.EndedAt = endedAt
	r.ErrorMessage = errMsg

	return &domain.BackupRunWithStats{
		Run:                    r,
		ResourceID:             resID,
		TotalArtifactSizeBytes: totalSizeBytes,
		ArtifactsCount:         artifactsCount,
	}, nil
}
