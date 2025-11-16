// repository/repository.go
package repository

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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

// UpdateUserStatus обновляет статус активности пользователя по внешнему ID
func (r *Repository) UpdateUserStatus(ctx context.Context, userID string, isActive bool) error {
	query := `UPDATE users SET is_active = $1, updated_at = NOW() WHERE external_id = $2`
	tag, err := r.pool.Exec(ctx, query, isActive, userID)
	if err != nil {
		return fmt.Errorf("failed to update user status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// CreateTeam создает или обновляет команду и ее участников
func (r *Repository) CreateTeam(ctx context.Context, teamData models.Team) (*models.Team, error) {
    tx, err := r.pool.Begin(ctx)
    if err != nil {
        return nil, fmt.Errorf("failed to begin transaction: %w", err)
    }
    defer tx.Rollback(ctx)

    // Создаем или получаем ID существующей команды
    var teamID int64
    teamUpsertQuery := `
        INSERT INTO teams (name) VALUES ($1)
        ON CONFLICT (name) DO UPDATE SET updated_at = NOW()
        RETURNING id
    `
    err = tx.QueryRow(ctx, teamUpsertQuery, teamData.TeamName).Scan(&teamID)
    if err != nil {
        return nil, fmt.Errorf("failed to upsert team: %w", err)
    }

    // Готовим данные для массового "upsert" пользователей
    userExternalIDs := make([]string, len(teamData.Members))
    userNames := make([]string, len(teamData.Members))
    userIsActive := make([]bool, len(teamData.Members))
    for i, member := range teamData.Members {
        userExternalIDs[i] = member.UserID
        userNames[i] = member.Username
        userIsActive[i] = member.IsActive
    }

    // Массово создаем или обновляем всех пользователей одним запросом
    userUpsertQuery := `
        INSERT INTO users (external_id, name, is_active)
        SELECT * FROM unnest($1::varchar[], $2::varchar[], $3::boolean[])
        ON CONFLICT (external_id) DO UPDATE
        SET name = excluded.name, is_active = excluded.is_active, updated_at = NOW()
        RETURNING id, external_id
    `
    rows, err := tx.Query(ctx, userUpsertQuery, userExternalIDs, userNames, userIsActive)
    if err != nil {
        return nil, fmt.Errorf("failed to upsert users: %w", err)
    }
    
    // Собираем мапу "внешний ID" -> "внутренний ID" для дальнейшей работы
    userInternalIDs := make(map[string]int64, len(teamData.Members))
    for rows.Next() {
        var internalID int64
        var externalID string
        if err := rows.Scan(&internalID, &externalID); err != nil {
            rows.Close()
            return nil, fmt.Errorf("failed to scan upserted user: %w", err)
        }
        userInternalIDs[externalID] = internalID
    }
    rows.Close()

    // Очищаем старый состав команды
    _, err = tx.Exec(ctx, "DELETE FROM team_users WHERE team_id = $1", teamID)
    if err != nil {
        return nil, fmt.Errorf("failed to clear old team members: %w", err)
    }
    
    // Добавляем новый состав
    newMembers := make([][]interface{}, 0, len(teamData.Members))
    for _, member := range teamData.Members {
        internalID, ok := userInternalIDs[member.UserID]
        if ok {
            newMembers = append(newMembers, []interface{}{teamID, internalID})
        }
    }
    
    _, err = tx.CopyFrom(
        ctx,
        pgx.Identifier{"team_users"},
        []string{"team_id", "user_id"},
        pgx.CopyFromRows(newMembers),
    )
    if err != nil {
        return nil, fmt.Errorf("failed to copy new members: %w", err)
    }

    if err = tx.Commit(ctx); err != nil {
        return nil, fmt.Errorf("failed to commit transaction: %w", err)
    }
    
    return &teamData, nil
}

// GetTeam получает команду по ее имени со списком всех участников
func (r *Repository) GetTeam(ctx context.Context, teamName string) (*models.Team, error) {
    // Находим команду по имени
    var teamID int64
    err := r.pool.QueryRow(ctx, "SELECT id FROM teams WHERE name = $1", teamName).Scan(&teamID)
    if errors.Is(err, pgx.ErrNoRows) {
        return nil, ErrNotFound
    }
    if err != nil {
        return nil, fmt.Errorf("failed to get team by name: %w", err)
    }

    // Получаем всех участников команды
    query := `
        SELECT u.external_id, u.name, u.is_active
        FROM users u
        JOIN team_users tu ON u.id = tu.user_id
        WHERE tu.team_id = $1
        ORDER BY u.name
    `
    rows, err := r.pool.Query(ctx, query, teamID)
    if err != nil {
        return nil, fmt.Errorf("failed to get team members: %w", err)
    }
    defer rows.Close()

    var members []models.TeamMember
    for rows.Next() {
        var member models.TeamMember
        if err := rows.Scan(&member.UserID, &member.Username, &member.IsActive); err != nil {
            return nil, fmt.Errorf("failed to scan team member: %w", err)
        }
        members = append(members, member)
    }
    if err := rows.Err(); err != nil {
        return nil, fmt.Errorf("failed to iterate team members: %w", err)
    }

    return &models.Team{
        TeamName: teamName,
        Members:  members,
    }, nil
}

// CreatePR создает новый PR и автоматически назначает до 2 ревьюеров из команды автора.
// Метод идемпотентен: при повторном вызове с тем же pullRequestID вернет ошибку ErrAlreadyExists.
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

	// Проверка на существование PR с таким внешним ID (для 409 Conflict)
	var exists bool
	checkQuery := `SELECT EXISTS(SELECT 1 FROM pull_requests WHERE external_id = $1)`
	err = tx.QueryRow(ctx, checkQuery, pullRequestID).Scan(&exists)
	if err != nil {
		return nil, fmt.Errorf("failed to check PR existence: %w", err)
	}
	if exists {
		return nil, ErrAlreadyExists
	}

	// Определение команды автора для поиска ревьюеров
	var teamID int64
	teamQuery := `SELECT team_id FROM team_users WHERE user_id = $1 LIMIT 1`
	err = tx.QueryRow(ctx, teamQuery, aID).Scan(&teamID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get author's team: %w", err)
	}

	// Выбор до 2-х случайных активных ревьюеров из команды, исключая автора
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

	// Создание основной записи о PR в базе данных
	var internalID int64
	var createdAt time.Time
	insertQuery := `
        INSERT INTO pull_requests (external_id, title, author_id, status) 
        VALUES ($1, $2, $3, $4) 
        RETURNING id, created_at
    `
	err = tx.QueryRow(ctx, insertQuery, pullRequestID, pullRequestName, aID, models.StatusOpen).Scan(&internalID, &createdAt)
	if err != nil {
		// Обработка возможного race condition
		if pgxErr, ok := err.(*pgconn.PgError); ok && pgxErr.Code == "23505" {
			return nil, ErrAlreadyExists
		}
		return nil, fmt.Errorf("failed to create PR: %w", err)
	}

	// Привязка найденных ревьюеров к созданному PR
	assignedReviewers := make([]string, 0, len(reviewerIDs))
	for _, rID := range reviewerIDs {
		_, err = tx.Exec(ctx, `INSERT INTO pr_reviewers (pr_id, reviewer_id) VALUES ($1, $2)`, internalID, rID)
		if err != nil {
			return nil, fmt.Errorf("failed to assign reviewer: %w", err)
		}
		assignedReviewers = append(assignedReviewers, strconv.FormatInt(rID, 10))
	}

	if err = tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	pr := &models.PullRequest{
		PullRequestID:     pullRequestID,
		PullRequestName:   pullRequestName,
		AuthorID:          authorID,
		Status:            models.StatusOpen,
		AssignedReviewers: assignedReviewers,
		CreatedAt:         &createdAt,
	}

	return pr, nil
}

// GetPR получает PR по внешнему ID с ревьюерами
func (r *Repository) GetPR(ctx context.Context, pullRequestID string) (*models.PullRequest, error) {
	pr := &models.PullRequest{
		PullRequestID: pullRequestID,
	}

	query := `
        SELECT id, title, author_id, status, created_at, merged_at 
        FROM pull_requests 
        WHERE external_id = $1
    `

	var internalID, authorID64 int64

	err := r.pool.QueryRow(ctx, query, pullRequestID).Scan(
		&internalID, &pr.PullRequestName, &authorID64, &pr.Status, &pr.CreatedAt, &pr.MergedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get PR by external id: %w", err)
	}

	pr.AuthorID = strconv.FormatInt(authorID64, 10)

	// Получаем ревьюеров по внутреннему ID
	reviewers, err := r.getPRReviewers(ctx, internalID)
	if err != nil {
		return nil, err
	}
	pr.AssignedReviewers = reviewers

	return pr, nil
}

// getPRReviewers получает список ревьюеров для PR по внутреннему ID
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

// MergePR переводит PR в статус MERGED по внешнему ID (идемпотентно)
func (r *Repository) MergePR(ctx context.Context, pullRequestID string) (*models.PullRequest, error) {
	pr := &models.PullRequest{
		PullRequestID: pullRequestID,
	}

	query := `
        UPDATE pull_requests 
        SET status = $1, merged_at = NOW() 
        WHERE external_id = $2
        RETURNING id, title, author_id, status, created_at, merged_at
    `

	var internalID, authorID64 int64

	err := r.pool.QueryRow(ctx, query, models.StatusMerged, pullRequestID).Scan(
		&internalID, &pr.PullRequestName, &authorID64, &pr.Status, &pr.CreatedAt, &pr.MergedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		// Если PR не найден, нужно проверить, не был ли он уже смержен
		mergedPR, getErr := r.GetPR(ctx, pullRequestID)
		if getErr != nil {
			return nil, ErrNotFound // Не найден вообще
		}
		if mergedPR.Status == models.StatusMerged {
			return mergedPR, nil // Уже смержен, возвращаем актуальное состояние
		}
		return nil, ErrNotFound // Другая причина, почему не обновился
	}
	if err != nil {
		return nil, fmt.Errorf("failed to merge PR: %w", err)
	}

	pr.AuthorID = strconv.FormatInt(authorID64, 10)

	// Получаем ревьюеров
	reviewers, err := r.getPRReviewers(ctx, internalID)
	if err != nil {
		return nil, err
	}
	pr.AssignedReviewers = reviewers

	return pr, nil
}

// ReassignReviewer переназначает ревьюера
func (r *Repository) ReassignReviewerAuto(ctx context.Context, pullRequestID, oldReviewerID string) (string, error) {
	// Получаем внутренний ID старого ревьюера
	var rInternalID int64
	usersQuery := `SELECT id FROM users WHERE external_id = $1`
	err := r.pool.QueryRow(ctx, usersQuery, oldReviewerID).Scan(&rInternalID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("failed to get old reviewer: %w", err)
	}
	
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)
	
	// Находим внутренний ID PR и проверяем статус
	var prInternalID int64
	var status string
	var authorID int64
	checkQuery := `SELECT id, status, author_id FROM pull_requests WHERE external_id = $1`
	err = tx.QueryRow(ctx, checkQuery, pullRequestID).Scan(&prInternalID, &status, &authorID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("failed to check PR status: %w", err)
	}
	if status == models.StatusMerged {
		return "", ErrAlreadyMerged
	}
	
	// Проверяем, что старый ревьюер действительно назначен
	var exists bool
	checkReviewerQuery := `SELECT EXISTS(SELECT 1 FROM pr_reviewers WHERE pr_id = $1 AND reviewer_id = $2)`
	err = tx.QueryRow(ctx, checkReviewerQuery, prInternalID, rInternalID).Scan(&exists)
	if err != nil {
		return "", fmt.Errorf("failed to check reviewer assignment: %w", err)
	}
	
	if !exists {
		return "", ErrNotFound // Ревьюер не назначен
	}
	
	// Получаем команду автора PR
	var teamID int64
	teamQuery := `SELECT team_id FROM team_users WHERE user_id = $1 LIMIT 1`
	err = tx.QueryRow(ctx, teamQuery, authorID).Scan(&teamID)
	if err != nil {
		return "", fmt.Errorf("failed to get author's team: %w", err)
	}
	
	// Получаем текущих ревьюеров PR
	currentReviewersQuery := `SELECT reviewer_id FROM pr_reviewers WHERE pr_id = $1`
	rows, err := tx.Query(ctx, currentReviewersQuery, prInternalID)
	if err != nil {
		return "", fmt.Errorf("failed to get current reviewers: %w", err)
	}
	
	var currentReviewers []int64
	for rows.Next() {
		var rID int64
		if err := rows.Scan(&rID); err != nil {
			rows.Close()
			return "", fmt.Errorf("failed to scan reviewer: %w", err)
		}
		currentReviewers = append(currentReviewers, rID)
	}
	rows.Close()
	
	// Ищем нового кандидата: активный член команды, не автор, не текущий ревьюер
	candidateQuery := `
		SELECT tu.user_id
		FROM team_users tu
		JOIN users u ON tu.user_id = u.id
		WHERE tu.team_id = $1
		AND u.is_active = true
		AND tu.user_id != $2
		AND tu.user_id != ALL($3)
		ORDER BY RANDOM()
		LIMIT 1
	`
	
	var newReviewerID int64
	err = tx.QueryRow(ctx, candidateQuery, teamID, authorID, currentReviewers).Scan(&newReviewerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound // Нет подходящих кандидатов
	}
	
	if err != nil {
		return "", fmt.Errorf("failed to find replacement candidate: %w", err)
	}
	
	// Удаляем старого ревьюера
	_, err = tx.Exec(ctx, `DELETE FROM pr_reviewers WHERE pr_id = $1 AND reviewer_id = $2`, prInternalID, rInternalID)
	if err != nil {
		return "", fmt.Errorf("failed to remove old reviewer: %w", err)
	}
	
	// Добавляем нового ревьюера
	_, err = tx.Exec(ctx, `INSERT INTO pr_reviewers (pr_id, reviewer_id) VALUES ($1, $2)`, prInternalID, newReviewerID)
	if err != nil {
		return "", fmt.Errorf("failed to add new reviewer: %w", err)
	}
	
	if err = tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("failed to commit transaction: %w", err)
	}
	
	return strconv.FormatInt(newReviewerID, 10), nil
}

// GetUser получает пользователя по внешнему ID
func (r *Repository) GetUser(ctx context.Context, userID string) (*models.User, error) {
	query := `
		SELECT u.external_id, u.name, t.name as team_name, u.is_active
		FROM users u
		LEFT JOIN team_users tu ON u.id = tu.user_id
		LEFT JOIN teams t ON tu.team_id = t.id
		WHERE u.external_id = $1
		LIMIT 1
	`
	
	var user models.User
	err := r.pool.QueryRow(ctx, query, userID).Scan(
		&user.UserID, &user.Username, &user.TeamName, &user.IsActive,
	)
	
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	
	if err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}
	
	return &user, nil
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
        SELECT DISTINCT pr.external_id, pr.title, pr.author_id, pr.status
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
		var authorID int64

		if err := rows.Scan(&pr.PullRequestID, &pr.PullRequestName, &authorID, &pr.Status); err != nil {
			return nil, fmt.Errorf("failed to scan PR: %w", err)
		}

		pr.AuthorID = strconv.FormatInt(authorID, 10)
		prs = append(prs, pr)
	}

	return prs, rows.Err()
}
