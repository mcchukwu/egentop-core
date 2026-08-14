package project

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mcchukwu/egentop/internal/activity"
	"github.com/mcchukwu/egentop/internal/apperrors"
	"github.com/mcchukwu/egentop/internal/audit"
	"github.com/mcchukwu/egentop/pkg/db"
	"github.com/mcchukwu/egentop/pkg/pagination"
)

type Service struct {
	DB              *sql.DB
	Repo            *Repository
	AuditService    *audit.Service
	ActivityService *activity.Service
}

// Lookup exposes the narrow project lookup needed by other services without
// coupling them to the project repository.
type Lookup interface {
	GetByID(ctx context.Context, organizationID uuid.UUID, projectID uuid.UUID) (*Project, error)
}

func NewService(db *sql.DB, repo *Repository, auditService *audit.Service, activityService *activity.Service) *Service {
	return &Service{
		DB:              db,
		Repo:            repo,
		AuditService:    auditService,
		ActivityService: activityService,
	}
}

// testHookAfterMilestoneLock is an unexported hook integration tests set to
// create a deterministic concurrency window between the milestone FOR UPDATE
// read and the guarded UPDATE.
var testHookAfterMilestoneLock func()

// CreateProject creates a new project
func (s *Service) Create(ctx context.Context, createdBy uuid.UUID, organizationID uuid.UUID, req CreateProjectRequest) (*Project, error) {
	priority := ProjectPriorityMedium
	var dueDate *time.Time

	if req.Priority != "" {
		switch ProjectPriority(req.Priority) {
		case ProjectPriorityLow, ProjectPriorityMedium, ProjectPriorityHigh:
			priority = ProjectPriority(req.Priority)

		default:
			return nil, apperrors.ErrValidation
		}
	}

	if req.DueDate != nil {
		dueDate = req.DueDate
	}

	project := &Project{
		OrganizationID: organizationID,
		CreatedBy:      createdBy,
		Name:           req.Name,
		Status:         ProjectStatusDraft,
		Priority:       priority,
		DueDate:        dueDate,
	}

	if req.Description != "" {
		project.Description = &req.Description
	}

	dbCtx, cancel := db.WithDBTimeout(ctx)
	defer cancel()

	err := db.WithTransaction(dbCtx, s.DB, func(tx *sql.Tx) error {
		if err := s.Repo.Create(dbCtx, tx, project); err != nil {
			return err
		}

		err := s.AuditService.Log(dbCtx, tx, audit.LogEntry{
			OrganizationID: &organizationID,
			UserID:         &createdBy,
			Action:         "project.created",
			EntityType:     "project",
			EntityID:       &project.ID,
			Metadata: map[string]any{
				"project_id": &project.ID,
				"name":       &project.Name,
			},
		})
		if err != nil {
			return err
		}

		// Log activity
		activity := activity.NewActivity(organizationID, createdBy, &project.ID, nil, activity.ActivityProjectCreated, "Project created", map[string]any{
			"project_id": &project.ID,
			"name":       &project.Name,
		})

		if err := s.ActivityService.Log(dbCtx, tx, activity); err != nil {
			return err
		}

		return nil
	})

	return project, err
}

// ListProjects lists all projects for an organization
func (s *Service) ListByOrganizationID(ctx context.Context, organizationID uuid.UUID, q pagination.Query) ([]Project, pagination.Meta, error) {
	dbCtx, cancel := db.WithDBTimeout(ctx)
	defer cancel()

	projects, total, err := s.Repo.ListByOrganizationID(dbCtx, organizationID, q)
	if err != nil {
		return nil, pagination.Meta{}, err
	}

	return projects, pagination.NewMeta(q, total), nil
}

// GetByID gets a project by ID, scoped to the actor's organization. This is
// the narrow read used by staff contexts (e.g. the assignment package); the
// client-facing read goes through ViewProject, which adds project-scope
// enforcement for client-role actors.
func (s *Service) GetByID(ctx context.Context, organizationID uuid.UUID, projectID uuid.UUID) (*Project, error) {
	dbCtx, cancel := db.WithDBTimeout(ctx)
	defer cancel()

	return s.Repo.GetProjectByIDAndOrganizationID(dbCtx, projectID, organizationID)
}

// ViewProject reads a project for the actor. Client-role actors resolve only
// when the project is assigned to them; every other outcome is project_not_found.
func (s *Service) ViewProject(ctx context.Context, actorID uuid.UUID, role string, organizationID uuid.UUID, projectID uuid.UUID) (*Project, error) {
	dbCtx, cancel := db.WithDBTimeout(ctx)
	defer cancel()

	return s.ensureActorProjectAccess(dbCtx, actorID, role, organizationID, projectID)
}

// Update updates a project's metadata (name, description, priority, due date
// and/or status). Fields left empty in the request are unchanged.
func (s *Service) Update(ctx context.Context, userID uuid.UUID, organizationID uuid.UUID, projectID uuid.UUID, req UpdateProjectRequest) (*Project, error) {
	dbCtx, cancel := db.WithDBTimeout(ctx)
	defer cancel()

	var updated *Project

	err := db.WithTransaction(dbCtx, s.DB, func(tx *sql.Tx) error {
		// Verify the project belongs to the actor organization
		project, err := s.Repo.GetProjectByIDAndOrganizationIDForUpdate(dbCtx, tx, projectID, organizationID)
		if err != nil {
			return err
		}

		oldStatus := project.Status

		var name *string
		var description *string
		var priority *ProjectPriority
		var status *ProjectStatus
		var dueDate *time.Time

		if req.Name != "" {
			name = &req.Name
		}
		if req.Description != "" {
			description = &req.Description
		}
		if req.Priority != "" {
			p := ProjectPriority(req.Priority)
			switch p {
			case ProjectPriorityLow, ProjectPriorityMedium, ProjectPriorityHigh:
				priority = &p
			default:
				return apperrors.ErrInvalidProjectPriority
			}
		}
		if req.Status != "" {
			st := ProjectStatus(req.Status)
			if err := validateProjectStatusTransition(oldStatus, st); err != nil {
				return err
			}
			status = &st
		}
		if req.DueDate != nil {
			dueDate = req.DueDate
		}

		if err := s.Repo.UpdateDetails(dbCtx, tx, projectID, organizationID, name, description, priority, status, dueDate); err != nil {
			return err
		}

		// Audit log
		metadata := map[string]any{"project_id": projectID}
		if name != nil {
			metadata["new_name"] = *name
		}
		if priority != nil {
			metadata["old_priority"] = project.Priority
			metadata["new_priority"] = *priority
		}
		if status != nil {
			metadata["old_status"] = oldStatus
			metadata["new_status"] = *status
		}
		if dueDate != nil {
			metadata["new_due_date"] = dueDate.String()
		}

		err = s.AuditService.Log(dbCtx, tx, audit.LogEntry{
			OrganizationID: &organizationID,
			UserID:         &userID,
			Action:         "project.updated",
			EntityType:     "project",
			EntityID:       &projectID,
			Metadata:       metadata,
		})
		if err != nil {
			return err
		}

		// Log activity
		activity := activity.NewActivity(organizationID, userID, &projectID, nil, activity.ActivityProjectUpdated, "Project updated", map[string]any{
			"project_id": &projectID,
			"name":       &project.Name,
		})
		if err := s.ActivityService.Log(dbCtx, tx, activity); err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	if updated == nil {
		return s.GetByID(ctx, organizationID, projectID)
	}

	return updated, nil
}

// CreateMilestone creates a new milestone
func (s *Service) CreateMilestone(ctx context.Context, organizationID uuid.UUID, projectID uuid.UUID, userID uuid.UUID, input CreateMilestoneInput) (*Milestone, error) {
	dbCtx, cancel := db.WithDBTimeout(ctx)
	defer cancel()

	// Validate input
	var dueDate *time.Time

	if input.DueDate != nil {
		dueDate = input.DueDate
	}

	milestone := &Milestone{
		OrganizationID: organizationID,
		CreatedBy:      userID,
		Title:          input.Title,
		Description:    &input.Description,
		Status:         MilestoneStatusPending,
		PaymentStatus:  MilestonePaymentStatusUnpaid,
		DueDate:        dueDate,
	}

	err := db.WithTransaction(dbCtx, s.DB, func(tx *sql.Tx) error {
		// Verify if project belong to actor organization
		project, err := s.ensureProjectAccess(dbCtx, projectID, organizationID)
		if err != nil {
			return err
		}

		milestone.ProjectID = project.ID

		// Create milestone
		err = s.Repo.CreateMilestone(dbCtx, tx, milestone)
		if err != nil {
			return err
		}

		// Audit log
		err = s.AuditService.Log(dbCtx, tx, audit.LogEntry{
			OrganizationID: &organizationID,
			UserID:         &userID,
			Action:         "milestone.created",
			EntityType:     "milestone",
			EntityID:       &milestone.ID,
			Metadata: map[string]any{
				"project_id":   project.ID,
				"milestone_id": milestone.ID,
				"title":        milestone.Title,
			},
		})
		if err != nil {
			return err
		}

		// Log activity
		activity := activity.NewActivity(organizationID, userID, &project.ID, &milestone.ID, activity.ActivityMilestoneCreated, "Milestone created", map[string]any{
			"project_id":   &project.ID,
			"milestone_id": &milestone.ID,
			"title":        &milestone.Title,
		})
		if err := s.ActivityService.Log(dbCtx, tx, activity); err != nil {
			return err
		}

		return nil
	})

	return milestone, err
}

// ListMilestonesByProjectID lists all milestones for a project, scoped to the
// actor's organization (and to the actor's project when they are a client).
func (s *Service) ListMilestonesByProjectID(ctx context.Context, actorID uuid.UUID, role string, organizationID uuid.UUID, projectID uuid.UUID, q pagination.Query) ([]Milestone, pagination.Meta, error) {
	dbCtx, cancel := db.WithDBTimeout(ctx)
	defer cancel()

	if _, err := s.ensureActorProjectAccess(dbCtx, actorID, role, organizationID, projectID); err != nil {
		return nil, pagination.Meta{}, err
	}

	milestones, total, err := s.Repo.ListMilestonesByProjectID(dbCtx, projectID, organizationID, q)
	if err != nil {
		return nil, pagination.Meta{}, err
	}

	return milestones, pagination.NewMeta(q, total), nil
}

// GetMilestoneByID gets a milestone by ID, scoped to the actor's organization.
// This narrow read does not embed deliverables and does not apply client
// project-scope enforcement; the client-facing read goes through
// GetMilestoneDetail.
func (s *Service) GetMilestoneByID(ctx context.Context, organizationID uuid.UUID, projectID uuid.UUID, milestoneID uuid.UUID) (*Milestone, error) {
	dbCtx, cancel := db.WithDBTimeout(ctx)
	defer cancel()

	return s.Repo.GetMilestoneByIDAndProjectIDAndOrganizationID(dbCtx, milestoneID, projectID, organizationID)
}

// GetMilestoneDetail reads a milestone for the actor (with client project
// scope enforcement) and embeds its deliverables.
func (s *Service) GetMilestoneDetail(ctx context.Context, actorID uuid.UUID, role string, organizationID uuid.UUID, projectID uuid.UUID, milestoneID uuid.UUID) (*Milestone, error) {
	dbCtx, cancel := db.WithDBTimeout(ctx)
	defer cancel()

	if _, err := s.ensureActorProjectAccess(dbCtx, actorID, role, organizationID, projectID); err != nil {
		return nil, err
	}

	milestone, err := s.Repo.GetMilestoneByIDAndProjectIDAndOrganizationID(dbCtx, milestoneID, projectID, organizationID)
	if err != nil {
		return nil, err
	}

	deliverables, err := s.Repo.ListDeliverablesByMilestoneID(dbCtx, organizationID, milestoneID)
	if err != nil {
		return nil, err
	}

	milestone.Deliverables = deliverables

	return milestone, nil
}

// UpdateMilestone updates a milestone's metadata (title, description, due
// date and/or position). Fields left empty in the request are unchanged.
// Status changes go through the state-machine endpoints.
func (s *Service) UpdateMilestone(ctx context.Context, orgID uuid.UUID, userID uuid.UUID, projectID uuid.UUID, milestoneID uuid.UUID, req UpdateMilestoneRequest) (*Milestone, error) {
	dbCtx, cancel := db.WithDBTimeout(ctx)
	defer cancel()

	var updated *Milestone

	err := db.WithTransaction(dbCtx, s.DB, func(tx *sql.Tx) error {
		// Verify the milestone belongs to the actor organization
		milestone, err := s.ensureMilestoneAccess(dbCtx, projectID, milestoneID, orgID)
		if err != nil {
			return err
		}

		var title *string
		var description *string
		var dueDate *time.Time
		var position *int

		if req.Title != "" {
			title = &req.Title
		}
		if req.Description != "" {
			description = &req.Description
		}
		if req.DueDate != nil {
			dueDate = req.DueDate
		}
		if req.Position != 0 {
			position = &req.Position
		}

		if title == nil && description == nil && dueDate == nil && position == nil {
			updated = milestone
			return nil
		}

		if err := s.Repo.UpdateMilestoneDetails(dbCtx, tx, milestoneID, projectID, orgID, title, description, dueDate, position); err != nil {
			return err
		}

		// Audit log
		metadata := map[string]any{"milestone_id": milestoneID}
		if title != nil {
			metadata["old_title"] = milestone.Title
			metadata["new_title"] = *title
		}
		if dueDate != nil {
			metadata["new_due_date"] = dueDate.String()
		}

		err = s.AuditService.Log(dbCtx, tx, audit.LogEntry{
			OrganizationID: &orgID,
			UserID:         &userID,
			Action:         "milestone.updated",
			EntityType:     "milestone",
			EntityID:       &milestoneID,
			Metadata:       metadata,
		})
		if err != nil {
			return err
		}

		// Log activity
		activity := activity.NewActivity(orgID, userID, &milestone.ProjectID, &milestoneID, activity.ActivityMilestoneUpdated, "Milestone updated", map[string]any{
			"project_id":   &milestone.ProjectID,
			"milestone_id": &milestoneID,
			"title":        &milestone.Title,
		})
		if err := s.ActivityService.Log(dbCtx, tx, activity); err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	if updated == nil {
		return s.GetMilestoneByID(ctx, orgID, projectID, milestoneID)
	}

	return updated, nil
}

// AssignClient assigns, reassigns, or unassigns (nil clientID) the client of
// a project. Reassignment ends the previous client's access immediately (the
// per-request scope check is authoritative). Both unassign and reassign prune
// the displaced client's membership when they are no longer the client of any
// other project in the organization.
func (s *Service) AssignClient(ctx context.Context, actorID uuid.UUID, organizationID uuid.UUID, projectID uuid.UUID, clientID *uuid.UUID) (*Project, error) {
	dbCtx, cancel := db.WithDBTimeout(ctx)
	defer cancel()

	var result *Project

	err := db.WithTransaction(dbCtx, s.DB, func(tx *sql.Tx) error {
		project, err := s.Repo.GetProjectByIDAndOrganizationIDForUpdate(dbCtx, tx, projectID, organizationID)
		if err != nil {
			return err
		}

		oldClient := project.ClientID

		// pruneClientMembership removes a displaced client's membership once
		// they are no longer assigned to any other project in the org, and
		// audits the removal explicitly.
		pruneClientMembership := func(displaced uuid.UUID) error {
			onOther, err := s.Repo.IsClientOnAnyOtherProject(dbCtx, tx, organizationID, displaced, projectID)
			if err != nil {
				return err
			}
			if onOther {
				return nil
			}

			membershipID, err := s.Repo.DeleteClientMembership(dbCtx, tx, organizationID, displaced)
			if err != nil {
				return err
			}
			if membershipID == nil {
				return nil
			}

			return s.AuditService.Log(dbCtx, tx, audit.LogEntry{
				OrganizationID: &organizationID,
				UserID:         &actorID,
				Action:         "membership.removed",
				EntityType:     "membership",
				EntityID:       membershipID,
				Metadata: map[string]any{
					"removed_member_id": displaced.String(),
					"reason":            "client no longer assigned to any project",
				},
			})
		}

		if clientID == nil {
			// Unassign.
			if oldClient == nil {
				result = project
				return nil
			}

			if err := s.Repo.AssignClient(dbCtx, tx, projectID, organizationID, nil); err != nil {
				return err
			}

			if err := pruneClientMembership(*oldClient); err != nil {
				return err
			}

			if err := s.AuditService.Log(dbCtx, tx, audit.LogEntry{
				OrganizationID: &organizationID,
				UserID:         &actorID,
				Action:         "project.client_removed",
				EntityType:     "project",
				EntityID:       &projectID,
				Metadata: audit.VersionedMetadata(
					map[string]any{"client_id": oldClient},
					map[string]any{"client_id": nil},
					"",
				),
			}); err != nil {
				return err
			}

			if err := s.ActivityService.Log(dbCtx, tx, activity.NewActivity(
				organizationID, actorID, &projectID, nil,
				activity.ActivityProjectClientRemoved, "Client removed from project",
				map[string]any{"project_id": &projectID, "client_id": oldClient},
			)); err != nil {
				return err
			}

			result = project
			result.ClientID = nil
			return nil
		}

		// Assign / reassign.
		if oldClient != nil && *oldClient == *clientID {
			result = project
			return nil
		}

		isClient, err := s.Repo.IsActiveClientMember(dbCtx, tx, organizationID, *clientID)
		if err != nil {
			return err
		}
		if !isClient {
			return apperrors.ErrClientNotFound
		}

		if err := s.Repo.AssignClient(dbCtx, tx, projectID, organizationID, clientID); err != nil {
			return err
		}

		// Reassignment displaces the previous client: their access ends
		// immediately, and their membership is pruned once they hold no other
		// project in the org.
		if oldClient != nil {
			if err := pruneClientMembership(*oldClient); err != nil {
				return err
			}
		}

		if err := s.AuditService.Log(dbCtx, tx, audit.LogEntry{
			OrganizationID: &organizationID,
			UserID:         &actorID,
			Action:         "project.client_assigned",
			EntityType:     "project",
			EntityID:       &projectID,
			Metadata: audit.VersionedMetadata(
				map[string]any{"client_id": oldClient},
				map[string]any{"client_id": clientID},
				"",
			),
		}); err != nil {
			return err
		}

		if err := s.ActivityService.Log(dbCtx, tx, activity.NewActivity(
			organizationID, actorID, &projectID, nil,
			activity.ActivityProjectClientAssigned, "Client assigned to project",
			map[string]any{"project_id": &projectID, "client_id": clientID},
		)); err != nil {
			return err
		}

		result = project
		result.ClientID = clientID
		return nil
	})
	if err != nil {
		return nil, err
	}

	return result, nil
}

// SubmitMilestone submits a milestone for client approval. Valid from
// in_progress or changes_requested; idempotent no-op when already
// awaiting_approval. Requires the project to have a client and the milestone
// to carry at least one deliverable.
func (s *Service) SubmitMilestone(ctx context.Context, actorID uuid.UUID, organizationID uuid.UUID, projectID uuid.UUID, milestoneID uuid.UUID) (*Milestone, error) {
	dbCtx, cancel := db.WithDBTimeout(ctx)
	defer cancel()

	var result *Milestone

	err := db.WithTransaction(dbCtx, s.DB, func(tx *sql.Tx) error {
		project, err := s.Repo.GetProjectByIDAndOrganizationIDForUpdate(dbCtx, tx, projectID, organizationID)
		if err != nil {
			return err
		}
		if project.Status == ProjectStatusArchived || project.Status == ProjectStatusCancelled {
			return apperrors.ErrInvalidStatusTransition
		}

		milestone, err := s.Repo.GetMilestoneByIDAndProjectIDAndOrganizationIDForUpdate(dbCtx, tx, milestoneID, projectID, organizationID)
		if err != nil {
			return err
		}

		// Idempotent: an already-submitted milestone stays awaiting_approval
		// with no duplicate revision row or counter increment.
		if milestone.Status == MilestoneStatusAwaitingApproval {
			result = milestone
			return nil
		}

		if milestone.Status != MilestoneStatusInProgress && milestone.Status != MilestoneStatusChangesRequested {
			return apperrors.ErrInvalidStatusTransition
		}

		if project.ClientID == nil {
			return apperrors.ErrProjectHasNoClient
		}

		deliverableCount, err := s.Repo.CountDeliverables(dbCtx, tx, organizationID, milestoneID)
		if err != nil {
			return err
		}
		if deliverableCount < 1 {
			return apperrors.ErrDeliverableRequired
		}

		oldStatus := milestone.Status
		newRevision := milestone.RevisionCount + 1

		if err := s.Repo.SubmitMilestone(dbCtx, tx, milestoneID, projectID, organizationID, oldStatus); err != nil {
			return err
		}
		if err := s.Repo.CreateMilestoneRevision(dbCtx, tx, organizationID, milestoneID, newRevision, actorID); err != nil {
			return err
		}

		if err := s.AuditService.Log(dbCtx, tx, audit.LogEntry{
			OrganizationID: &organizationID,
			UserID:         &actorID,
			Action:         "milestone.submitted",
			EntityType:     "milestone",
			EntityID:       &milestoneID,
			Metadata: audit.VersionedMetadata(
				map[string]any{"status": oldStatus, "revision_count": milestone.RevisionCount},
				map[string]any{"status": MilestoneStatusAwaitingApproval, "revision_count": newRevision},
				"",
			),
		}); err != nil {
			return err
		}

		if err := s.ActivityService.Log(dbCtx, tx, activity.NewActivity(
			organizationID, actorID, &projectID, &milestoneID,
			activity.ActivityMilestoneSubmitted, "Milestone submitted for approval",
			map[string]any{"milestone_id": &milestoneID, "revision_count": newRevision},
		)); err != nil {
			return err
		}

		milestone.Status = MilestoneStatusAwaitingApproval
		milestone.RevisionCount = newRevision
		milestone.LimitReached = revisionLimitReached(milestone.RevisionCount, milestone.RevisionLimit)
		result = milestone
		return nil
	})
	if err != nil {
		return nil, err
	}

	return result, nil
}

// ApproveMilestone is the client's sign-off. Valid only from
// awaiting_approval; idempotent when already approved (no duplicate events).
// Only the project's assigned client may approve.
func (s *Service) ApproveMilestone(ctx context.Context, actorID uuid.UUID, organizationID uuid.UUID, projectID uuid.UUID, milestoneID uuid.UUID) (*Milestone, error) {
	dbCtx, cancel := db.WithDBTimeout(ctx)
	defer cancel()

	var result *Milestone

	err := db.WithTransaction(dbCtx, s.DB, func(tx *sql.Tx) error {
		if err := s.ensureActorProjectAccessInTx(dbCtx, tx, actorID, organizationID, projectID, "milestone.approve"); err != nil {
			return err
		}

		milestone, err := s.Repo.GetMilestoneByIDAndProjectIDAndOrganizationIDForUpdate(dbCtx, tx, milestoneID, projectID, organizationID)
		if err != nil {
			return err
		}

		// Idempotent: an already-approved milestone returns success with no
		// duplicate audit/activity events (double-approve race safe).
		if milestone.Status == MilestoneStatusApproved {
			result = milestone
			return nil
		}

		if milestone.Status != MilestoneStatusAwaitingApproval {
			return apperrors.ErrMilestoneNotAwaitingApproval
		}

		if testHookAfterMilestoneLock != nil {
			testHookAfterMilestoneLock()
		}

		if err := s.Repo.SetMilestoneStatus(dbCtx, tx, milestoneID, projectID, organizationID, MilestoneStatusAwaitingApproval, MilestoneStatusApproved); err != nil {
			return err
		}

		if err := s.AuditService.Log(dbCtx, tx, audit.LogEntry{
			OrganizationID: &organizationID,
			UserID:         &actorID,
			Action:         "milestone.approved",
			EntityType:     "milestone",
			EntityID:       &milestoneID,
			Metadata: audit.VersionedMetadata(
				map[string]any{"status": milestone.Status},
				map[string]any{"status": MilestoneStatusApproved},
				"",
			),
		}); err != nil {
			return err
		}

		if err := s.ActivityService.Log(dbCtx, tx, activity.NewActivity(
			organizationID, actorID, &projectID, &milestoneID,
			activity.ActivityMilestoneApproved, "Milestone approved",
			map[string]any{"milestone_id": &milestoneID},
		)); err != nil {
			return err
		}

		milestone.Status = MilestoneStatusApproved
		result = milestone
		return nil
	})
	if err != nil {
		return nil, err
	}

	return result, nil
}

// RequestMilestoneChanges is the client's request to revise a submission.
// Valid only from awaiting_approval; NOT idempotent (a stale client request
// returns 409 milestone_not_awaiting_approval).
func (s *Service) RequestMilestoneChanges(ctx context.Context, actorID uuid.UUID, organizationID uuid.UUID, projectID uuid.UUID, milestoneID uuid.UUID, notes string) (*Milestone, error) {
	dbCtx, cancel := db.WithDBTimeout(ctx)
	defer cancel()

	var result *Milestone

	err := db.WithTransaction(dbCtx, s.DB, func(tx *sql.Tx) error {
		if err := s.ensureActorProjectAccessInTx(dbCtx, tx, actorID, organizationID, projectID, "milestone.revision.request"); err != nil {
			return err
		}

		milestone, err := s.Repo.GetMilestoneByIDAndProjectIDAndOrganizationIDForUpdate(dbCtx, tx, milestoneID, projectID, organizationID)
		if err != nil {
			return err
		}

		if milestone.Status != MilestoneStatusAwaitingApproval {
			return apperrors.ErrMilestoneNotAwaitingApproval
		}

		if err := s.Repo.SetMilestoneStatus(dbCtx, tx, milestoneID, projectID, organizationID, MilestoneStatusAwaitingApproval, MilestoneStatusChangesRequested); err != nil {
			return err
		}

		if err := s.AuditService.Log(dbCtx, tx, audit.LogEntry{
			OrganizationID: &organizationID,
			UserID:         &actorID,
			Action:         "milestone.changes_requested",
			EntityType:     "milestone",
			EntityID:       &milestoneID,
			Metadata: audit.VersionedMetadata(
				map[string]any{"status": milestone.Status},
				map[string]any{"status": MilestoneStatusChangesRequested},
				notes,
			),
		}); err != nil {
			return err
		}

		if err := s.ActivityService.Log(dbCtx, tx, activity.NewActivity(
			organizationID, actorID, &projectID, &milestoneID,
			activity.ActivityMilestoneChangesRequested, "Changes requested on milestone",
			map[string]any{"milestone_id": &milestoneID, "notes": notes},
		)); err != nil {
			return err
		}

		milestone.Status = MilestoneStatusChangesRequested
		result = milestone
		return nil
	})
	if err != nil {
		return nil, err
	}

	return result, nil
}

// UpdateMilestoneStatus applies a generic staff transition per the canonical
// state machine. Action-endpoint statuses (awaiting_approval, approved,
// changes_requested) can never be PATCHed to; completed and cancelled are
// terminal. State-machine actions are blocked when the project is
// archived/cancelled, matching submit/approve/changes-requested.
func (s *Service) UpdateMilestoneStatus(ctx context.Context, userID uuid.UUID, organizationID uuid.UUID, projectID uuid.UUID, milestoneID uuid.UUID, next MilestoneStatus) (*Milestone, error) {
	dbCtx, cancel := db.WithDBTimeout(ctx)
	defer cancel()

	var result *Milestone

	err := db.WithTransaction(dbCtx, s.DB, func(tx *sql.Tx) error {
		project, err := s.Repo.GetProjectByIDAndOrganizationIDForUpdate(dbCtx, tx, projectID, organizationID)
		if err != nil {
			return err
		}
		if project.Status == ProjectStatusArchived || project.Status == ProjectStatusCancelled {
			return apperrors.ErrInvalidStatusTransition
		}

		milestone, err := s.Repo.GetMilestoneByIDAndProjectIDAndOrganizationIDForUpdate(dbCtx, tx, milestoneID, projectID, organizationID)
		if err != nil {
			return err
		}

		if next == MilestoneStatusAwaitingApproval || next == MilestoneStatusApproved || next == MilestoneStatusChangesRequested {
			return apperrors.ErrInvalidStatusTransition
		}

		if milestone.Status == next {
			// Same-state no-op: return the milestone without a transition event.
			result = milestone
			return nil
		}

		if err := validateMilestoneStatusTransition(milestone.Status, next); err != nil {
			return err
		}

		if err := s.Repo.SetMilestoneStatus(dbCtx, tx, milestoneID, projectID, organizationID, milestone.Status, next); err != nil {
			return err
		}

		// Re-read within the transaction so the audit metadata and the
		// response carry the actual completed_at value stamped by the guarded
		// UPDATE (approved -> completed sets it in the DB, not in Go).
		updated, err := s.Repo.GetMilestoneByIDAndProjectIDAndOrganizationIDForUpdate(dbCtx, tx, milestoneID, projectID, organizationID)
		if err != nil {
			return err
		}

		if err := s.AuditService.Log(dbCtx, tx, audit.LogEntry{
			OrganizationID: &organizationID,
			UserID:         &userID,
			Action:         "milestone.status_changed",
			EntityType:     "milestone",
			EntityID:       &milestoneID,
			Metadata: audit.VersionedMetadata(
				map[string]any{"status": milestone.Status, "completed_at": milestone.CompletedAt},
				map[string]any{"status": next, "completed_at": updated.CompletedAt},
				"",
			),
		}); err != nil {
			return err
		}

		if err := s.ActivityService.Log(dbCtx, tx, activity.NewActivity(
			organizationID, userID, &projectID, &milestoneID,
			activity.ActivityMilestoneStatusChanged, "Milestone status changed",
			map[string]any{"milestone_id": &milestoneID, "old_status": milestone.Status, "new_status": next},
		)); err != nil {
			return err
		}

		result = updated
		return nil
	})
	if err != nil {
		return nil, err
	}

	return result, nil
}

// CreateDeliverable adds a link-based deliverable to a milestone. Milestones
// in completed or cancelled state are frozen (delete + re-add is the edit
// mechanism).
func (s *Service) CreateDeliverable(ctx context.Context, userID uuid.UUID, organizationID uuid.UUID, projectID uuid.UUID, milestoneID uuid.UUID, url string, title *string, description *string) (*Deliverable, error) {
	dbCtx, cancel := db.WithDBTimeout(ctx)
	defer cancel()

	if !isHTTPURL(url) {
		return nil, apperrors.ErrValidation
	}

	var result *Deliverable

	err := db.WithTransaction(dbCtx, s.DB, func(tx *sql.Tx) error {
		milestone, err := s.Repo.GetMilestoneByIDAndProjectIDAndOrganizationIDForUpdate(dbCtx, tx, milestoneID, projectID, organizationID)
		if err != nil {
			return err
		}

		if milestone.Status == MilestoneStatusCompleted || milestone.Status == MilestoneStatusCancelled {
			return apperrors.ErrInvalidStatusTransition
		}

		d := &Deliverable{
			OrganizationID: organizationID,
			MilestoneID:    milestoneID,
			URL:            url,
			Title:          title,
			Description:    description,
			SubmittedBy:    userID,
		}

		if err := s.Repo.CreateDeliverable(dbCtx, tx, d); err != nil {
			return err
		}

		if err := s.AuditService.Log(dbCtx, tx, audit.LogEntry{
			OrganizationID: &organizationID,
			UserID:         &userID,
			Action:         "deliverable.submitted",
			EntityType:     "deliverable",
			EntityID:       &d.ID,
			Metadata: audit.VersionedMetadata(
				nil,
				map[string]any{"url": d.URL, "title": d.Title, "milestone_id": milestoneID},
				"",
			),
		}); err != nil {
			return err
		}

		if err := s.ActivityService.Log(dbCtx, tx, activity.NewActivity(
			organizationID, userID, &projectID, &milestoneID,
			activity.ActivityDeliverableSubmitted, "Deliverable submitted",
			map[string]any{"milestone_id": &milestoneID, "deliverable_id": &d.ID, "url": d.URL},
		)); err != nil {
			return err
		}

		result = d
		return nil
	})
	if err != nil {
		return nil, err
	}

	return result, nil
}

// DeleteDeliverable removes a deliverable. Milestones in completed or
// cancelled state are frozen.
func (s *Service) DeleteDeliverable(ctx context.Context, userID uuid.UUID, organizationID uuid.UUID, projectID uuid.UUID, milestoneID uuid.UUID, deliverableID uuid.UUID) error {
	dbCtx, cancel := db.WithDBTimeout(ctx)
	defer cancel()

	return db.WithTransaction(dbCtx, s.DB, func(tx *sql.Tx) error {
		milestone, err := s.Repo.GetMilestoneByIDAndProjectIDAndOrganizationIDForUpdate(dbCtx, tx, milestoneID, projectID, organizationID)
		if err != nil {
			return err
		}

		if milestone.Status == MilestoneStatusCompleted || milestone.Status == MilestoneStatusCancelled {
			return apperrors.ErrInvalidStatusTransition
		}

		existing, err := s.Repo.GetDeliverableByID(dbCtx, tx, organizationID, milestoneID, deliverableID)
		if err != nil {
			return err
		}

		if err := s.Repo.DeleteDeliverable(dbCtx, tx, organizationID, milestoneID, deliverableID); err != nil {
			return err
		}

		if err := s.AuditService.Log(dbCtx, tx, audit.LogEntry{
			OrganizationID: &organizationID,
			UserID:         &userID,
			Action:         "deliverable.removed",
			EntityType:     "deliverable",
			EntityID:       &deliverableID,
			Metadata: audit.VersionedMetadata(
				map[string]any{"url": existing.URL, "milestone_id": milestoneID},
				nil,
				"",
			),
		}); err != nil {
			return err
		}

		if err := s.ActivityService.Log(dbCtx, tx, activity.NewActivity(
			organizationID, userID, &projectID, &milestoneID,
			activity.ActivityDeliverableRemoved, "Deliverable removed",
			map[string]any{"milestone_id": &milestoneID, "deliverable_id": deliverableID},
		)); err != nil {
			return err
		}

		return nil
	})
}

// UpdateMilestonePaymentStatus updates the display-only payment status. Any
// transition is allowed and audited; no state restriction applies.
func (s *Service) UpdateMilestonePaymentStatus(ctx context.Context, userID uuid.UUID, organizationID uuid.UUID, projectID uuid.UUID, milestoneID uuid.UUID, next MilestonePaymentStatus) (*Milestone, error) {
	dbCtx, cancel := db.WithDBTimeout(ctx)
	defer cancel()

	var result *Milestone

	err := db.WithTransaction(dbCtx, s.DB, func(tx *sql.Tx) error {
		milestone, err := s.Repo.GetMilestoneByIDAndProjectIDAndOrganizationIDForUpdate(dbCtx, tx, milestoneID, projectID, organizationID)
		if err != nil {
			return err
		}

		if milestone.PaymentStatus == next {
			result = milestone
			return nil
		}

		if err := s.Repo.SetMilestonePaymentStatus(dbCtx, tx, milestoneID, projectID, organizationID, milestone.PaymentStatus, next); err != nil {
			return err
		}

		if err := s.AuditService.Log(dbCtx, tx, audit.LogEntry{
			OrganizationID: &organizationID,
			UserID:         &userID,
			Action:         "milestone.payment_status_changed",
			EntityType:     "milestone",
			EntityID:       &milestoneID,
			Metadata: audit.VersionedMetadata(
				map[string]any{"payment_status": milestone.PaymentStatus},
				map[string]any{"payment_status": next},
				"",
			),
		}); err != nil {
			return err
		}

		if err := s.ActivityService.Log(dbCtx, tx, activity.NewActivity(
			organizationID, userID, &projectID, &milestoneID,
			activity.ActivityMilestonePaymentStatusChanged, "Milestone payment status changed",
			map[string]any{"milestone_id": &milestoneID, "old_status": milestone.PaymentStatus, "new_status": next},
		)); err != nil {
			return err
		}

		milestone.PaymentStatus = next
		result = milestone
		return nil
	})
	if err != nil {
		return nil, err
	}

	return result, nil
}

// GetApprovalView builds the shared deep-link payload: the project plus every
// milestone with deliverables and payment status. It is the WhatsApp landing
// page for clients. Revision limit/limit_reached are deliberately excluded.
func (s *Service) GetApprovalView(ctx context.Context, actorID uuid.UUID, role string, organizationID uuid.UUID, projectID uuid.UUID) (*ApprovalView, error) {
	dbCtx, cancel := db.WithDBTimeout(ctx)
	defer cancel()

	project, err := s.ensureActorProjectAccess(dbCtx, actorID, role, organizationID, projectID)
	if err != nil {
		return nil, err
	}

	milestones, err := s.Repo.ListAllMilestonesByProjectID(dbCtx, organizationID, projectID)
	if err != nil {
		return nil, err
	}

	view := &ApprovalView{
		Project:    *project,
		Milestones: make([]ApprovalMilestone, 0, len(milestones)),
	}

	for _, m := range milestones {
		deliverables, err := s.Repo.ListDeliverablesByMilestoneID(dbCtx, organizationID, m.ID)
		if err != nil {
			return nil, err
		}

		view.Milestones = append(view.Milestones, ApprovalMilestone{
			ID:            m.ID,
			ProjectID:     m.ProjectID,
			Title:         m.Title,
			Description:   m.Description,
			Status:        m.Status,
			DueDate:       m.DueDate,
			Position:      m.Position,
			RevisionCount: m.RevisionCount,
			PaymentStatus: m.PaymentStatus,
			Deliverables:  deliverables,
			CreatedAt:     m.CreatedAt,
			UpdatedAt:     m.UpdatedAt,
		})
	}

	return view, nil
}

// ListProjectActivities returns the project-scoped activity feed. Client
// actors are restricted to their own project by ensureActorProjectAccess.
func (s *Service) ListProjectActivities(ctx context.Context, actorID uuid.UUID, role string, organizationID uuid.UUID, projectID uuid.UUID, q pagination.Query) ([]activity.Activity, pagination.Meta, error) {
	dbCtx, cancel := db.WithDBTimeout(ctx)
	defer cancel()

	if _, err := s.ensureActorProjectAccess(dbCtx, actorID, role, organizationID, projectID); err != nil {
		return nil, pagination.Meta{}, err
	}

	return s.ActivityService.ListByProjectID(dbCtx, organizationID, projectID, q)
}

// --- Helpers ---

// Ensure project is accessible by the user (staff path: org-scoped only).
func (s *Service) ensureProjectAccess(ctx context.Context, projectID uuid.UUID, organizationID uuid.UUID) (*Project, error) {
	project, err := s.Repo.GetProjectByIDAndOrganizationID(ctx, projectID, organizationID)

	return project, err
}

// Ensure milestone is accessible by the user
func (s *Service) ensureMilestoneAccess(ctx context.Context, projectID uuid.UUID, milestoneID uuid.UUID, organizationID uuid.UUID) (*Milestone, error) {
	milestone, err := s.Repo.GetMilestoneByIDAndProjectIDAndOrganizationID(ctx, milestoneID, projectID, organizationID)

	return milestone, err
}

// ensureActorProjectAccess enforces project scope for client-role actors on
// read paths. Staff actors pass through the plain org-scoped lookup. A client
// actor resolves ONLY when projects.client_id == actor; any other outcome is
// recorded as a denied authz decision (with resource identity) and returned
// as project_not_found so existence never leaks.
func (s *Service) ensureActorProjectAccess(ctx context.Context, actorID uuid.UUID, role string, organizationID uuid.UUID, projectID uuid.UUID) (*Project, error) {
	project, err := s.Repo.GetProjectByIDAndOrganizationID(ctx, projectID, organizationID)
	if err != nil {
		return nil, err
	}

	if role != "client" {
		return project, nil
	}

	if project.ClientID == nil || *project.ClientID != actorID {
		_ = audit.RecordDecision(ctx, s.DB, organizationID, actorID, "project.view", "project", projectID, false, "client actor outside project scope")
		return nil, apperrors.ErrProjectNotFound
	}

	return project, nil
}

// ensureActorProjectAccessInTx is the transactional variant used by the
// client-only approval actions. The project row is locked for the duration of
// the transaction and the actor must be the project's assigned client. The
// scope check comes first so a non-client actor cannot learn whether the
// project exists or its state (404, never 403/400).
func (s *Service) ensureActorProjectAccessInTx(ctx context.Context, tx *sql.Tx, actorID uuid.UUID, organizationID uuid.UUID, projectID uuid.UUID, permissionKey string) error {
	project, err := s.Repo.GetProjectByIDAndOrganizationIDForUpdate(ctx, tx, projectID, organizationID)
	if err != nil {
		return err
	}

	// Approve / changes-requested are the client's signature actions: only the
	// project's assigned client may perform them, regardless of RBAC grants.
	if project.ClientID == nil || *project.ClientID != actorID {
		_ = audit.RecordDecision(ctx, s.DB, organizationID, actorID, permissionKey, "project", projectID, false, "actor is not the project's client")
		return apperrors.ErrProjectNotFound
	}

	if project.Status == ProjectStatusArchived || project.Status == ProjectStatusCancelled {
		return apperrors.ErrInvalidStatusTransition
	}

	return nil
}

// revisionLimitReached is the pure limit_reached computation:
// limit_reached = limit IS NOT NULL AND revision_count >= limit.
func revisionLimitReached(revisionCount int, effectiveLimit *int) bool {
	return effectiveLimit != nil && revisionCount >= *effectiveLimit
}

// isHTTPURL reports whether the deliverable URL uses the http or https scheme
// (mirrors the milestone_deliverables_url_prefix_check CHECK constraint).
func isHTTPURL(u string) bool {
	return strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://")
}

// Validate the transition between project statuses
func validateProjectStatusTransition(current ProjectStatus, next ProjectStatus) error {
	if current == next {
		return nil
	}

	switch current {
	case ProjectStatusDraft:
		if next == ProjectStatusActive || next == ProjectStatusArchived || next == ProjectStatusCancelled {
			return nil
		}
	case ProjectStatusActive:
		if next == ProjectStatusCompleted || next == ProjectStatusArchived || next == ProjectStatusCancelled {
			return nil
		}
	case ProjectStatusCompleted:
		if next == ProjectStatusArchived {
			return nil
		}

	case ProjectStatusArchived:
		return apperrors.ErrInvalidStatusTransition
	case ProjectStatusCancelled:
		return apperrors.ErrInvalidStatusTransition
	}

	return apperrors.ErrInvalidStatusTransition
}

// validateMilestoneStatusTransition is the single source of truth for the
// generic staff PATCH /status state machine.
//
//	Pending          -> InProgress | Cancelled
//	InProgress       -> Pending | Blocked | Cancelled
//	Blocked          -> InProgress | Cancelled
//	AwaitingApproval -> Blocked | Cancelled        (escape hatch; never Completed)
//	ChangesRequested -> InProgress | Blocked | Cancelled
//	Approved         -> Completed                  (stamps completed_at)
//	Completed | Cancelled -> terminal
//
// Cancellation is the escape hatch and must exist from every non-terminal
// state. The action-only statuses (awaiting_approval, approved,
// changes_requested) are never valid targets here: they are reached
// exclusively through submit/approve/changes-requested. The caller
// additionally rejects those targets even for same-state no-ops.
func validateMilestoneStatusTransition(current MilestoneStatus, next MilestoneStatus) error {
	if current == next {
		return nil
	}

	switch current {
	case MilestoneStatusPending:
		if next == MilestoneStatusInProgress || next == MilestoneStatusCancelled {
			return nil
		}
	case MilestoneStatusInProgress:
		if next == MilestoneStatusPending || next == MilestoneStatusBlocked || next == MilestoneStatusCancelled {
			return nil
		}
	case MilestoneStatusBlocked:
		if next == MilestoneStatusInProgress || next == MilestoneStatusCancelled {
			return nil
		}
	case MilestoneStatusAwaitingApproval:
		if next == MilestoneStatusBlocked || next == MilestoneStatusCancelled {
			return nil
		}
	case MilestoneStatusChangesRequested:
		if next == MilestoneStatusInProgress || next == MilestoneStatusBlocked || next == MilestoneStatusCancelled {
			return nil
		}
	case MilestoneStatusApproved:
		if next == MilestoneStatusCompleted {
			return nil
		}
	case MilestoneStatusCompleted:
		return apperrors.ErrInvalidStatusTransition
	case MilestoneStatusCancelled:
		return apperrors.ErrInvalidStatusTransition
	}

	return apperrors.ErrInvalidStatusTransition
}
