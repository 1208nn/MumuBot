package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/flow/agent/react"

	"mumu-bot/internal/memory"
)

// StyleClassification 保存群聊风格分类结果。
type StyleClassification struct {
	Intent string `json:"intent"`
	Tone   string `json:"tone"`
}

type styleClassificationInput struct {
	Intent string `json:"intent" jsonschema:"enum=轻松起哄,enum=认同接话,enum=询问推进,enum=安抚缓和,description=当前聊天更适合参考的意图标签"`
	Tone   string `json:"tone" jsonschema:"enum=直接,enum=轻松,enum=夸张,enum=克制,description=当前聊天更适合参考的语气标签"`
}

type styleClassificationOutput struct {
	Success bool `json:"success"`
}

type styleClassificationCaptureKey struct{}

const StyleClassificationToolName = "submitStyleClassification"

func WithStyleClassificationTarget(ctx context.Context, target *StyleClassification) context.Context {
	return context.WithValue(ctx, styleClassificationCaptureKey{}, target)
}

func getStyleClassificationTarget(ctx context.Context) *StyleClassification {
	target, _ := ctx.Value(styleClassificationCaptureKey{}).(*StyleClassification)
	return target
}

func NewStyleClassificationTool() (tool.InvokableTool, error) {
	return utils.InferTool(
		StyleClassificationToolName,
		"提交当前聊天上下文的群聊风格分类结果。必须调用一次，并且只提交最合适的 intent 和 tone 标签。",
		func(ctx context.Context, input *styleClassificationInput) (*styleClassificationOutput, error) {
			target := getStyleClassificationTarget(ctx)
			if target == nil {
				return nil, fmt.Errorf("分类结果接收器未初始化")
			}

			input.Intent = strings.TrimSpace(input.Intent)
			input.Tone = strings.TrimSpace(input.Tone)
			if !memory.IsValidStyleIntent(input.Intent) || !memory.IsValidStyleTone(input.Tone) {
				return nil, fmt.Errorf("非法的分类标签")
			}

			target.Intent = input.Intent
			target.Tone = input.Tone
			if err := react.SetReturnDirectly(ctx); err != nil {
				return nil, err
			}
			return &styleClassificationOutput{Success: true}, nil
		},
	)
}
