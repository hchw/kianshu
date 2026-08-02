package main

import (
	"os"

	log "github.com/sirupsen/logrus"

	"github/hchw/kianshu/internal/config"
	"github/hchw/kianshu/internal/db"
	"github/hchw/kianshu/internal/httpapi"
	"github/hchw/kianshu/internal/model"
)

func main() {
	log.SetFormatter(&log.TextFormatter{FullTimestamp: true})
	log.SetOutput(os.Stdout)
	log.SetLevel(log.InfoLevel)

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("配置加载失败: %v", err)
	}
	gdb, err := db.Open(cfg)
	if err != nil {
		log.Fatalf("数据库连接失败: %v", err)
	}
	if err := model.AutoMigrate(gdb); err != nil {
		log.Fatalf("数据库迁移失败: %v", err)
	}
	srv, err := httpapi.New(gdb, cfg)
	if err != nil {
		log.Fatalf("初始化失败: %v", err)
	}
	log.Infof("鉴枢服务启动于 %s (db=%s)", cfg.Addr, cfg.DBDriver)
	srv.Start()
	if err := srv.Routes().Run(cfg.Addr); err != nil {
		srv.Stop()
		log.Fatalf("服务退出: %v", err)
	}
}
