package llm

import (
	"context"
	"fmt"
	"mumu-bot/internal/config"
	"strings"
	"sync"

	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/model"
)

var (
	highTierClientSlot = &tierClientSlot{}
	midTierClientSlot  = &tierClientSlot{}
	lowTierClientSlot  = &tierClientSlot{}

	chatModelFactory = newOpenAIChatModel
)

type Tier string

const (
	TierHigh Tier = "high"
	TierMid  Tier = "mid"
	TierLow  Tier = "low"
)

type tierClientSlot struct {
	client model.ToolCallingChatModel
	err    error
	once   sync.Once
}

func newOpenAIChatModel(baseURL string, apiKey string, modelName string, extraFields map[string]interface{}) (model.ToolCallingChatModel, error) {
	return openai.NewChatModel(context.Background(), &openai.ChatModelConfig{
		BaseURL:     baseURL,
		APIKey:      apiKey,
		Model:       modelName,
		ExtraFields: extraFields,
	})
}

func TierDisplayName(tier Tier) string {
	switch tier {
	case TierHigh:
		return "高档模型"
	case TierMid:
		return "中档模型"
	case TierLow:
		return "轻量模型"
	default:
		return "模型"
	}
}

func NewClientForTier(tier Tier) (model.ToolCallingChatModel, error) {
	cfg := config.Get()
	if cfg == nil {
		return nil, fmt.Errorf("配置未加载")
	}

	modelCfg, err := tierConfig(cfg, tier)
	if err != nil {
		return nil, err
	}
	return getTierClient(tier, modelCfg)
}

func getTierClient(tier Tier, cfg config.ModelConfig) (model.ToolCallingChatModel, error) {
	slot := tierClientSlotFor(tier)
	slot.once.Do(func() {
		apiKey := strings.TrimSpace(cfg.APIKey)
		baseURL := strings.TrimSpace(cfg.BaseURL)
		modelName := strings.TrimSpace(cfg.Model)
		if apiKey == "" || baseURL == "" || modelName == "" {
			slot.err = fmt.Errorf("%s配置不完整", TierDisplayName(tier))
			return
		}

		chatModel, err := chatModelFactory(baseURL, apiKey, modelName, cfg.ExtraFields)
		if err != nil {
			slot.err = fmt.Errorf("创建%s失败: %w", TierDisplayName(tier), err)
			return
		}
		slot.client = chatModel
	})

	return slot.client, slot.err
}

func tierConfig(cfg *config.Config, tier Tier) (config.ModelConfig, error) {
	switch tier {
	case TierHigh:
		return cfg.ModelTiers.High, nil
	case TierMid:
		return cfg.ModelTiers.Mid, nil
	case TierLow:
		return cfg.ModelTiers.Low, nil
	default:
		return config.ModelConfig{}, fmt.Errorf("未知模型档位: %s", tier)
	}
}

func tierClientSlotFor(tier Tier) *tierClientSlot {
	switch tier {
	case TierHigh:
		return highTierClientSlot
	case TierMid:
		return midTierClientSlot
	case TierLow:
		return lowTierClientSlot
	default:
		return &tierClientSlot{}
	}
}
