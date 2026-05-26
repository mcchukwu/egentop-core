package project

import (
	"context"
	"database/sql"
	"time"

	"github.com/mcchukwu/egentop/pkg/db"
)

type ProjectRepository struct {
	DB *sql.DB
}

func NewProjectRepository(db *sql.DB) *ProjectRepository {
	return &ProjectRepository{DB: db}
}

func (r *ProjectRepository) Create(ctx context.Context, project *Project) error {
	err := db.WithTransaction(ctx, r.DB, func(tx *sql.Tx) error {
		dbCtx, cancel := db.WithDBTimeout(ctx)
		defer cancel()

		query := `
		INSERT INTO projects (name, description, status, priority, due_date, created_by, organization_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, created_at, updated_at
	`

		err := tx.QueryRowContext(dbCtx, query, project.Name, project.Description, project.Status, project.Priority, project.DueDate, project.CreatedBy, project.OrganizationID).Scan(&project.ID, &project.CreatedAt, &project.UpdatedAt)
		if err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		return err
	}

	return nil
}

func (r *ProjectRepository) ListByOrganization(ctx context.Context, organizationID string) ([]Project, error) {
	var projects []Project

	err := db.WithTransaction(ctx, r.DB, func(tx *sql.Tx) error {
		dbCtx, cancel := db.WithDBTimeout(ctx)
		defer cancel()

		query := `
		SELECT
			id,
			organization_id,
			created_by,
			name,
			description,
			status,
			priority,
			due_date,
			created_at,
			updated_at
		FROM projects
		WHERE organization_id = $1
		ORDER BY created_at DESC
	`

		rows, err := tx.QueryContext(dbCtx, query, organizationID)
		if err != nil {
			return err
		}

		defer rows.Close()

		for rows.Next() {
			var p Project

			err := rows.Scan(&p.ID, &p.OrganizationID, &p.CreatedBy, &p.Name, &p.Description, &p.Status, &p.Priority, &p.DueDate, &p.CreatedAt, &p.UpdatedAt)
			if err != nil {
				return err
			}

			projects = append(projects, p)
		}
		if err := rows.Err(); err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return projects, nil
}

func (r *ProjectRepository) UpdateStatus(ctx context.Context, projectID string, status Status) error {
	err := db.WithTransaction(ctx, r.DB, func(tx *sql.Tx) error {
		dbCtx, cancel := db.WithDBTimeout(ctx)
		defer cancel()

		query := `
		UPDATE projects
		SET
			status = $1,
			updated_at = $2
		WHERE id = $3
	`
		_, err := tx.ExecContext(dbCtx, query, status, time.Now().UTC(), projectID)
		if err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		return err
	}

	return nil
}
