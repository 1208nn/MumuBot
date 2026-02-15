package learning

import (
	"context"
	"fmt"
	"mumu-bot/internal/config"
	"mumu-bot/internal/jargon"
	"mumu-bot/internal/llm"
	"mumu-bot/internal/memory"
	"mumu-bot/internal/tools"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/flow/agent/react"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

type Learner struct {
	cfg       *config.Config
	memMgr    *memory.Manager
	jargonMgr *jargon.Manager
	auxModel  model.ToolCallingChatModel
	tools     []tool.BaseTool
	agent     *react.Agent
	stopCh    chan struct{}
	wg        sync.WaitGroup
	isRunning bool
	mu        sync.Mutex
}

func New(cfg *config.Config, memMgr *memory.Manager, jargonMgr *jargon.Manager) (*Learner, error) {
	auxModel, err := llm.NewAuxClient(cfg)
	if err != nil {
		return nil, err
	}

	var learningTools []tool.BaseTool
	toolBuilders := []func() (tool.BaseTool, error){
		func() (tool.BaseTool, error) { return tools.NewSaveJargonTool() },
		func() (tool.BaseTool, error) { return tools.NewSaveExpressionTool() },
		func() (tool.BaseTool, error) { return tools.NewGetUncheckedExpressionsTool() },
		func() (tool.BaseTool, error) { return tools.NewReviewExpressionTool() },
		func() (tool.BaseTool, error) { return tools.NewGetUncheckedJargonsTool() },
		func() (tool.BaseTool, error) { return tools.NewReviewJargonTool() },
	}
	for _, build := range toolBuilders {
		t, err := build()
		if err != nil {
			return nil, err
		}
		learningTools = append(learningTools, t)
	}

	// Initialize ReAct agent
	maxStep := cfg.Learning.MaxStep
	if maxStep <= 0 {
		maxStep = 10
	}
	agent, err := react.NewAgent(context.Background(), &react.AgentConfig{
		ToolCallingModel: auxModel,
		ToolsConfig:      compose.ToolsNodeConfig{Tools: learningTools},
		MaxStep:          maxStep,
	})
	if err != nil {
		return nil, fmt.Errorf("创建学习 Agent 失败: %w", err)
	}

	return &Learner{
		cfg:       cfg,
		memMgr:    memMgr,
		jargonMgr: jargonMgr,
		auxModel:  auxModel,
		tools:     learningTools,
		agent:     agent,
		stopCh:    make(chan struct{}),
	}, nil
}

func (l *Learner) Start() {
	l.mu.Lock()
	if l.isRunning {
		l.mu.Unlock()
		return
	}
	l.isRunning = true
	l.stopCh = make(chan struct{})
	l.mu.Unlock()

	l.wg.Add(1)
	go l.runLoop()
	zap.L().Info("后台学习系统已启动")
}

func (l *Learner) Stop() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.isRunning {
		return
	}
	close(l.stopCh)
	l.wg.Wait()
	l.isRunning = false
	zap.L().Info("后台学习系统已停止")
}

func (l *Learner) runLoop() {
	defer l.wg.Done()

	// Check interval
	intervalMinutes := l.cfg.Learning.IntervalMinutes
	if intervalMinutes <= 0 {
		intervalMinutes = 10
	}
	interval := time.Duration(intervalMinutes) * time.Minute
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	go l.runTask()

	// 启动定期审核任务
	reviewIntervalMinutes := l.cfg.Learning.ReviewIntervalMinutes
	if reviewIntervalMinutes <= 0 {
		reviewIntervalMinutes = 30
	}
	reviewTicker := time.NewTicker(time.Duration(reviewIntervalMinutes) * time.Minute)
	defer reviewTicker.Stop()

	for {
		select {
		case <-l.stopCh:
			return
		case <-ticker.C:
			l.runTask()
		case <-reviewTicker.C:
			l.runReviewTask()
		}
	}
}

func (l *Learner) runTask() {
	for _, group := range l.cfg.Groups {
		if !group.Enabled {
			continue
		}
		l.processGroup(group.GroupID)
	}
}

func (l *Learner) runReviewTask() {
	for _, group := range l.cfg.Groups {
		if !group.Enabled {
			continue
		}
		l.processReview(group.GroupID)
	}
}

func (l *Learner) processReview(groupID int64) {
	prompt := `请检查当前待审核的“黑话/梗”和“表达方式”。
你需要使用 'getUncheckedJargons' 和 'getUncheckedExpressions' 工具来获取待审核列表。
然后，根据你的知识库判断这些内容的准确性和健康度。
- 如果内容准确且无害，使用 'reviewJargon' 或 'reviewExpression' 通过审核 (approve=true)。
- 如果内容明显错误、垃圾信息或有害，请拒绝 (approve=false)。
- 如果你不确定，请保持待审核状态（不做操作）。

注意：审核工具支持批量操作，请将同一审核结果的 ID 放入列表中一次性提交，尽量减少工具调用次数。`

	// 创建学习上下文
	ctx := context.Background()
	ctx = tools.WithLearningContext(ctx, &tools.LearningContext{
		GroupID:   groupID,
		MemMgr:    l.memMgr,
		JargonMgr: l.jargonMgr,
	})

	// 调用 Agent
	_, err := l.agent.Generate(ctx, []*schema.Message{
		schema.UserMessage(prompt),
	})
	if err != nil {
		zap.L().Error("后台审核任务失败", zap.Int64("group_id", groupID), zap.Error(err))
		return
	}

	zap.L().Info("后台审核任务完成", zap.Int64("group_id", groupID))
}

func (l *Learner) processGroup(groupID int64) {
	// 获取上次学习进度
	state, err := l.memMgr.GetLearningState(groupID)
	if err != nil {
		zap.L().Error("获取学习进度失败", zap.Int64("group_id", groupID), zap.Error(err))
		return
	}

	// Fetch messages after last learned ID (limit batchSize)
	batchSize := l.cfg.Learning.BatchSize
	if batchSize <= 0 {
		batchSize = 100
	}

	// 过滤 Bot 自己的消息
	botQQ := l.cfg.Persona.QQ
	botID, err := strconv.ParseInt(botQQ, 10, 64)
	if err != nil {
		zap.L().Error("解析 Bot QQ 失败", zap.String("qq", botQQ), zap.Error(err))
		return
	}

	msgs, err := l.memMgr.GetMessagesAfterID(groupID, botID, state.LastMessageID, batchSize)
	if err != nil {
		zap.L().Error("获取消息失败", zap.Int64("group_id", groupID), zap.Error(err))
		return
	}

	if len(msgs) == 0 {
		return
	}

	// 记录本批次最新的消息ID，用于更新进度
	newLastID := state.LastMessageID
	if len(msgs) > 0 {
		newLastID = msgs[len(msgs)-1].ID
	}

	minMsgCount := l.cfg.Learning.MinMsgCount
	if minMsgCount <= 0 {
		minMsgCount = 5
	}
	if len(msgs) < minMsgCount {
		// 消息太少，直接跳过，也不更新进度，等待积累更多
		return
	}

	// 构建提示词
	var chatLog strings.Builder
	for _, m := range msgs {
		chatLog.WriteString(fmt.Sprintf("%s: %s\n", m.Nickname, m.Content))
	}

	// 注入已知的黑话/梗（避免重复学习）
	knownJargons := ""
	if l.jargonMgr != nil {
		matches := l.jargonMgr.Match(chatLog.String())
		if len(matches) > 0 {
			var b strings.Builder
			b.WriteString("\n【已知黑话】：\n")
			for term, meaning := range matches {
				b.WriteString(fmt.Sprintf("- %s: %s\n", term, meaning))
			}
			knownJargons = b.String()
		}
	}

	prompt := fmt.Sprintf(`请分析以下 QQ 群聊天记录。你的任务是提取“黑话/梗”（该群体特有的术语、缩写、meme）和“表达方式”（说话风格、口头禅、句式）。

聊天记录：

%s

%s

要求：
1. 识别**新的**黑话/梗。黑话的含义应该是与上下文相关的，如果无需上下文就可以推断该词的意思，则该词不是黑话。
2. 识别独特的表达风格或口头禅。
3. 忽略通用语言或普通词汇，专注于独特的群体文化。
4. 使用提供的工具 'saveJargon' 和 'saveExpression' 来保存你的发现。
5. 如果没有发现有价值的内容，请直接回复“无新发现”。
`, chatLog.String(), knownJargons)

	// 创建学习上下文
	ctx := context.Background()
	ctx = tools.WithLearningContext(ctx, &tools.LearningContext{
		GroupID:   groupID,
		MemMgr:    l.memMgr,
		JargonMgr: l.jargonMgr,
	})

	// 调用 Agent
	_, err = l.agent.Generate(ctx, []*schema.Message{
		schema.UserMessage(prompt),
	})
	if err != nil {
		zap.L().Error("后台学习任务失败", zap.Int64("group_id", groupID), zap.Error(err))
		return
	}

	zap.L().Info("后台学习任务完成", zap.Int64("group_id", groupID))

	// 更新进度
	if err := l.memMgr.UpdateLearningState(groupID, newLastID); err != nil {
		zap.L().Error("更新学习进度失败", zap.Int64("group_id", groupID), zap.Error(err))
	}
}
