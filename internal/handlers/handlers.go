package handlers

import (
	"errors"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/untibullet/pr-manager-avito/internal/models"
	"github.com/untibullet/pr-manager-avito/internal/repository"
	"go.uber.org/zap"
)

// Коды ошибок для API
const (
	ErrCodeTeamExists  = "TEAM_EXISTS"
	ErrCodePRExists    = "PR_EXISTS"
	ErrCodePRMerged    = "PR_MERGED"
	ErrCodeNotAssigned = "NOT_ASSIGNED"
	ErrCodeNoCandidate = "NO_CANDIDATE"
	ErrCodeNotFound    = "NOT_FOUND"
)

type Handler struct {
	repo   *repository.Repository
	logger *zap.Logger
}

// New создает новый экземпляр обработчика
func New(repo *repository.Repository, logger *zap.Logger) *Handler {
	return &Handler{
		repo:   repo,
		logger: logger,
	}
}

// ErrorResponse представляет структуру ошибки API
type ErrorResponse struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// newErrorResponse создает стандартный ответ с ошибкой
func newErrorResponse(code, message string) ErrorResponse {
	var resp ErrorResponse
	resp.Error.Code = code
	resp.Error.Message = message
	return resp
}

// CreateTeam создает новую команду
func (h *Handler) CreateTeam(c echo.Context) error {
	h.logger.Info("CreateTeam: начало обработки запроса")

	var req models.Team
	if err := c.Bind(&req); err != nil {
		h.logger.Error("CreateTeam: ошибка парсинга тела запроса", zap.Error(err))
		return c.JSON(http.StatusBadRequest, newErrorResponse(ErrCodeNotFound, "invalid request body"))
	}

	h.logger.Info("CreateTeam: валидация данных команды", zap.String("team_name", req.TeamName), zap.Int("members_count", len(req.Members)))

	team, err := h.repo.CreateTeam(c.Request().Context(), req)
	if err != nil {
		h.logger.Error("CreateTeam: ошибка создания команды", zap.Error(err), zap.String("team_name", req.TeamName))
		return c.JSON(http.StatusInternalServerError, newErrorResponse(ErrCodeNotFound, "failed to create team"))
	}

	h.logger.Info("CreateTeam: команда успешно создана", zap.String("team_name", team.TeamName))
	return c.JSON(http.StatusCreated, map[string]interface{}{"team": team})
}

// GetTeam получает команду по имени
func (h *Handler) GetTeam(c echo.Context) error {
	teamName := c.QueryParam("team_name")
	h.logger.Info("GetTeam: получение команды", zap.String("team_name", teamName))

	if teamName == "" {
		h.logger.Warn("GetTeam: параметр team_name отсутствует")
		return c.JSON(http.StatusBadRequest, newErrorResponse(ErrCodeNotFound, "team_name parameter is required"))
	}

	team, err := h.repo.GetTeam(c.Request().Context(), teamName)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			h.logger.Warn("GetTeam: команда не найдена", zap.String("team_name", teamName))
			return c.JSON(http.StatusNotFound, newErrorResponse(ErrCodeNotFound, "team not found"))
		}
		h.logger.Error("GetTeam: ошибка получения команды", zap.Error(err), zap.String("team_name", teamName))
		return c.JSON(http.StatusInternalServerError, newErrorResponse(ErrCodeNotFound, "failed to get team"))
	}

	h.logger.Info("GetTeam: команда успешно получена", zap.String("team_name", teamName), zap.Int("members_count", len(team.Members)))
	return c.JSON(http.StatusOK, team)
}

// SetUserIsActive обновляет статус активности пользователя
func (h *Handler) SetUserIsActive(c echo.Context) error {
	h.logger.Info("SetUserIsActive: начало обработки запроса")

	var req struct {
		UserID   string `json:"user_id"`
		IsActive bool   `json:"is_active"`
	}

	if err := c.Bind(&req); err != nil {
		h.logger.Error("SetUserIsActive: ошибка парсинга тела запроса", zap.Error(err))
		return c.JSON(http.StatusBadRequest, newErrorResponse(ErrCodeNotFound, "invalid request body"))
	}

	h.logger.Info("SetUserIsActive: обновление статуса пользователя", zap.String("user_id", req.UserID), zap.Bool("is_active", req.IsActive))

	err := h.repo.UpdateUserStatus(c.Request().Context(), req.UserID, req.IsActive)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			h.logger.Warn("SetUserIsActive: пользователь не найден", zap.String("user_id", req.UserID))
			return c.JSON(http.StatusNotFound, newErrorResponse(ErrCodeNotFound, "user not found"))
		}
		h.logger.Error("SetUserIsActive: ошибка обновления статуса", zap.Error(err), zap.String("user_id", req.UserID))
		return c.JSON(http.StatusInternalServerError, newErrorResponse(ErrCodeNotFound, "failed to update user status"))
	}

	// Получаем обновленные данные пользователя
	user, err := h.repo.GetUser(c.Request().Context(), req.UserID)
	if err != nil {
		h.logger.Error("SetUserIsActive: ошибка получения обновленного пользователя", zap.Error(err))
		return c.JSON(http.StatusInternalServerError, newErrorResponse(ErrCodeNotFound, "failed to get updated user"))
	}

	h.logger.Info("SetUserIsActive: статус пользователя обновлен", zap.String("user_id", req.UserID))
	return c.JSON(http.StatusOK, map[string]interface{}{"user": user})
}

// CreatePullRequest создает новый PR с автоматическим назначением ревьюеров
func (h *Handler) CreatePullRequest(c echo.Context) error {
	h.logger.Info("CreatePullRequest: начало обработки запроса")

	var req struct {
		PullRequestID   string `json:"pull_request_id"`
		PullRequestName string `json:"pull_request_name"`
		AuthorID        string `json:"author_id"`
	}

	if err := c.Bind(&req); err != nil {
		h.logger.Error("CreatePullRequest: ошибка парсинга тела запроса", zap.Error(err))
		return c.JSON(http.StatusBadRequest, newErrorResponse(ErrCodeNotFound, "invalid request body"))
	}

	h.logger.Info("CreatePullRequest: создание PR",
		zap.String("pr_id", req.PullRequestID),
		zap.String("pr_name", req.PullRequestName),
		zap.String("author_id", req.AuthorID))

	pr, err := h.repo.CreatePR(c.Request().Context(), req.PullRequestID, req.PullRequestName, req.AuthorID)
	if err != nil {
		if errors.Is(err, repository.ErrAlreadyExists) {
			h.logger.Warn("CreatePullRequest: PR уже существует", zap.String("pr_id", req.PullRequestID))
			return c.JSON(http.StatusConflict, newErrorResponse(ErrCodePRExists, "PR id already exists"))
		}
		if errors.Is(err, repository.ErrNotFound) {
			h.logger.Warn("CreatePullRequest: автор или команда не найдены", zap.String("author_id", req.AuthorID))
			return c.JSON(http.StatusNotFound, newErrorResponse(ErrCodeNotFound, "author or team not found"))
		}
		h.logger.Error("CreatePullRequest: ошибка создания PR", zap.Error(err), zap.String("pr_id", req.PullRequestID))
		return c.JSON(http.StatusInternalServerError, newErrorResponse(ErrCodeNotFound, "failed to create PR"))
	}

	h.logger.Info("CreatePullRequest: PR успешно создан",
		zap.String("pr_id", pr.PullRequestID),
		zap.Int("reviewers_count", len(pr.AssignedReviewers)))
	return c.JSON(http.StatusCreated, map[string]interface{}{"pr": pr})
}

// MergePullRequest переводит PR в статус MERGED
func (h *Handler) MergePullRequest(c echo.Context) error {
	h.logger.Info("MergePullRequest: начало обработки запроса")

	var req struct {
		PullRequestID string `json:"pull_request_id"`
	}

	if err := c.Bind(&req); err != nil {
		h.logger.Error("MergePullRequest: ошибка парсинга тела запроса", zap.Error(err))
		return c.JSON(http.StatusBadRequest, newErrorResponse(ErrCodeNotFound, "invalid request body"))
	}

	h.logger.Info("MergePullRequest: слияние PR", zap.String("pr_id", req.PullRequestID))

	pr, err := h.repo.MergePR(c.Request().Context(), req.PullRequestID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			h.logger.Warn("MergePullRequest: PR не найден", zap.String("pr_id", req.PullRequestID))
			return c.JSON(http.StatusNotFound, newErrorResponse(ErrCodeNotFound, "PR not found"))
		}
		h.logger.Error("MergePullRequest: ошибка слияния PR", zap.Error(err), zap.String("pr_id", req.PullRequestID))
		return c.JSON(http.StatusInternalServerError, newErrorResponse(ErrCodeNotFound, "failed to merge PR"))
	}

	h.logger.Info("MergePullRequest: PR успешно слит", zap.String("pr_id", pr.PullRequestID), zap.String("status", pr.Status))
	return c.JSON(http.StatusOK, map[string]interface{}{"pr": pr})
}

// ReassignReviewer переназначает ревьюера на PR с автоматическим поиском замены
func (h *Handler) ReassignReviewer(c echo.Context) error {
	h.logger.Info("ReassignReviewer: начало обработки запроса")

	var req struct {
		PullRequestID string `json:"pull_request_id"`
		OldUserID     string `json:"old_user_id"`
	}

	if err := c.Bind(&req); err != nil {
		h.logger.Error("ReassignReviewer: ошибка парсинга тела запроса", zap.Error(err))
		return c.JSON(http.StatusBadRequest, newErrorResponse(ErrCodeNotFound, "invalid request body"))
	}

	h.logger.Info("ReassignReviewer: переназначение ревьюера", 
		zap.String("pr_id", req.PullRequestID), 
		zap.String("old_user_id", req.OldUserID))

	newReviewerID, err := h.repo.ReassignReviewerAuto(c.Request().Context(), req.PullRequestID, req.OldUserID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			// Может быть несколько причин: PR не найден, ревьюер не назначен, нет кандидатов
			pr, getErr := h.repo.GetPR(c.Request().Context(), req.PullRequestID)
			if getErr != nil {
				h.logger.Warn("ReassignReviewer: PR не найден", zap.String("pr_id", req.PullRequestID))
				return c.JSON(http.StatusNotFound, newErrorResponse(ErrCodeNotFound, "PR not found"))
			}
			
			// Проверяем, был ли старый ревьюер назначен
			oldReviewerAssigned := false
			for _, reviewerID := range pr.AssignedReviewers {
				if reviewerID == req.OldUserID {
					oldReviewerAssigned = true
					break
				}
			}
			
			if !oldReviewerAssigned {
				h.logger.Warn("ReassignReviewer: пользователь не назначен ревьюером", 
					zap.String("pr_id", req.PullRequestID), 
					zap.String("old_user_id", req.OldUserID))
				return c.JSON(http.StatusConflict, newErrorResponse(ErrCodeNotAssigned, "reviewer is not assigned to this PR"))
			}
			
			// Значит нет подходящих кандидатов
			h.logger.Warn("ReassignReviewer: нет активных кандидатов для замены", zap.String("pr_id", req.PullRequestID))
			return c.JSON(http.StatusConflict, newErrorResponse(ErrCodeNoCandidate, "no active replacement candidate in team"))
		}
		
		if errors.Is(err, repository.ErrAlreadyMerged) {
			h.logger.Warn("ReassignReviewer: попытка переназначения на смерженный PR", zap.String("pr_id", req.PullRequestID))
			return c.JSON(http.StatusConflict, newErrorResponse(ErrCodePRMerged, "cannot reassign on merged PR"))
		}
		
		h.logger.Error("ReassignReviewer: ошибка переназначения", zap.Error(err), zap.String("pr_id", req.PullRequestID))
		return c.JSON(http.StatusInternalServerError, newErrorResponse(ErrCodeNotFound, "failed to reassign reviewer"))
	}

	// Получаем обновленный PR
	pr, err := h.repo.GetPR(c.Request().Context(), req.PullRequestID)
	if err != nil {
		h.logger.Error("ReassignReviewer: ошибка получения обновленного PR", zap.Error(err))
		return c.JSON(http.StatusInternalServerError, newErrorResponse(ErrCodeNotFound, "failed to get updated PR"))
	}

	h.logger.Info("ReassignReviewer: ревьюер успешно переназначен", 
		zap.String("pr_id", req.PullRequestID),
		zap.String("old_reviewer", req.OldUserID),
		zap.String("new_reviewer", newReviewerID))

	response := map[string]interface{}{
		"pr":          pr,
		"replaced_by": newReviewerID,
	}
	
	return c.JSON(http.StatusOK, response)
}

// GetUserReviews получает список PR, где пользователь назначен ревьюером
func (h *Handler) GetUserReviews(c echo.Context) error {
	userID := c.QueryParam("user_id")
	h.logger.Info("GetUserReviews: получение PR для ревьюера", zap.String("user_id", userID))

	if userID == "" {
		h.logger.Warn("GetUserReviews: параметр user_id отсутствует")
		return c.JSON(http.StatusBadRequest, newErrorResponse(ErrCodeNotFound, "user_id parameter is required"))
	}

	prs, err := h.repo.GetPRsByReviewer(c.Request().Context(), userID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			h.logger.Warn("GetUserReviews: пользователь не найден", zap.String("user_id", userID))
			return c.JSON(http.StatusNotFound, newErrorResponse(ErrCodeNotFound, "user not found"))
		}
		h.logger.Error("GetUserReviews: ошибка получения PR", zap.Error(err), zap.String("user_id", userID))
		return c.JSON(http.StatusInternalServerError, newErrorResponse(ErrCodeNotFound, "failed to get user reviews"))
	}

	h.logger.Info("GetUserReviews: PR успешно получены", zap.String("user_id", userID), zap.Int("prs_count", len(prs)))

	response := map[string]interface{}{
		"user_id":       userID,
		"pull_requests": prs,
	}

	return c.JSON(http.StatusOK, response)
}

// RegisterRoutes регистрирует все маршруты API
func (h *Handler) RegisterRoutes(e *echo.Echo) {
	// Teams
	e.POST("/team/add", h.CreateTeam)
	e.GET("/team/get", h.GetTeam)

	// Users
	e.POST("/users/setIsActive", h.SetUserIsActive)
	e.GET("/users/getReview", h.GetUserReviews)

	// Pull Requests
	e.POST("/pullRequest/create", h.CreatePullRequest)
	e.POST("/pullRequest/merge", h.MergePullRequest)
	e.POST("/pullRequest/reassign", h.ReassignReviewer)
}
