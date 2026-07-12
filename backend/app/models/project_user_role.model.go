package models

import (
	"time"

	"github.com/google/uuid"
)

type ProjectUserRole struct {
	Id        int       `json:"id"`
	ProjectId uuid.UUID `json:"projectId"`
	UserId    int       `json:"userId"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"createdAt"`
}

type MemberProjectRole struct {
	ProjectId uuid.UUID `json:"projectId" lit:"project_id"`
	Name      string    `json:"name" lit:"name"`
	Framework string    `json:"framework" lit:"framework"`
	Role      *string   `json:"role" lit:"role"`
}
