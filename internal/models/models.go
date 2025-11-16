// models/models.go
package models

import "time"

// TeamMember представляет участника команды
type TeamMember struct {
    UserID   string `json:"user_id" db:"user_id"`
    Username string `json:"username" db:"username"`
    IsActive bool   `json:"is_active" db:"is_active"`
}

// Team представляет команду с участниками
type Team struct {
    TeamName string       `json:"team_name" db:"team_name"`
    Members  []TeamMember `json:"members" db:"-"`
}

// User представляет пользователя с принадлежностью к команде
type User struct {
    UserID   string `json:"user_id" db:"user_id"`
    Username string `json:"username" db:"username"`
    TeamName string `json:"team_name" db:"team_name"`
    IsActive bool   `json:"is_active" db:"is_active"`
}

// PullRequest представляет PR с полной информацией
type PullRequest struct {
    PullRequestID      string    `json:"pull_request_id" db:"pull_request_id"`
    PullRequestName    string    `json:"pull_request_name" db:"pull_request_name"`
    AuthorID           string    `json:"author_id" db:"author_id"`
    Status             string    `json:"status" db:"status"`
    AssignedReviewers  []string  `json:"assigned_reviewers" db:"-"`
    CreatedAt          *time.Time `json:"createdAt,omitempty" db:"created_at"`
    MergedAt           *time.Time `json:"mergedAt,omitempty" db:"merged_at"`
}

// PullRequestShort представляет краткую информацию о PR
type PullRequestShort struct {
    PullRequestID   string `json:"pull_request_id" db:"pull_request_id"`
    PullRequestName string `json:"pull_request_name" db:"pull_request_name"`
    AuthorID        string `json:"author_id" db:"author_id"`
    Status          string `json:"status" db:"status"`
}

// Константы статусов PR
const (
    StatusOpen   = "OPEN"
    StatusMerged = "MERGED"
)
