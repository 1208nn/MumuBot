package llm

import (
	"context"
	"fmt"
	"mumu-bot/internal/config"
	"sync"

	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/model"
)

var (
	defaultClient     model.ToolCallingChatModel
	defaultClientErr  error
	defaultClientOnce sync.Once

	auxClient     model.ToolCallingChatModel
	auxClientErr  error
	auxClientOnce sync.Once

	styleClassificationClient     model.ToolCallingChatModel
	styleClassificationClientErr  error
	styleClassificationClientOnce sync.Once
)

func newOpenAIChatModel(baseURL string, apiKey string, modelName string, extraFields map[string]interface{}) (model.ToolCallingChatModel, error) {
	return openai.NewChatModel(context.Background(), &openai.ChatModelConfig{
		BaseURL:     baseURL,
		APIKey:      apiKey,
		Model:       modelName,
		ExtraFields: extraFields,
	})
}

// NewClient 创建 LLM 客户端（单例）
func NewClient() (model.ToolCallingChatModel, error) {
	defaultClientOnce.Do(func() {
		cfg := config.Get()

		chatModel, err := newOpenAIChatModel(cfg.LLM.BaseURL, cfg.LLM.APIKey, cfg.LLM.Model, cfg.LLM.ExtraFields)
		if err != nil {
			defaultClientErr = fmt.Errorf("创建 ChatModel 失败: %w", err)
			return
		}

		defaultClient = chatModel
	})

	return defaultClient, defaultClientErr
}

// NewAuxClient 创建辅助 LLM 客户端（单例）
func NewAuxClient() (model.ToolCallingChatModel, error) {
	auxClientOnce.Do(func() {
		cfg := config.Get()
		if cfg == nil {
			auxClientErr = fmt.Errorf("配置未加载")
			return
		}
		if cfg.AuxiliaryModel.APIKey == "" || cfg.AuxiliaryModel.BaseURL == "" || cfg.AuxiliaryModel.Model == "" {
			auxClientErr = fmt.Errorf("辅助模型配置不完整")
			return
		}

		chatModel, err := newOpenAIChatModel(cfg.AuxiliaryModel.BaseURL, cfg.AuxiliaryModel.APIKey, cfg.AuxiliaryModel.Model, nil)
		if err != nil {
			auxClientErr = fmt.Errorf("创建辅助 ChatModel 失败: %w", err)
			return
		}

		auxClient = chatModel
	})

	return auxClient, auxClientErr
}

// NewStyleClassificationClient 创建风格分类 LLM 客户端（单例）
func NewStyleClassificationClient() (model.ToolCallingChatModel, error) {
	styleClassificationClientOnce.Do(func() {
		cfg := config.Get()
		if cfg == nil {
			styleClassificationClientErr = fmt.Errorf("配置未加载")
			return
		}

		modelCfg := cfg.StyleClassificationModel
		if modelCfg.APIKey == "" || modelCfg.BaseURL == "" || modelCfg.Model == "" {
			styleClassificationClientErr = fmt.Errorf("style classification 模型配置不完整")
			return
		}

		chatModel, err := newOpenAIChatModel(modelCfg.BaseURL, modelCfg.APIKey, modelCfg.Model, nil)
		if err != nil {
			styleClassificationClientErr = fmt.Errorf("创建风格分类 ChatModel 失败: %w", err)
			return
		}

		styleClassificationClient = chatModel
	})

	return styleClassificationClient, styleClassificationClientErr
}
