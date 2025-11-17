package config

import (
	"fmt"
	"os"
)

type Config struct {
	Database DatabaseConfig
	Server   ServerConfig
	Logger   LoggerConfig
}

type DatabaseConfig struct {
	Host     string
	Port     string
	User     string
	Password string
	Name     string
	SSLMode  string
}

type ServerConfig struct {
	Host string
	Port string
}

type LoggerConfig struct {
	Level  string
	Format string
}

// Load загружает конфигурацию ТОЛЬКО из переменных окружения
func Load() (*Config, error) {
	cfg := &Config{
		Database: DatabaseConfig{
			Host:     getEnv("DB_HOST", "localhost"),
			Port:     getEnv("DB_PORT", "5432"),
			User:     getEnv("DB_USER", "postgres"),
			Password: getEnv("DB_PASSWORD", "postgres"),
			Name:     getEnv("DB_NAME", "pr_manager_db"),
			SSLMode:  getEnv("DB_SSLMODE", "disable"),
		},
		Server: ServerConfig{
			Host: getEnv("APP_HOST", "0.0.0.0"),
			Port: getEnv("APP_PORT", "9000"),
		},
		Logger: LoggerConfig{
			Level:  getEnv("LOG_LEVEL", "info"),
			Format: getEnv("LOG_FORMAT", "json"),
		},
	}

	// Валидация критически важных параметров
	if cfg.Database.Host == "" || cfg.Database.Name == "" {
		return nil, fmt.Errorf("critical database config missing: DB_HOST or DB_NAME not set")
	}

	return cfg, nil
}

// getEnv возвращает значение переменной окружения или значение по умолчанию
func getEnv(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}

// GetDSN возвращает строку подключения к PostgreSQL
func (c *DatabaseConfig) GetDSN() string {
	return fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		c.Host, c.Port, c.User, c.Password, c.Name, c.SSLMode,
	)
}

// GetAddress возвращает адрес сервера в формате host:port
func (c *ServerConfig) GetAddress() string {
	return fmt.Sprintf("%s:%s", c.Host, c.Port)
}
