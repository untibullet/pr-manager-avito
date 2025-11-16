// repository/repository.go
package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/untibullet/pr-manager-avito/internal/models"
)

var (
	ErrNotFound      = errors.New("not found")
	ErrAlreadyMerged = errors.New("PR already merged")
)

type Repository struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

// CreateUser создает нового пользователя
func (r *Repository) CreateUser(ctx context.Context, name string, isActive bool) (*models.User, error) {
	user := &models.User{}
	query := `INSERT INTO users (name, is_active) VALUES ($1, $2) 
              RETURNING id, name, is_active, created_at, updated_at`

	err := r.pool.QueryRow(ctx, query, name, isActive).Scan(
		&user.ID, &user.Name, &user.IsActive, &user.CreatedAt, &user.UpdatedAt,
	)
	return user, err
}

// GetUser получает пользователя по ID
func (r *Repository) GetUser(ctx context.Context, id int64) (*models.User, error) {
	user := &models.User{}
	query := `SELECT id, name, is_active, created_at, updated_at FROM users WHERE id = $1`

	err := r.pool.QueryRow(ctx, query, id).Scan(
		&user.ID, &user.Name, &user.IsActive, &user.CreatedAt, &user.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return user, err
}

// UpdateUserStatus обновляет статус активности пользователя
func (r *Repository) UpdateUserStatus(ctx context.Context, id int64, isActive bool) error {
	query := `UPDATE users SET is_active = $1, updated_at = NOW() WHERE id = $2`
	tag, err := r.pool.Exec(ctx, query, isActive, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// CreateTeam создает новую команду
func (r *Repository) CreateTeam(ctx context.Context, name string) (*models.Team, error) {
	team := &models.Team{}
	query := `INSERT INTO teams (name) VALUES ($1) 
              RETURNING id, name, created_at, updated_at`

	err := r.pool.QueryRow(ctx, query, name).Scan(
		&team.ID, &team.Name, &team.CreatedAt, &team.UpdatedAt,
	)
	return team, err
}

// GetTeam получает команду по ID
func (r *Repository) GetTeam(ctx context.Context, id int64) (*models.Team, error) {
	team := &models.Team{}
	query := `SELECT id, name, created_at, updated_at FROM teams WHERE id = $1`

	err := r.pool.QueryRow(ctx, query, id).Scan(
		&team.ID, &team.Name, &team.CreatedAt, &team.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return team, err
}

// AddUserToTeam добавляет пользователя в команду
func (r *Repository) AddUserToTeam(ctx context.Context, teamID, userID int64) error {
	query := `INSERT INTO team_users (team_id, user_id) VALUES ($1, $2) 
              ON CONFLICT (team_id, user_id) DO NOTHING`
	_, err := r.pool.Exec(ctx, query, teamID, userID)
	return err
}

// RemoveUserFromTeam удаляет пользователя из команды
func (r *Repository) RemoveUserFromTeam(ctx context.Context, teamID, userID int64) error {
	query := `DELETE FROM team_users WHERE team_id = $1 AND user_id = $2`
	tag, err := r.pool.Exec(ctx, query, teamID, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// GetTeamMembers получает всех участников команды
func (r *Repository) GetTeamMembers(ctx context.Context, teamID int64) ([]int64, error) {
	query := `SELECT user_id FROM team_users WHERE team_id = $1`
	rows, err := r.pool.Query(ctx, query, teamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var members []int64
	for rows.Next() {
		var userID int64
		if err := rows.Scan(&userID); err != nil {
			return nil, err
		}
		members = append(members, userID)
	}
	return members, rows.Err()
}

// GetActiveTeamMembers получает активных участников команды (исключая authorID)
func (r *Repository) GetActiveTeamMembers(ctx context.Context, teamID, excludeUserID int64) ([]int64, error) {
	query := `SELECT tu.user_id FROM team_users tu
              JOIN users u ON tu.user_id = u.id
              WHERE tu.team_id = $1 AND u.is_active = true AND tu.user_id != $2`

	rows, err := r.pool.Query(ctx, query, teamID, excludeUserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var members []int64
	for rows.Next() {
		var userID int64
		if err := rows.Scan(&userID); err != nil {
			return nil, err
		}
		members = append(members, userID)
	}
	return members, rows.Err()
}

// CreatePR создает новый PR с назначением ревьюеров
func (r *Repository) CreatePR(ctx context.Context, title string, authorID int64, reviewers []int64) (*models.PullRequest, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	pr := &models.PullRequest{}
	query := `INSERT INTO pull_requests (title, author_id, status) 
              VALUES ($1, $2, $3) 
              RETURNING id, title, author_id, status, created_at, updated_at`

	err = tx.QueryRow(ctx, query, title, authorID, models.StatusOpen).Scan(
		&pr.ID, &pr.Title, &pr.AuthorID, &pr.Status, &pr.CreatedAt, &pr.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	// Назначаем ревьюеров
	for _, reviewerID := range reviewers {
		_, err = tx.Exec(ctx, `INSERT INTO pr_reviewers (pr_id, reviewer_id) VALUES ($1, $2)`,
			pr.ID, reviewerID)
		if err != nil {
			return nil, err
		}
	}

	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}

	pr.Reviewers = reviewers
	return pr, nil
}

// GetPR получает PR по ID с ревьюерами
func (r *Repository) GetPR(ctx context.Context, id int64) (*models.PullRequest, error) {
	pr := &models.PullRequest{}
	query := `SELECT id, title, author_id, status, created_at, updated_at 
              FROM pull_requests WHERE id = $1`

	err := r.pool.QueryRow(ctx, query, id).Scan(
		&pr.ID, &pr.Title, &pr.AuthorID, &pr.Status, &pr.CreatedAt, &pr.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	// Получаем ревьюеров
	reviewers, err := r.GetPRReviewers(ctx, id)
	if err != nil {
		return nil, err
	}
	pr.Reviewers = reviewers

	return pr, nil
}

// GetPRReviewers получает список ревьюеров для PR
func (r *Repository) GetPRReviewers(ctx context.Context, prID int64) ([]int64, error) {
	query := `SELECT reviewer_id FROM pr_reviewers WHERE pr_id = $1`
	rows, err := r.pool.Query(ctx, query, prID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var reviewers []int64
	for rows.Next() {
		var reviewerID int64
		if err := rows.Scan(&reviewerID); err != nil {
			return nil, err
		}
		reviewers = append(reviewers, reviewerID)
	}
	return reviewers, rows.Err()
}

// GetPRsByReviewer получает все PR для указанного ревьюера
func (r *Repository) GetPRsByReviewer(ctx context.Context, reviewerID int64) ([]*models.PullRequest, error) {
	query := `SELECT DISTINCT pr.id, pr.title, pr.author_id, pr.status, pr.created_at, pr.updated_at
              FROM pull_requests pr
              JOIN pr_reviewers prr ON pr.id = prr.pr_id
              WHERE prr.reviewer_id = $1
              ORDER BY pr.created_at DESC`

	rows, err := r.pool.Query(ctx, query, reviewerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var prs []*models.PullRequest
	for rows.Next() {
		pr := &models.PullRequest{}
		if err := rows.Scan(&pr.ID, &pr.Title, &pr.AuthorID, &pr.Status, &pr.CreatedAt, &pr.UpdatedAt); err != nil {
			return nil, err
		}

		// Получаем ревьюеров для каждого PR
		reviewers, err := r.GetPRReviewers(ctx, pr.ID)
		if err != nil {
			return nil, err
		}
		pr.Reviewers = reviewers
		prs = append(prs, pr)
	}

	return prs, rows.Err()
}

// ReassignReviewer переназначает ревьюера
func (r *Repository) ReassignReviewer(ctx context.Context, prID, oldReviewerID, newReviewerID int64) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// Проверяем статус PR
	var status string
	err = tx.QueryRow(ctx, `SELECT status FROM pull_requests WHERE id = $1`, prID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if status == models.StatusMerged {
		return ErrAlreadyMerged
	}

	// Удаляем старого ревьюера
	_, err = tx.Exec(ctx, `DELETE FROM pr_reviewers WHERE pr_id = $1 AND reviewer_id = $2`,
		prID, oldReviewerID)
	if err != nil {
		return err
	}

	// Добавляем нового ревьюера
	_, err = tx.Exec(ctx, `INSERT INTO pr_reviewers (pr_id, reviewer_id) VALUES ($1, $2)`,
		prID, newReviewerID)
	if err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// MergePR переводит PR в статус MERGED (идемпотентно)
func (r *Repository) MergePR(ctx context.Context, prID int64) (*models.PullRequest, error) {
	pr := &models.PullRequest{}
	query := `UPDATE pull_requests SET status = $1, updated_at = NOW() 
              WHERE id = $2
              RETURNING id, title, author_id, status, created_at, updated_at`

	err := r.pool.QueryRow(ctx, query, models.StatusMerged, prID).Scan(
		&pr.ID, &pr.Title, &pr.AuthorID, &pr.Status, &pr.CreatedAt, &pr.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	// Получаем ревьюеров
	reviewers, err := r.GetPRReviewers(ctx, prID)
	if err != nil {
		return nil, err
	}
	pr.Reviewers = reviewers

	return pr, nil
}

// GetUserTeams получает все команды пользователя
func (r *Repository) GetUserTeams(ctx context.Context, userID int64) ([]int64, error) {
	query := `SELECT team_id FROM team_users WHERE user_id = $1`
	rows, err := r.pool.Query(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var teams []int64
	for rows.Next() {
		var teamID int64
		if err := rows.Scan(&teamID); err != nil {
			return nil, err
		}
		teams = append(teams, teamID)
	}
	return teams, rows.Err()
}
