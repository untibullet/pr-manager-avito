// repository/repository.go
package repository

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/untibullet/pr-manager-avito/internal/models"
)

var (
	ErrNotFound      = errors.New("not found")
	ErrAlreadyMerged = errors.New("PR already merged")
	ErrAlreadyExists = errors.New("resource already exists")
	ErrInvalidInput  = errors.New("invalid input")
)

type Repository struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

// CreateUser создает нового пользователя
func (r *Repository) CreateUser(ctx context.Context, username string, isActive bool) (*models.User, error) {
	var userID int64
	query := `INSERT INTO users (name, is_active) VALUES ($1, $2) RETURNING id`

	err := r.pool.QueryRow(ctx, query, username, isActive).Scan(&userID)
	if err != nil {
		return nil, fmt.Errorf("failed to create user: %w", err)
	}

	return &models.User{
		UserID:   strconv.FormatInt(userID, 10),
		Username: username,
		IsActive: isActive,
	}, nil
}

// GetUser получает пользователя по ID с информацией о команде
func (r *Repository) GetUser(ctx context.Context, userID string) (*models.User, error) {
	id, err := strconv.ParseInt(userID, 10, 64)
	if err != nil {
		return nil, ErrInvalidInput
	}

	user := &models.User{}
	query := `
        SELECT u.id, u.name, u.is_active, COALESCE(t.name, '') as team_name
        FROM users u
        LEFT JOIN team_users tu ON u.id = tu.user_id
        LEFT JOIN teams t ON tu.team_id = t.id
        WHERE u.id = $1
        LIMIT 1
    `

	var id64 int64
	var teamName string
	err = r.pool.QueryRow(ctx, query, id).Scan(&id64, &user.Username, &user.IsActive, &teamName)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}

	user.UserID = strconv.FormatInt(id64, 10)
	user.TeamName = teamName

	return user, nil
}

// UpdateUserStatus обновляет статус активности пользователя
func (r *Repository) UpdateUserStatus(ctx context.Context, userID string, isActive bool) error {
	id, err := strconv.ParseInt(userID, 10, 64)
	if err != nil {
		return ErrInvalidInput
	}

	query := `UPDATE users SET is_active = $1, updated_at = NOW() WHERE id = $2`
	tag, err := r.pool.Exec(ctx, query, isActive, id)
	if err != nil {
		return fmt.Errorf("failed to update user status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// CreateTeam создает новую команду
func (r *Repository) CreateTeam(ctx context.Context, teamName string) (*models.Team, error) {
	query := `INSERT INTO teams (name) VALUES ($1) RETURNING id`

	var teamID int64
	err := r.pool.QueryRow(ctx, query, teamName).Scan(&teamID)
	if err != nil {
		return nil, fmt.Errorf("failed to create team: %w", err)
	}

	return &models.Team{
		TeamName: teamName,
		Members:  []models.TeamMember{},
	}, nil
}

// GetTeam получает команду по ID
func (r *Repository) GetTeam(ctx context.Context, teamID string) (*models.Team, error) {
	id, err := strconv.ParseInt(teamID, 10, 64)
	if err != nil {
		return nil, ErrInvalidInput
	}

	// Проверяем существование команды и получаем название
	var teamName string
	query := `SELECT name FROM teams WHERE id = $1`
	err = r.pool.QueryRow(ctx, query, id).Scan(&teamName)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get team: %w", err)
	}

	// Получаем всех участников команды
	membersQuery := `
        SELECT u.id, u.name, u.is_active
        FROM users u
        JOIN team_users tu ON u.id = tu.user_id
        WHERE tu.team_id = $1
        ORDER BY u.name
    `

	rows, err := r.pool.Query(ctx, membersQuery, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get team members: %w", err)
	}
	defer rows.Close()

	var members []models.TeamMember
	for rows.Next() {
		var userID int64
		var username string
		var isActive bool

		if err := rows.Scan(&userID, &username, &isActive); err != nil {
			return nil, fmt.Errorf("failed to scan team member: %w", err)
		}

		members = append(members, models.TeamMember{
			UserID:   strconv.FormatInt(userID, 10),
			Username: username,
			IsActive: isActive,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate team members: %w", err)
	}

	return &models.Team{
		TeamName: teamName,
		Members:  members,
	}, nil
}

// AddUserToTeam добавляет пользователя в команду
func (r *Repository) AddUserToTeam(ctx context.Context, teamID, userID string) error {
	tID, err := strconv.ParseInt(teamID, 10, 64)
	if err != nil {
		return ErrInvalidInput
	}
	uID, err := strconv.ParseInt(userID, 10, 64)
	if err != nil {
		return ErrInvalidInput
	}

	// Проверяем существование команды и пользователя
	var exists bool
	checkQuery := `
        SELECT EXISTS(SELECT 1 FROM teams WHERE id = $1) AND
               EXISTS(SELECT 1 FROM users WHERE id = $2)
    `
	err = r.pool.QueryRow(ctx, checkQuery, tID, uID).Scan(&exists)
	if err != nil {
		return fmt.Errorf("failed to check existence: %w", err)
	}
	if !exists {
		return ErrNotFound
	}

	query := `INSERT INTO team_users (team_id, user_id) VALUES ($1, $2) 
              ON CONFLICT (team_id, user_id) DO NOTHING`
	_, err = r.pool.Exec(ctx, query, tID, uID)
	if err != nil {
		return fmt.Errorf("failed to add user to team: %w", err)
	}

	return nil
}

// RemoveUserFromTeam удаляет пользователя из команды
func (r *Repository) RemoveUserFromTeam(ctx context.Context, teamID, userID string) error {
	tID, err := strconv.ParseInt(teamID, 10, 64)
	if err != nil {
		return ErrInvalidInput
	}
	uID, err := strconv.ParseInt(userID, 10, 64)
	if err != nil {
		return ErrInvalidInput
	}

	query := `DELETE FROM team_users WHERE team_id = $1 AND user_id = $2`
	tag, err := r.pool.Exec(ctx, query, tID, uID)
	if err != nil {
		return fmt.Errorf("failed to remove user from team: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}

// GetActiveTeamMembers получает активных участников команды (исключая authorID)
func (r *Repository) GetActiveTeamMembers(ctx context.Context, teamID, excludeUserID string) ([]string, error) {
	tID, err := strconv.ParseInt(teamID, 10, 64)
	if err != nil {
		return nil, ErrInvalidInput
	}
	eID, err := strconv.ParseInt(excludeUserID, 10, 64)
	if err != nil {
		return nil, ErrInvalidInput
	}

	query := `
        SELECT tu.user_id 
        FROM team_users tu
        JOIN users u ON tu.user_id = u.id
        WHERE tu.team_id = $1 AND u.is_active = true AND tu.user_id != $2
    `

	rows, err := r.pool.Query(ctx, query, tID, eID)
	if err != nil {
		return nil, fmt.Errorf("failed to get active team members: %w", err)
	}
	defer rows.Close()

	var members []string
	for rows.Next() {
		var userID int64
		if err := rows.Scan(&userID); err != nil {
			return nil, fmt.Errorf("failed to scan user id: %w", err)
		}
		members = append(members, strconv.FormatInt(userID, 10))
	}

	return members, rows.Err()
}

// CreatePR создает новый PR и автоматически назначает до 2 ревьюеров из команды автора
func (r *Repository) CreatePR(ctx context.Context, pullRequestID, pullRequestName, authorID string) (*models.PullRequest, error) {
	aID, err := strconv.ParseInt(authorID, 10, 64)
	if err != nil {
		return nil, ErrInvalidInput
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// Проверяем, существует ли PR с таким внешним ID (для 409 Conflict)
	var exists bool
	checkQuery := `SELECT EXISTS(SELECT 1 FROM pull_requests WHERE external_id = $1)`
	err = tx.QueryRow(ctx, checkQuery, pullRequestID).Scan(&exists)
	if err != nil {
		return nil, fmt.Errorf("failed to check PR existence: %w", err)
	}
	if exists {
		return nil, ErrAlreadyExists
	}

	// Находим команду автора
	var teamID int64
	teamQuery := `SELECT team_id FROM team_users WHERE user_id = $1 LIMIT 1`
	err = tx.QueryRow(ctx, teamQuery, aID).Scan(&teamID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound // Автор не в команде
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get author team: %w", err)
	}

	// Получаем активных участников команды (исключая автора)
	candidatesQuery := `
        SELECT tu.user_id
        FROM team_users tu
        JOIN users u ON tu.user_id = u.id
        WHERE tu.team_id = $1 
          AND u.is_active = true 
          AND tu.user_id != $2
        ORDER BY RANDOM()
        LIMIT 2
    `

	rows, err := tx.Query(ctx, candidatesQuery, teamID, aID)
	if err != nil {
		return nil, fmt.Errorf("failed to get reviewer candidates: %w", err)
	}

	var reviewerIDs []int64
	for rows.Next() {
		var rID int64
		if err := rows.Scan(&rID); err != nil {
			rows.Close()
			return nil, fmt.Errorf("failed to scan reviewer: %w", err)
		}
		reviewerIDs = append(reviewerIDs, rID)
	}
	rows.Close()

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate reviewers: %w", err)
	}

	// Создаем PR (внутренний ID генерируется БД, внешний задан клиентом)
	var internalID int64
	var createdAt time.Time
	insertQuery := `
        INSERT INTO pull_requests (external_id, title, author_id, status) 
        VALUES ($1, $2, $3, $4) 
        RETURNING id, created_at
    `

	err = tx.QueryRow(ctx, insertQuery, pullRequestID, pullRequestName, aID, models.StatusOpen).Scan(&internalID, &createdAt)
	if err != nil {
		return nil, fmt.Errorf("failed to create PR: %w", err)
	}

	// Назначаем найденных ревьюеров
	assignedReviewers := []string{}
	for _, rID := range reviewerIDs {
		_, err = tx.Exec(ctx,
			`INSERT INTO pr_reviewers (pr_id, reviewer_id) VALUES ($1, $2)`,
			internalID, rID)
		if err != nil {
			return nil, fmt.Errorf("failed to assign reviewer: %w", err)
		}
		assignedReviewers = append(assignedReviewers, strconv.FormatInt(rID, 10))
	}

	if err = tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	pr := &models.PullRequest{
		PullRequestID:     pullRequestID, // Внешний ID от клиента
		PullRequestName:   pullRequestName,
		AuthorID:          authorID,
		Status:            models.StatusOpen,
		AssignedReviewers: assignedReviewers, // 0-2 ревьюера
		CreatedAt:         &createdAt,
	}

	return pr, nil
}

// GetPR получает PR по ID с ревьюерами
func (r *Repository) GetPR(ctx context.Context, prID string) (*models.PullRequest, error) {
	id, err := strconv.ParseInt(prID, 10, 64)
	if err != nil {
		return nil, ErrInvalidInput
	}

	pr := &models.PullRequest{}
	query := `SELECT id, title, author_id, status, created_at, updated_at 
              FROM pull_requests WHERE id = $1`

	var id64, authorID64 int64
	var createdAt, updatedAt time.Time
	err = r.pool.QueryRow(ctx, query, id).Scan(
		&id64, &pr.PullRequestName, &authorID64, &pr.Status, &createdAt, &updatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get PR: %w", err)
	}

	pr.PullRequestID = strconv.FormatInt(id64, 10)
	pr.AuthorID = strconv.FormatInt(authorID64, 10)

	// Получаем ревьюеров
	reviewers, err := r.getPRReviewers(ctx, id)
	if err != nil {
		return nil, err
	}
	pr.AssignedReviewers = reviewers

	return pr, nil
}

// GetPRReviewers получает список ревьюеров для PR
func (r *Repository) getPRReviewers(ctx context.Context, prID int64) ([]string, error) {
	query := `SELECT reviewer_id FROM pr_reviewers WHERE pr_id = $1`
	rows, err := r.pool.Query(ctx, query, prID)
	if err != nil {
		return nil, fmt.Errorf("failed to get reviewers: %w", err)
	}
	defer rows.Close()

	var reviewers []string
	for rows.Next() {
		var reviewerID int64
		if err := rows.Scan(&reviewerID); err != nil {
			return nil, fmt.Errorf("failed to scan reviewer id: %w", err)
		}
		reviewers = append(reviewers, strconv.FormatInt(reviewerID, 10))
	}

	return reviewers, rows.Err()
}

// MergePR переводит PR в статус MERGED (идемпотентно)
func (r *Repository) MergePR(ctx context.Context, prID string) (*models.PullRequest, error) {
	id, err := strconv.ParseInt(prID, 10, 64)
	if err != nil {
		return nil, ErrInvalidInput
	}

	pr := &models.PullRequest{}
	query := `UPDATE pull_requests SET status = $1, updated_at = NOW() 
              WHERE id = $2
              RETURNING id, title, author_id, status, created_at, updated_at`

	var prID64, authorID64 int64
	var createdAt, updatedAt time.Time
	err = r.pool.QueryRow(ctx, query, models.StatusMerged, id).Scan(
		&prID64, &pr.PullRequestName, &authorID64, &pr.Status, &createdAt, &updatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to merge PR: %w", err)
	}

	pr.PullRequestID = strconv.FormatInt(prID64, 10)
	pr.AuthorID = strconv.FormatInt(authorID64, 10)

	// Получаем ревьюеров
	reviewers, err := r.getPRReviewers(ctx, id)
	if err != nil {
		return nil, err
	}
	pr.AssignedReviewers = reviewers

	return pr, nil
}

// ReassignReviewer переназначает ревьюера
func (r *Repository) ReassignReviewer(ctx context.Context, prID, oldReviewerID, newReviewerID string) error {
	pID, err := strconv.ParseInt(prID, 10, 64)
	if err != nil {
		return ErrInvalidInput
	}
	oID, err := strconv.ParseInt(oldReviewerID, 10, 64)
	if err != nil {
		return ErrInvalidInput
	}
	nID, err := strconv.ParseInt(newReviewerID, 10, 64)
	if err != nil {
		return ErrInvalidInput
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// Проверяем статус PR
	var status string
	err = tx.QueryRow(ctx, `SELECT status FROM pull_requests WHERE id = $1`, pID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("failed to check PR status: %w", err)
	}
	if status == models.StatusMerged {
		return ErrAlreadyMerged
	}

	// Удаляем старого ревьюера
	tag, err := tx.Exec(ctx, `DELETE FROM pr_reviewers WHERE pr_id = $1 AND reviewer_id = $2`,
		pID, oID)
	if err != nil {
		return fmt.Errorf("failed to remove old reviewer: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}

	// Добавляем нового ревьюера
	_, err = tx.Exec(ctx, `INSERT INTO pr_reviewers (pr_id, reviewer_id) VALUES ($1, $2)
                           ON CONFLICT DO NOTHING`,
		pID, nID)
	if err != nil {
		return fmt.Errorf("failed to add new reviewer: %w", err)
	}

	return tx.Commit(ctx)
}

// GetUserTeams получает все команды пользователя
func (r *Repository) GetUserTeams(ctx context.Context, userID string) ([]string, error) {
	uID, err := strconv.ParseInt(userID, 10, 64)
	if err != nil {
		return nil, ErrInvalidInput
	}

	query := `SELECT team_id FROM team_users WHERE user_id = $1`
	rows, err := r.pool.Query(ctx, query, uID)
	if err != nil {
		return nil, fmt.Errorf("failed to get user teams: %w", err)
	}
	defer rows.Close()

	var teams []string
	for rows.Next() {
		var teamID int64
		if err := rows.Scan(&teamID); err != nil {
			return nil, fmt.Errorf("failed to scan team id: %w", err)
		}
		teams = append(teams, strconv.FormatInt(teamID, 10))
	}

	return teams, rows.Err()
}

// GetPRsByReviewer получает все PR для указанного ревьюера
func (r *Repository) GetPRsByReviewer(ctx context.Context, reviewerID string) ([]models.PullRequestShort, error) {
	rID, err := strconv.ParseInt(reviewerID, 10, 64)
	if err != nil {
		return nil, ErrInvalidInput
	}

	query := `
        SELECT DISTINCT pr.id, pr.title, pr.author_id, pr.status
        FROM pull_requests pr
        JOIN pr_reviewers prr ON pr.id = prr.pr_id
        WHERE prr.reviewer_id = $1
        ORDER BY pr.created_at DESC
    `

	rows, err := r.pool.Query(ctx, query, rID)
	if err != nil {
		return nil, fmt.Errorf("failed to get PRs by reviewer: %w", err)
	}
	defer rows.Close()

	var prs []models.PullRequestShort
	for rows.Next() {
		var pr models.PullRequestShort
		var prID, authorID int64

		if err := rows.Scan(&prID, &pr.PullRequestName, &authorID, &pr.Status); err != nil {
			return nil, fmt.Errorf("failed to scan PR: %w", err)
		}

		pr.PullRequestID = strconv.FormatInt(prID, 10)
		pr.AuthorID = strconv.FormatInt(authorID, 10)
		prs = append(prs, pr)
	}

	return prs, rows.Err()
}
