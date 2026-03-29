package config

import (
	"crypto/rand"
	"encoding/base64"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type Config struct {
	Addr                   string
	DBPath                 string
	JWTSecret              string
	EncryptionSecret       string
	FrontendDir            string
	ProvisionRoot          string
	KubesprayImage         string
	KubesprayPlatform      string
	BootstrapAdminUser     string
	BootstrapAdminPassword string
}

func Load() Config {
	workingDir, _ := os.Getwd()
	secretDir := envOrDefault("APP_SECRET_DIR", filepath.Join(workingDir, ".kubefeel"))

	cfg := Config{
		Addr:               envOrDefault("APP_ADDR", ":8080"),
		DBPath:             envOrDefault("APP_DB_PATH", filepath.Join(workingDir, "app.db")),
		JWTSecret:          envOrStoredSecret("APP_JWT_SECRET", filepath.Join(secretDir, "jwt_secret"), 32),
		EncryptionSecret:   envOrStoredSecret("APP_ENCRYPTION_SECRET", filepath.Join(secretDir, "encryption_secret"), 32),
		FrontendDir:        envOrDefault("APP_FRONTEND_DIR", filepath.Join(workingDir, "frontend", "dist")),
		ProvisionRoot:      envOrDefault("APP_PROVISION_ROOT", filepath.Join(workingDir, "provision")),
		KubesprayImage:     envOrDefault("APP_KUBESPRAY_IMAGE", "quay.io/kubespray/kubespray:v2.29.0"),
		KubesprayPlatform:  envOrDefault("APP_KUBESPRAY_PLATFORM", defaultKubesprayPlatform()),
		BootstrapAdminUser: envOrDefault("APP_BOOTSTRAP_ADMIN_USER", "admin"),
		BootstrapAdminPassword: envOrStoredSecret(
			"APP_BOOTSTRAP_ADMIN_PASSWORD",
			filepath.Join(secretDir, "bootstrap_admin_password"),
			24,
		),
	}

	if cfg.EncryptionSecret == "" {
		cfg.EncryptionSecret = cfg.JWTSecret
	}

	return cfg
}

func defaultKubesprayPlatform() string {
	if runtime.GOARCH == "arm64" {
		return "linux/amd64"
	}

	return ""
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}

	return fallback
}

func envOrStoredSecret(key, filePath string, byteLength int) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}

	if data, err := os.ReadFile(filePath); err == nil {
		if value := strings.TrimSpace(string(data)); value != "" {
			return value
		}
	}

	if err := os.MkdirAll(filepath.Dir(filePath), 0o700); err != nil {
		return randomSecret(byteLength)
	}

	value := randomSecret(byteLength)
	if writeErr := os.WriteFile(filePath, []byte(value), 0o600); writeErr != nil {
		return value
	}

	return value
}

func randomSecret(byteLength int) string {
	if byteLength <= 0 {
		byteLength = 32
	}

	buffer := make([]byte, byteLength)
	if _, err := rand.Read(buffer); err != nil {
		return base64.RawURLEncoding.EncodeToString([]byte("kubefeel-secret-fallback"))
	}

	return base64.RawURLEncoding.EncodeToString(buffer)
}
