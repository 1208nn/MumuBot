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

// ContextClassification 保存回复前上下文分类结果。
type ContextClassification struct {
	Intent     string `json:"intent"`
	Tone       string `json:"tone"`
	TopicQuery string `json:"topic_query"`
}

type contextClassificationInput struct {
	Intent     string `json:"intent" jsonschema:"enum=轻松起哄,enum=认同接话,enum=询问推进,enum=安抚缓和,description=当前聊天更适合参考的发言方向"`
	Tone       string `json:"tone" jsonschema:"enum=直接,enum=轻松,enum=夸张,enum=克制,description=当前聊天更适合参考的语气标签"`
	TopicQuery string `json:"topic_query" jsonschema:"description=用于检索历史话题和长期记忆的短查询；低信息量或不需要检索时留空"`
}

type contextClassificationOutput struct {
	Success bool `json:"success"`
}

type contextClassificationCaptureKey struct{}

const ContextClassificationToolName = "submitContextClassification"

func WithContextClassificationTarget(ctx context.Context, target *ContextClassification) context.Context {
	return context.WithValue(ctx, contextClassificationCaptureKey{}, target)
}

func getContextClassificationTarget(ctx context.Context) *ContextClassification {
	target, _ := ctx.Value(contextClassificationCaptureKey{}).(*ContextClassification)
	return target
}

func NewContextClassificationTool() (tool.InvokableTool, error) {
	return utils.InferTool(
		ContextClassificationToolName,
		"提交当前聊天上下文分类结果。必须调用一次，并且只提交 intent、tone、topic_query。",
		func(ctx context.Context, input *contextClassificationInput) (*contextClassificationOutput, error) {
			target := getContextClassificationTarget(ctx)
			if target == nil {
				return nil, fmt.Errorf("分类结果接收器未初始化")
			}

			input.Intent = strings.TrimSpace(input.Intent)
			input.Tone = strings.TrimSpace(input.Tone)
			input.TopicQuery = strings.TrimSpace(input.TopicQuery)
			if !memory.IsValidStyleIntent(input.Intent) || !memory.IsValidStyleTone(input.Tone) {
				return nil, fmt.Errorf("非法的分类标签")
			}

			target.Intent = input.Intent
			target.Tone = input.Tone
			target.TopicQuery = input.TopicQuery
			_ = react.SetReturnDirectly(ctx)
			return &contextClassificationOutput{Success: true}, nil
		},
	)
}
