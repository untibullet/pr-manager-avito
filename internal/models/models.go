// models/models.go
package models

import "time"

type User struct {
    ID        int64     `json:"id" db:"id"`
    Name      string    `json:"name" db:"name"`
    IsActive  bool      `json:"isActive" db:"is_active"`
    CreatedAt time.Time `json:"createdAt" db:"created_at"`
    UpdatedAt time.Time `json:"updatedAt" db:"updated_at"`
}

type Team struct {
    ID        int64     `json:"id" db:"id"`
    Name      string    `json:"name" db:"name"`
    CreatedAt time.Time `json:"createdAt" db:"created_at"`
    UpdatedAt time.Time `json:"updatedAt" db:"updated_at"`
}

type PullRequest struct {
    ID        int64     `json:"id" db:"id"`
    Title     string    `json:"title" db:"title"`
    AuthorID  int64     `json:"authorId" db:"author_id"`
    Status    string    `json:"status" db:"status"`
    CreatedAt time.Time `json:"createdAt" db:"created_at"`
    UpdatedAt time.Time `json:"updatedAt" db:"updated_at"`
    Reviewers []int64   `json:"reviewers,omitempty" db:"-"`
}

const (
    StatusOpen   = "OPEN"
    StatusMerged = "MERGED"
)
