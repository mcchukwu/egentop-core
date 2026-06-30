package assignment

import (
	"database/sql"

	"github.com/mcchukwu/egentop/internal/activity"
	"github.com/mcchukwu/egentop/internal/audit"
)

type AssignmentService struct {
	DB              *sql.DB
	Repo            *AssignmentRepo
	AuditServie     *audit.AuditService
	ActivityService *activity.ActivityService
}
