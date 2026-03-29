package main

import (
	"log"

	"multikube-manager/internal/api"
	"multikube-manager/internal/config"
	"multikube-manager/internal/database"
	"multikube-manager/internal/kube"
	"multikube-manager/internal/provision"
	"multikube-manager/internal/rbac"
	"multikube-manager/internal/security"
)

func main() {
	cfg := config.Load()

	db, err := database.Open(cfg)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}

	if err := rbac.Seed(db, cfg.BootstrapAdminUser, cfg.BootstrapAdminPassword); err != nil {
		log.Fatalf("seed rbac: %v", err)
	}

	cipher := security.NewCipher(cfg.EncryptionSecret)
	jwtManager := security.NewJWTManager(cfg.JWTSecret)
	kubeFactory := kube.NewFactory(cipher)
	provisionRunner := provision.NewRunner(cfg.ProvisionRoot, cfg.KubesprayImage, cfg.KubesprayPlatform)

	server := api.NewServer(cfg, db, jwtManager, kubeFactory, provisionRunner)
	if err := server.Run(); err != nil {
		log.Fatalf("run server: %v", err)
	}
}
