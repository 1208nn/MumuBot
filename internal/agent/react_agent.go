package agent

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"mumu-bot/internal/config"
	"mumu-bot/internal/jargon"
	"mumu-bot/internal/learning"
	"mumu-bot/internal/llm"
	"mumu-bot/internal/mcp"
	"mumu-bot/internal/memory"
	"mumu-bot/internal/onebot"
	"mumu-bot/internal/persona"
	"mumu-bot/internal/tools"
	"mumu-bot/internal/utils"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bytedance/sonic"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/flow/agent/react"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

// Agent 沐沐智能体
type Agent struct {
	persona        *persona.Persona
	memory         *memory.Manager
	model          model.ToolCallingChatModel
	vision         *llm.VisionClient // 多模态视觉模型
	bot            *onebot.Client
	react          *react.Agent
	tools          []tool.BaseTool
	mcpMgr         *mcp.Manager        // MCP 管理器
	concurrencyMgr *ConcurrencyManager // 并发管理器

	jargonMgr *jargon.Manager   // 黑话管理器
	learner   *learning.Learner // 后台学习系统

	// 消息缓冲（使用 ring buffer 避免扩容缩容开销）
	buffers   map[int64]*utils.RingBuffer[*onebot.GroupMessage]
	buffersMu sync.RWMutex // 保护 map 本身的并发访问

	// 正在处理中的群组（防止重复思考）和最后处理时间
	processing        map[int64]bool
	lastProcessedTime map[int64]time.Time
	processingMu      sync.RWMutex

	stopCh chan struct{}
	wg     sync.WaitGroup
}

// New 创建 Agent
func New(
	p *persona.Persona,
	mem *memory.Manager,
	m model.ToolCallingChatModel,
	vision *llm.VisionClient,
	bot *onebot.Client,
) (*Agent, error) {
	a := &Agent{
		persona:           p,
		memory:            mem,
		model:             m,
		vision:            vision,
		bot:               bot,
		buffers:           make(map[int64]*utils.RingBuffer[*onebot.GroupMessage]),
		processing:        make(map[int64]bool),
		lastProcessedTime: make(map[int64]time.Time),
		stopCh:            make(chan struct{}),
	}
	cfg := config.Get()

	// 初始化并发管理器
	a.concurrencyMgr = NewConcurrencyManager(cfg.Agent.MaxCoroutine, a.think)

	// 初始化黑话管理器
	a.jargonMgr = jargon.New(mem)

	// 初始化后台学习系统
	learner, err := learning.New(mem, a.jargonMgr)
	if err != nil {
		zap.L().Error("初始化后台学习系统失败", zap.Error(err))
	} else {
		a.learner = learner
	}

	// 初始化 MCP 管理器
	a.mcpMgr = mcp.NewMCPManager()
	if err := a.mcpMgr.LoadFromConfig("config/mcp.json"); err != nil {
		zap.L().Error("加载 MCP 配置失败", zap.Error(err))
	}

	if err := a.initTools(); err != nil {
		return nil, err
	}
	if err := a.initReact(); err != nil {
		return nil, err
	}
	return a, nil
}

func (a *Agent) initTools() error {
	toolBuilders := []func() (tool.BaseTool, error){
		// 记忆相关
		func() (tool.BaseTool, error) { return tools.NewSaveMemoryTool() },
		func() (tool.BaseTool, error) { return tools.NewQueryMemoryTool() },
		// 搜索黑话/表达
		func() (tool.BaseTool, error) { return tools.NewSearchJargonTool() },
		func() (tool.BaseTool, error) { return tools.NewSearchExpressionsTool() },
		// 用户信息
		func() (tool.BaseTool, error) { return tools.NewUpdateMemberProfileTool() },
		func() (tool.BaseTool, error) { return tools.NewGetMemberInfoTool() },
		func() (tool.BaseTool, error) { return tools.NewGetRecentMessagesTool() },
		// 发言相关
		func() (tool.BaseTool, error) { return tools.NewSpeakTool() },
		func() (tool.BaseTool, error) { return tools.NewStayQuietTool() },
		// 群交互
		func() (tool.BaseTool, error) { return tools.NewGetGroupInfoTool() },
		func() (tool.BaseTool, error) { return tools.NewGetGroupMemberDetailTool() },
		func() (tool.BaseTool, error) { return tools.NewPokeTool() },
		func() (tool.BaseTool, error) { return tools.NewReactToMessageTool() },
		func() (tool.BaseTool, error) { return tools.NewRecallMessageTool() },
		// 表情包相关
		func() (tool.BaseTool, error) { return tools.NewSearchStickersTool() },
		func() (tool.BaseTool, error) { return tools.NewSendStickerTool() },
		// 群信息
		func() (tool.BaseTool, error) { return tools.NewGetGroupNoticesTool() },
		func() (tool.BaseTool, error) { return tools.NewGetEssenceMessagesTool() },
		func() (tool.BaseTool, error) { return tools.NewGetMessageReactionsTool() },
		func() (tool.BaseTool, error) { return tools.NewGetForwardMessageDetailTool() },
		// 情绪系统
		func() (tool.BaseTool, error) { return tools.NewUpdateMoodTool() },
		// HTTP GET
		func() (tool.BaseTool, error) { return tools.NewHttpRequestTool() },
	}

	for _, build := range toolBuilders {
		t, err := build()
		if err != nil {
			return err
		}
		a.tools = append(a.tools, t)
	}

	// 添加 MCP 工具
	mcpTools := a.mcpMgr.GetTools()
	if len(mcpTools) > 0 {
		a.tools = append(a.tools, mcpTools...)
		zap.L().Info("已加载 MCP 工具", zap.Int("count", len(mcpTools)))
	}

	return nil
}

func (a *Agent) initReact() error {
	cfg := config.Get()
	maxStep := cfg.Agent.MaxStep
	if maxStep <= 0 {
		maxStep = 12 // 默认最大步数
	}
	agent, err := react.NewAgent(context.Background(), &react.AgentConfig{
		ToolCallingModel:   a.model,
		ToolsConfig:        compose.ToolsNodeConfig{Tools: a.tools, ExecuteSequentially: true},
		MaxStep:            maxStep,
		ToolReturnDirectly: map[string]struct{}{"stayQuiet": {}},
	})
	if err != nil {
		return err
	}
	a.react = agent
	return nil
}

// Start 启动
func (a *Agent) Start() {
	if a.learner != nil {
		a.learner.Start()
	}

	// 启动时从数据库加载历史消息到缓冲区
	a.loadBuffersFromDB()

	a.bot.OnMessage(a.onMessage)
	a.wg.Add(1)
	go a.thinkLoop()
	zap.L().Info("Agent 已启动")
}

// loadBuffersFromDB 从数据库加载消息日志到缓冲区
func (a *Agent) loadBuffersFromDB() {
	cfg := config.Get()
	for _, gc := range cfg.Groups {
		if !gc.Enabled {
			continue
		}

		// 获取缓冲区大小
		bufSize := cfg.Agent.MessageBufferSize
		if bufSize <= 0 {
			bufSize = 15
		}

		// 从数据库获取最近的消息
		logs := a.memory.GetRecentMessages(gc.GroupID, bufSize, 0)
		if len(logs) == 0 {
			continue
		}

		// 初始化缓冲区
		a.buffersMu.Lock()
		buf := utils.NewRingBuffer[*onebot.GroupMessage](bufSize)
		a.buffers[gc.GroupID] = buf

		// 填充缓冲区
		for _, log := range logs {
			msgID, _ := strconv.ParseInt(log.MessageID, 10, 64)

			// 还原合并转发内容
			var forwards []onebot.ForwardMessage
			if log.Forwards != "" {
				_ = sonic.UnmarshalString(log.Forwards, &forwards)
			}

			msg := &onebot.GroupMessage{
				MessageID:    msgID,
				GroupID:      log.GroupID,
				UserID:       log.UserID,
				Nickname:     log.Nickname,
				Content:      log.Content, // 这里使用解析后的内容作为 Content
				FinalContent: log.Content, // FinalContent 也是解析后的内容
				IsMentioned:  log.IsMentioned,
				Time:         log.CreatedAt,
				MessageType:  log.MsgType,
				Forwards:     forwards,
			}
			buf.Push(msg)
		}
		a.buffersMu.Unlock()

		zap.L().Info("已从数据库加载消息历史", zap.Int64("group_id", gc.GroupID), zap.Int("count", len(logs)))
	}
}

// Stop 停止
func (a *Agent) Stop() {
	if a.learner != nil {
		a.learner.Stop()
	}
	close(a.stopCh)
	a.wg.Wait()
	// 关闭 MCP 连接
	if a.mcpMgr != nil {
		a.mcpMgr.Close()
	}
	zap.L().Info("Agent 已停止")
}

func (a *Agent) onMessage(msg *onebot.GroupMessage) {
	cfg := config.Get()
	if !cfg.IsGroupEnabled(msg.GroupID) {
		return
	}

	// 检测是否通过名字或别名提及了沐沐
	isMentioned := msg.IsMentioned || a.persona.IsMentioned(msg.Content)

	// 序列化合并转发内容
	forwardsJSON := ""
	if len(msg.Forwards) > 0 {
		if b, err := sonic.MarshalString(msg.Forwards); err == nil {
			forwardsJSON = b
		}
	}

	// 解析消息内容（图片、视频、表情、回复等）
	parsedContent := a.parseMessageContent(msg)
	msg.FinalContent = parsedContent

	// 防止注入工具名字
	for _, t := range a.tools {
		info, _ := t.Info(context.Background())
		parsedContent = strings.ReplaceAll(parsedContent, info.Name, "\"危险指令，已屏蔽\"")
	}

	a.addBuffer(msg)
	_ = a.memory.AddMessage(memory.MessageLog{
		MessageID:   fmt.Sprintf("%d", msg.MessageID),
		GroupID:     msg.GroupID,
		UserID:      msg.UserID,
		Nickname:    msg.Nickname,
		Content:     parsedContent, // 使用解析后的内容
		MsgType:     msg.MessageType,
		IsMentioned: isMentioned,
		CreatedAt:   msg.Time,
		Forwards:    forwardsJSON,
	})

	if msg.UserID == a.bot.GetSelfID() {
		return
	}

	go a.updateMember(msg)

	// 如果被 @ 了，立即触发一次思考（跳过等待）
	if isMentioned {
		go a.concurrencyMgr.Submit(msg.GroupID, true)
	}
}

// parseMessageContent 解析消息内容（图片、视频、表情、回复等）
func (a *Agent) parseMessageContent(msg *onebot.GroupMessage) string {
	ctx := context.Background()

	// 构建回复信息
	replyInfo := ""
	if msg.Reply != nil {
		if msg.Reply.Content != "" {
			replyContent := []rune(msg.Reply.Content)
			if len(replyContent) > 50 {
				replyContent = replyContent[:50]
			}
			replyInfo = fmt.Sprintf(" [回复 #%d %s:\"%s\"]", msg.Reply.MessageID, msg.Reply.Nickname, string(replyContent))
		} else {
			replyInfo = fmt.Sprintf(" [回复 #%d]", msg.Reply.MessageID)
		}
	}

	// 构建消息内容（包含图片和表情描述）
	content := msg.Content

	// 处理表情
	for _, face := range msg.Faces {
		if face.Name != "" {
			content += fmt.Sprintf(" [表情:%s]", face.Name)
		} else if face.ID > 0 {
			content += fmt.Sprintf(" [表情:%d]", face.ID)
		} else {
			content += " [表情]"
		}
	}

	// 处理图片（调用 Vision 模型识别）
	for _, img := range msg.Images {
		if img.SubType == 1 {
			// 表情包类型
			var desc string
			if a.vision != nil && img.URL != "" {
				if d, err := a.vision.DescribeImage(ctx, img.URL); err == nil {
					desc = d
				}
			}
			if desc == "" && img.Summary != "" {
				desc = img.Summary
			}
			// 自动保存表情包
			if img.URL != "" && config.Get().Sticker.AutoSave {
				go a.autoSaveSticker(img.URL, desc)
			}
			if desc != "" {
				content += fmt.Sprintf(" [表情包 描述:%s]", desc)
			} else {
				content += " [表情包]"
			}
		} else {
			// 普通图片
			if a.vision != nil && img.URL != "" {
				if desc, err := a.vision.DescribeImage(ctx, img.URL); err == nil {
					content += " " + desc
				} else {
					content += " [图片]"
				}
			} else {
				content += " [图片]"
			}
		}
	}

	// 处理视频（调用 Vision 模型识别）
	for _, vid := range msg.Videos {
		if a.vision != nil && vid.URL != "" {
			if desc, err := a.vision.DescribeVideo(ctx, vid.URL); err == nil {
				content += " " + desc
			} else {
				content += " [视频]"
			}
		} else {
			content += " [视频]"
		}
	}

	var qid string
	if msg.UserID == config.Get().Persona.QQ {
		qid = "你"
	} else {
		qid = fmt.Sprintf("%d", msg.UserID)
	}

	// 构建完整消息行
	return fmt.Sprintf("[%s] #%d %s(%s):%s %s\n",
		msg.Time.Format("15:04:05"), msg.MessageID, msg.Nickname, qid, replyInfo, content)
}

func (a *Agent) addBuffer(msg *onebot.GroupMessage) {
	a.buffersMu.Lock()
	buf, ok := a.buffers[msg.GroupID]
	if !ok {
		// 确保缓冲区大小有效
		bufSize := config.Get().Agent.MessageBufferSize
		if bufSize <= 0 {
			bufSize = 15 // 默认缓冲区大小
		}
		buf = utils.NewRingBuffer[*onebot.GroupMessage](bufSize)
		a.buffers[msg.GroupID] = buf
	}
	a.buffersMu.Unlock()

	buf.Push(msg)
}

func (a *Agent) getBuffer(groupID int64) []*onebot.GroupMessage {
	a.buffersMu.RLock()
	buf, ok := a.buffers[groupID]
	a.buffersMu.RUnlock()

	if !ok || buf.IsEmpty() {
		return nil
	}
	return buf.GetAll()
}

func (a *Agent) updateMember(msg *onebot.GroupMessage) {
	p, err := a.memory.GetOrCreateMemberProfile(msg.UserID, msg.Nickname)
	if err != nil {
		zap.L().Error("获取成员画像失败", zap.Error(err))
		return
	}
	p.MsgCount++
	p.LastSpeak = msg.Time
	p.Nickname = msg.Nickname
	if err := a.memory.UpdateMemberProfile(p); err != nil {
		zap.L().Error("更新成员画像失败", zap.Error(err))
	}
}

func (a *Agent) thinkLoop() {
	defer a.wg.Done()
	ticker := time.NewTicker(time.Duration(config.Get().Agent.ThinkInterval) * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-a.stopCh:
			return
		case <-ticker.C:
			a.thinkCycle()
		}
	}
}

func (a *Agent) thinkCycle() {
	cfg := config.Get()
	for _, gc := range cfg.Groups {
		if !gc.Enabled {
			continue
		}
		msgs := a.getBuffer(gc.GroupID)
		if len(msgs) == 0 {
			continue
		}

		lastMsg := msgs[len(msgs)-1]

		// 如果该消息的时间不晚于最后处理时间，说明是旧消息，跳过
		a.processingMu.RLock()
		lastTime := a.lastProcessedTime[gc.GroupID]
		a.processingMu.RUnlock()
		if !lastTime.IsZero() && lastMsg.Time.Before(lastTime) {
			continue
		}

		// 如果最后一条消息是自己发的，跳过
		if lastMsg.UserID == a.bot.GetSelfID() {
			continue
		}

		// 如果最后一条消息是 @提及，已经在 onMessage 中触发了即时思考，这里跳过
		if a.persona.IsMentioned(lastMsg.Content) || lastMsg.IsMentioned {
			continue
		}

		if time.Since(lastMsg.Time) > time.Duration(cfg.Agent.ObserveWindow)*time.Second {
			continue
		}
		// 获取当前的发言概率（考虑时段规则）
		speakProb := a.getSpeakProbability(gc.GroupID)
		if rand.Float64() > speakProb {
			continue
		}
		a.concurrencyMgr.Submit(gc.GroupID, false)
	}
}

// getSpeakProbability 获取发言概率（考虑时段规则）
func (a *Agent) getSpeakProbability(groupID int64) float64 {
	cfg := config.Get()
	baseProb := cfg.Chat.TalkFrequency
	if !cfg.Chat.EnableTimeRules || len(cfg.Chat.TimeRules) == 0 {
		return baseProb
	}

	now := time.Now()
	hour := now.Hour()
	minute := now.Minute()
	currentMinutes := hour*60 + minute

	for _, rule := range cfg.Chat.TimeRules {
		// 检查是否适用于当前群（0表示全局）
		if rule.GroupID != 0 && rule.GroupID != groupID {
			continue
		}
		// 解析时间范围
		var startHour, startMin, endHour, endMin int
		if _, err := fmt.Sscanf(rule.TimeRange, "%d:%d-%d:%d", &startHour, &startMin, &endHour, &endMin); err != nil {
			continue
		}
		startMinutes := startHour*60 + startMin
		endMinutes := endHour*60 + endMin

		// 检查当前时间是否在范围内
		if startMinutes <= endMinutes {
			// 正常时间范围
			if currentMinutes >= startMinutes && currentMinutes < endMinutes {
				baseProb = rule.TalkValue // 使用时段配置的概率覆盖基础概率
				break                     // 找到匹配规则后跳出
			}
		} else {
			// 跨午夜的时间范围
			if currentMinutes >= startMinutes || currentMinutes < endMinutes {
				baseProb = rule.TalkValue
				break
			}
		}
	}

	// 防话痨限流逻辑
	limitCfg := config.Get().Chat.RateLimit
	if limitCfg.Enabled && limitCfg.PeriodSec > 0 && limitCfg.MaxMessages > 0 {
		startTime := time.Now().Add(-time.Duration(limitCfg.PeriodSec) * time.Second)
		count, err := a.memory.GetMessageCountByTime(groupID, a.bot.GetSelfID(), startTime)
		if err == nil {
			maxMsgs := float64(limitCfg.MaxMessages)
			current := float64(count)

			// 线性衰减系数
			// 当 current=0, decay=1.0; 当 current=max, decay=0.0
			var decay float64
			if current >= maxMsgs {
				decay = 0
			} else {
				decay = (maxMsgs - current) / maxMsgs
			}

			// 应用衰减
			oldProb := baseProb
			baseProb *= decay

			// 最小保底检查
			minProb := utils.ClampFloat64(limitCfg.MinProb, 0, 1)
			baseProb = utils.ClampFloat64(baseProb, minProb, 1)

			// 仅在触发衰减时打印日志
			if decay < 1.0 {
				zap.L().Debug("触发防话痨限制",
					zap.Int64("group_id", groupID),
					zap.Int64("recent_msgs", count),
					zap.Float64("decay", decay),
					zap.Float64("original_prob", oldProb),
					zap.Float64("new_prob", baseProb))
			}
		}
	}

	return baseProb
}

// think 提交思考任务
func (a *Agent) think(groupID int64, isMention bool) {
	if a.bot.IsSelfMuted(groupID) {
		return
	}
	// 并发锁：确保同一时间一个群只有一个思考进程
	a.processingMu.Lock()
	if a.processing[groupID] {
		a.processingMu.Unlock()
		return
	}
	a.processing[groupID] = true
	lastProcessedTime := a.lastProcessedTime[groupID]
	a.lastProcessedTime[groupID] = time.Now()
	a.processingMu.Unlock()

	defer func() {
		a.processingMu.Lock()
		a.processing[groupID] = false
		a.processingMu.Unlock()
	}()

	ctx := tools.WithToolContext(context.Background(), &tools.ToolContext{
		GroupID:   groupID,
		MemoryMgr: a.memory,
		Bot:       a.bot,
		SpeakCallback: func(gid int64, content string, replyTo int64, mentions []int64) (int64, error) {
			return a.doSpeak(gid, content, replyTo, mentions)
		},
		SendStickerCallback: func(gid int64, filePath string, description string) (int64, error) {
			return a.doSendSticker(gid, filePath, description)
		},
	})

	// 构建对话上下文
	chatContext := a.buildChatContext(groupID, lastProcessedTime)
	if chatContext == "" {
		return
	}

	// 构建动态 prompt 上下文
	promptCtx := &persona.PromptContext{
		GroupID: groupID,
	}

	// 主动记忆检索
	if config.Get().Agent.EnableActiveRetrieval {
		threshold := 0.7

		// 获取上下文消息内容
		msgs := a.getBuffer(groupID)
		var contentBuilder strings.Builder
		for _, m := range msgs {
			contentBuilder.WriteString(m.Content) // 使用语义内容字段
		}

		// 向量搜索
		if contentBuilder.Len() > 0 {
			memories, err := a.memory.SearchSimilarMemories(ctx, contentBuilder.String(), 0, threshold)
			if err != nil {
				zap.L().Warn("主动记忆检索失败", zap.Error(err))
			} else if len(memories) > 0 {
				promptCtx.RelatedMemories = memories
				zap.L().Debug("主动记忆检索成功", zap.Int("count", len(memories)))
			}
		}
	}

	// 获取当前情绪状态
	if mood, err := a.memory.GetMoodState(); err == nil {
		promptCtx.MoodState = &persona.MoodInfo{
			Valence:     mood.Valence,
			Energy:      mood.Energy,
			Sociability: mood.Sociability,
		}
	}

	// 注入黑话/梗的解释（AC自动机匹配）
	if a.jargonMgr != nil {
		promptCtx.JargonMatches = a.jargonMgr.Match(chatContext)
	}

	// 获取说话者信息
	memberInfo := a.getMemberInfo(groupID)

	// 构建消息
	systemPrompt := a.persona.GetSystemPrompt()

	// 添加群专属额外提示词
	groupExtra := ""
	if gc := config.Get().GetGroupConfig(groupID); gc != nil && gc.ExtraPrompt != "" {
		groupExtra = gc.ExtraPrompt
	}

	thinkPrompt := a.persona.GetThinkPrompt(promptCtx, chatContext, groupExtra, memberInfo)
	if isMention {
		thinkPrompt += "\n\n注意：有人提到你了，可能在找你说话，你可以看情况回复。"
	}

	// 调试：显示系统提示词
	if config.Get().Debug.ShowPrompt {
		zap.L().Debug("系统提示词", zap.String("prompt", systemPrompt))
		zap.L().Debug("思考提示词", zap.String("prompt", thinkPrompt))
	}

	msgs := []*schema.Message{
		schema.SystemMessage(systemPrompt),
		schema.UserMessage(thinkPrompt),
	}

	// 设置超时时间（默认60秒），防止LLM请求无限阻塞
	timeout := 60 * time.Second
	ctxWithTimeout, cancelTimeout := context.WithTimeout(ctx, timeout)
	defer cancelTimeout()

	result, err := a.react.Generate(ctxWithTimeout, msgs)
	if err != nil {
		// 区分是超时还是主动取消（stayQuiet）
		if errors.Is(ctxWithTimeout.Err(), context.DeadlineExceeded) {
			zap.L().Warn("思考超时", zap.Int64("group_id", groupID), zap.Duration("timeout", timeout))
		} else {
			zap.L().Error("思考失败", zap.Int64("group_id", groupID), zap.Error(err))
		}
	}

	// 记录 Agent 输出
	if config.Get().Debug.ShowThinking && result != nil && result.Content != "" {
		zap.L().Debug("Agent 输出", zap.Int64("group_id", groupID), zap.String("content", result.Content))
	}
}

// buildChatContext 构建聊天上下文
func (a *Agent) buildChatContext(groupID int64, lastProcessedTime time.Time) string {
	msgs := a.getBuffer(groupID)
	if len(msgs) == 0 {
		return ""
	}

	var b strings.Builder
	for _, m := range msgs {
		if !lastProcessedTime.IsZero() && m.Time.Before(lastProcessedTime) {
			b.WriteString("(OLD)")
		}
		b.WriteString(m.FinalContent)
	}
	return b.String()
}

// getMemberInfo 获取当前说话者信息
func (a *Agent) getMemberInfo(groupID int64) string {
	msgs := a.getBuffer(groupID)
	if len(msgs) == 0 {
		return ""
	}

	// 获取最后一个说话者的信息
	lastMsg := msgs[len(msgs)-1]
	profile, err := a.memory.GetMemberProfile(lastMsg.UserID)
	if err != nil {
		return ""
	}

	var parts []string
	parts = append(parts, fmt.Sprintf("昵称: %s", profile.Nickname))
	parts = append(parts, fmt.Sprintf("活跃度（0-1）: %.2f", profile.Activity))
	parts = append(parts, fmt.Sprintf("你与他的亲密度（0-1）: %.2f", profile.Intimacy))
	if profile.SpeakStyle != "" {
		parts = append(parts, fmt.Sprintf("说话风格: %s", profile.SpeakStyle))
	}
	if profile.Interests != "" {
		parts = append(parts, fmt.Sprintf("兴趣: %s", profile.Interests))
	}
	if !profile.LastSpeak.IsZero() {
		parts = append(parts, fmt.Sprintf("上次发言时间: %s", profile.LastSpeak.Format(time.DateTime)))
	}
	return strings.Join(parts, ", ")
}

// doSpeak 执行发言，返回消息ID
func (a *Agent) doSpeak(groupID int64, content string, replyTo int64, mentions []int64) (int64, error) {
	// 模拟打字延迟
	cfg := config.Get()
	if cfg.Chat.TypingSimulation {
		typingSpeed := cfg.Chat.TypingSpeed
		if typingSpeed <= 0 {
			typingSpeed = 6
		}
		delay := time.Duration(float64(len([]rune(content)))/float64(typingSpeed)*1000) * time.Millisecond
		if delay > 5*time.Second {
			delay = 5 * time.Second
		}
		if delay < 500*time.Millisecond {
			delay = 500 * time.Millisecond
		}
		time.Sleep(delay)
	}

	msgID, err := a.bot.SendGroupMessage(groupID, content, replyTo, mentions)
	if err != nil {
		zap.L().Error("发言失败", zap.Int64("group_id", groupID), zap.Error(err))
		return 0, err
	}

	msg := &onebot.GroupMessage{
		MessageID:   msgID,
		GroupID:     groupID,
		UserID:      a.bot.GetSelfID(),
		Nickname:    a.persona.GetName(),
		Content:     content,
		Time:        time.Now(),
		MessageType: "group",
	}
	a.onMessage(msg)
	zap.L().Info("发言成功", zap.Int64("group_id", groupID), zap.String("content", content))
	return msgID, nil
}

// doSendSticker 执行发送表情包，并记录消息
func (a *Agent) doSendSticker(groupID int64, filePath string, description string) (int64, error) {
	msgID, err := a.bot.SendImageMessage(groupID, filePath, true)
	if err != nil {
		zap.L().Error("发送表情包失败", zap.Int64("group_id", groupID), zap.String("path", filePath), zap.Error(err))
		return 0, err
	}

	var content string
	if description != "" {
		content = fmt.Sprintf("[表情包:%s]", description)
	} else {
		content = "[表情包]"
	}

	msg := &onebot.GroupMessage{
		MessageID:   msgID,
		GroupID:     groupID,
		UserID:      a.bot.GetSelfID(),
		Nickname:    a.persona.GetName(),
		Content:     content,
		Time:        time.Now(),
		MessageType: "group",
	}
	a.onMessage(msg)
	zap.L().Info("发送表情包成功", zap.Int64("group_id", groupID), zap.String("desc", description))
	return msgID, nil
}

// autoSaveSticker 自动保存表情包（异步执行）
func (a *Agent) autoSaveSticker(url string, description string) {
	if url == "" {
		return
	}

	// 获取配置
	cfg := config.Get()
	storagePath := cfg.Sticker.StoragePath
	if storagePath == "" {
		storagePath = "./stickers"
	}
	maxSizeMB := cfg.Sticker.MaxSizeMB
	if maxSizeMB <= 0 {
		maxSizeMB = 2
	}

	// 下载图片
	result, err := utils.DownloadImage(url, storagePath, maxSizeMB)
	if err != nil {
		zap.L().Debug("下载表情包失败", zap.String("url", url), zap.Error(err))
		return
	}

	// 如果没有描述，使用默认描述
	if description == "" {
		description = "未描述的表情包"
	}

	// 保存到数据库
	sticker := &memory.Sticker{
		FileName:    result.FileName,
		FileHash:    result.FileHash,
		Description: description,
	}

	isDuplicate, err := a.memory.SaveSticker(sticker)
	if err != nil {
		// 保存失败，删除已下载的文件
		_ = os.Remove(result.FilePath)
		zap.L().Warn("保存表情包失败", zap.Error(err))
		return
	}

	if isDuplicate {
		// 已存在，删除刚下载的文件
		_ = os.Remove(result.FilePath)
		zap.L().Debug("表情包已存在，跳过保存", zap.String("hash", result.FileHash))
		return
	}

	zap.L().Info("自动保存表情包", zap.Uint("id", sticker.ID), zap.String("desc", description))
}
