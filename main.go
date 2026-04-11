package main

import (
	"context"
	"fmt"
	"mumu-bot/internal/agent"
	"mumu-bot/internal/config"
	"mumu-bot/internal/llm"
	"mumu-bot/internal/logger"
	"mumu-bot/internal/memory"
	webapp "mumu-bot/internal/web/app"
	"mumu-bot/internal/web/services"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.uber.org/zap"
)

type runtimeSource struct {
	cfg       *config.Config
	memMgr    *memory.Manager
	mumuAgent *agent.Agent
}

func (s runtimeSource) Snapshot() webapp.RuntimeSnapshot {
	snapshot := webapp.RuntimeSnapshot{
		LearningOn:    s.cfg.Learning.Enabled,
		EnabledGroups: enabledGroups(s.cfg.Groups),
	}

	if s.mumuAgent != nil {
		snapshot.Connected = s.mumuAgent.OneBotConnected()
		snapshot.SelfID = s.mumuAgent.BotSelfID()
		snapshot.MCPToolCount = s.mumuAgent.MCPToolCount()
	}

	if s.memMgr != nil {
		if mood, err := s.memMgr.GetMoodState(); err == nil {
			snapshot.CurrentMood = mood
		}
	}

	return snapshot
}

func main() {
	configPath := "config/config.yaml"
	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Printf("加载配置失败: %v\n", err)
		os.Exit(1)
	}

	logger.Init(cfg.App.LogLevel, cfg.App.Debug)
	zap.L().Info("配置已加载", zap.String("path", configPath))

	embeddingClient, err := llm.NewEmbeddingClient()
	if err != nil {
		zap.L().Fatal("Embedding 客户端创建失败", zap.Error(err))
	}

	memoryMgr, err := memory.NewManager(embeddingClient)
	if err != nil {
		zap.L().Fatal("记忆管理器创建失败", zap.Error(err))
	}
	defer memoryMgr.Close()
	zap.L().Info("记忆系统已初始化")

	mumuAgent, err := agent.New(memoryMgr)
	if err != nil {
		zap.L().Fatal("Agent 创建失败", zap.Error(err))
	}
	mumuAgent.Start()

	stickerDir := cfg.Sticker.StoragePath
	if stickerDir == "" {
		stickerDir = "./stickers"
	}
	adminService := services.NewAdminService(memoryMgr.GetDB(), stickerDir).
		WithMemoryDeleter(memoryMgr).
		WithJargonReloader(mumuAgent)
	app := webapp.New(cfg, adminService, runtimeSource{
		cfg:       cfg,
		memMgr:    memoryMgr,
		mumuAgent: mumuAgent,
	})
	httpServer := app.Server()

	go func() {
		zap.L().Info("管理后台启动", zap.String("addr", app.Addr()))
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			zap.L().Error("管理后台异常退出", zap.Error(err))
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	zap.L().Info("沐沐已上线，按 Ctrl+C 退出")
	<-quit

	zap.L().Info("正在关闭...")
	mumuAgent.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(ctx); err != nil {
		zap.L().Warn("关闭管理后台失败", zap.Error(err))
	}

	zap.L().Info("再见！")
}

func enabledGroups(groups []config.GroupConfig) int {
	count := 0
	for _, group := range groups {
		if group.Enabled {
			count++
		}
	}
	return count
}
