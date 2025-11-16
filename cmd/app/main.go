package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/untibullet/pr-manager-avito/internal/config"
	"github.com/untibullet/pr-manager-avito/internal/handlers"
	"github.com/untibullet/pr-manager-avito/internal/repository"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func main() {
	// Загрузка конфигурации
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
		os.Exit(1)
	}

	// Инициализация логгера
	logger, err := initLogger(cfg.Logger)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialize logger: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync()

	logger.Info("starting PR reviewer assignment service",
		zap.String("server_address", cfg.Server.GetAddress()))

	// Подключение к базе данных
	dbPool, err := initDatabase(context.Background(), cfg.Database, logger)
	if err != nil {
		logger.Fatal("failed to connect to database", zap.Error(err))
	}
	defer dbPool.Close()

	logger.Info("database connection established")

	// Инициализация слоя данных
	repo := repository.New(dbPool)

	// Инициализация обработчиков
	handler := handlers.New(repo, logger)

	// Настройка Echo сервера
	e := echo.New()
	e.HideBanner = true
	e.HidePort = true

	// Middleware
	e.Use(middleware.RequestLoggerWithConfig(middleware.RequestLoggerConfig{
		LogURI:    true,
		LogStatus: true,
		LogError:  true,
		LogValuesFunc: func(c echo.Context, v middleware.RequestLoggerValues) error {
			if v.Error == nil {
				logger.Info("request",
					zap.String("method", c.Request().Method),
					zap.String("uri", v.URI),
					zap.Int("status", v.Status),
				)
			} else {
				logger.Error("request error",
					zap.String("method", c.Request().Method),
					zap.String("uri", v.URI),
					zap.Int("status", v.Status),
					zap.Error(v.Error),
				)
			}
			return nil
		},
	}))
	e.Use(middleware.Recover())
	e.Use(middleware.CORS())

	// Регистрация роутов
	handler.RegisterRoutes(e)

	// Health check endpoint
	e.GET("/health", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	})

	// Graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Запуск сервера в горутине
	go func() {
		addr := cfg.Server.GetAddress()
		logger.Info("server listening", zap.String("address", addr))
		if err := e.Start(addr); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Fatal("server start failed", zap.Error(err))
		}
	}()

	// Ожидание сигнала завершения
	<-ctx.Done()
	logger.Info("shutting down server gracefully")

	// Таймаут для graceful shutdown
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := e.Shutdown(shutdownCtx); err != nil {
		logger.Error("server shutdown error", zap.Error(err))
	}

	logger.Info("server stopped")
}

// initLogger инициализирует zap логгер на основе конфигурации
func initLogger(cfg config.LoggerConfig) (*zap.Logger, error) {
	var level zapcore.Level
	if err := level.UnmarshalText([]byte(cfg.Level)); err != nil {
		level = zapcore.InfoLevel
	}

	var zapConfig zap.Config
	if cfg.Format == "json" {
		zapConfig = zap.NewProductionConfig()
	} else {
		zapConfig = zap.NewDevelopmentConfig()
		zapConfig.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	}

	zapConfig.Level = zap.NewAtomicLevelAt(level)

	logger, err := zapConfig.Build()
	if err != nil {
		return nil, fmt.Errorf("failed to build logger: %w", err)
	}

	return logger, nil
}

// initDatabase инициализирует пул подключений к PostgreSQL
func initDatabase(ctx context.Context, cfg config.DatabaseConfig, logger *zap.Logger) (*pgxpool.Pool, error) {
	poolConfig, err := pgxpool.ParseConfig(cfg.GetDSN())
	if err != nil {
		return nil, fmt.Errorf("failed to parse database config: %w", err)
	}

	// Настройки пула
	poolConfig.MaxConns = 25
	poolConfig.MinConns = 5
	poolConfig.MaxConnLifetime = time.Hour
	poolConfig.MaxConnIdleTime = 30 * time.Minute
	poolConfig.HealthCheckPeriod = time.Minute

	// Создание пула
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection pool: %w", err)
	}

	// Проверка подключения
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return pool, nil
}
