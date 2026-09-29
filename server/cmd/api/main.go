package main

import (
	"flag"
	"log"

	"github.com/gin-gonic/gin"
	"github.com/remote/server/internal/admin"
	apiserver "github.com/remote/server/internal/api"
	"github.com/remote/server/internal/audit"
	"github.com/remote/server/internal/auth"
	"github.com/remote/server/internal/config"
	"github.com/remote/server/internal/device"
	"github.com/remote/server/internal/hub"
	"github.com/remote/server/internal/session"
	"github.com/remote/server/internal/store"
	"github.com/remote/server/internal/user"
)

func main() {
	cfgPath := flag.String("config", "configs/config.yaml", "config file path")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatal(err)
	}

	db, err := store.Open(cfg)
	if err != nil {
		log.Fatal(err)
	}

	if cfg.Server.Mode == "release" {
		gin.SetMode(gin.ReleaseMode)
	}

	jwtSvc := auth.NewService(&cfg.JWT)
	h := hub.New()
	srv := &apiserver.Server{
		DB:       db,
		JWT:      jwtSvc,
		Users:    user.NewService(db, jwtSvc),
		Devices:  device.NewService(db),
		Sessions: session.NewService(db, h),
		Admin:    admin.NewService(db),
		Audit:    audit.NewService(db),
		Hub:      h,
	}

	r := gin.Default()
	srv.RegisterRoutes(r)

	log.Printf("Remote API listening on %s", cfg.Server.Addr)
	if err := r.Run(cfg.Server.Addr); err != nil {
		log.Fatal(err)
	}
}
