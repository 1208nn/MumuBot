package main

import (
	"fmt"
	"mumu-bot/internal/agent"
	"mumu-bot/internal/config"
	"mumu-bot/internal/llm"
	"mumu-bot/internal/logger"
	"mumu-bot/internal/memory"
	"mumu-bot/internal/server"
	"os"
	"os/signal"
	"syscall"

	"go.uber.org/zap"
)

func main() {
	configPath := "config/config.yaml"
	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Printf("加载配置失败: %v\n", err)
		os.Exit(1)
	}

	// 初始化日志系统
	logger.Init(cfg.App.LogLevel, cfg.App.Debug)

	zap.L().Info("配置已加载", zap.String("path", configPath))

	// 创建 Embedding 客户端
	embeddingClient, err := llm.NewEmbeddingClient()
	if err != nil {
		zap.L().Error("Embedding 客户端创建失败，向量检索不可用", zap.Error(err))
		embeddingClient = nil
	}

	// 创建记忆管理器
	memoryMgr, err := memory.NewManager(embeddingClient)
	if err != nil {
		zap.L().Fatal("记忆管理器创建失败", zap.Error(err))
	}
	defer memoryMgr.Close()
	zap.L().Info("记忆系统已初始化")

	// 创建 Agent
	mumuAgent, err := agent.New(memoryMgr)
	if err != nil {
		zap.L().Fatal("Agent 创建失败", zap.Error(err))
	}
	mumuAgent.Start()

	// 启动HTTP服务（用于健康检查等）
	httpServer := server.NewServer(memoryMgr)
	go httpServer.Start()

	// 等待退出信号
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	zap.L().Info("沐沐已上线，按 Ctrl+C 退出")
	<-quit

	zap.L().Info("正在关闭...")
	mumuAgent.Stop()
	httpServer.Stop()
	zap.L().Info("再见！")
}
