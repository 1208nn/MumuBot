package agent

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"mumu-bot/internal/memory"
	"mumu-bot/internal/onebot"

	"github.com/cloudwego/eino/components/model"
	agentreact "github.com/cloudwego/eino/flow/agent/react"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

const (
	topicPromptTailLines    = 4
	maxTopicPersistAttempts = 3
)

type topicMemoryStore interface {
	ListActiveTopicThreads(ctx context.Context, groupID int64) ([]memory.TopicThread, error)
	ListArchivedTopicThreadsNeedingSummary(ctx context.Context, groupID int64) ([]memory.TopicThread, error)
	ListRecentTopicMessages(ctx context.Context, topicID uint, limit int) ([]memory.MessageLog, error)
	ListRecentTopicParticipants(ctx context.Context, topicID uint, limit int) ([]memory.TopicParticipantRef, error)
	CountTopicMessagesAfterSummary(ctx context.Context, topicID uint) (int, error)
	GetTopicMessagesAfterSummary(ctx context.Context, topicID uint, limit int) ([]memory.MessageLog, error)
	UpdateTopicSummary(ctx context.Context, topicID uint, summary memory.TopicSummaryV1, summaryUntil uint, capturedAt time.Time) error
	GetTopicThread(ctx context.Context, topicID uint) (*memory.TopicThread, error)
	SearchArchivedTopicThreadHits(ctx context.Context, query string, groupID int64, topK int, threshold float64) ([]memory.TopicThreadSearchHit, error)
	SaveMessageLogWithTopic(ctx context.Context, input memory.SaveMessageLogWithTopicInput) (memory.SaveMessageLogWithTopicResult, error)
	ArchiveTopicThreadForRepair(ctx context.Context, groupID int64, topicID uint) error
	SyncTopicThreadVector(ctx context.Context, topicID uint) error
	GetMessageLogByID(messageID string) (*memory.MessageLog, error)
	AddMessage(msg memory.MessageLog) error
}

type topicSummaryFunc func(ctx context.Context, oldSummary memory.TopicSummaryV1, newMessages []memory.MessageLog) (memory.TopicSummaryV1, error)

type topicRuntimeState struct {
	thread       memory.TopicThread
	tail         []*onebot.GroupMessage
	participants []memory.TopicParticipantRef
	dirty        bool
	pendingCount int
	queued       bool
}

type topicGroupState struct {
	topics map[uint]*topicRuntimeState
}

type topicSummaryTask struct {
	groupID int64
	topicID uint
}

type topicSummaryCall struct {
	done  chan struct{}
	topic *memory.TopicThread
	err   error
}

type TopicAssignInput struct {
	GroupID int64
	Message *onebot.GroupMessage
}

type TopicAssignDecision struct {
	TopicID       uint
	MatchReason   string
	MatchScore    float64
	SlotAction    memory.TopicSlotAction
	VictimTopicID uint
}

type TopicPromptContext struct {
	Prompt         string
	RetrievalQuery string
	TopicIDs       []uint
	MainTopicID    uint
}

type PersistMessageInput struct {
	Message      *onebot.GroupMessage
	IsMentioned  bool
	ForwardsJSON string
}

type PersistMessageResult struct {
	TopicID      uint
	MessageLogID uint
	Fallback     bool
}

type TopicManager struct {
	ctx              context.Context
	store            topicMemoryStore
	summaryExtractor *agentreact.Agent
	summaryFn        topicSummaryFunc
	groupStates      map[int64]*topicGroupState
	groupLocks       map[int64]*sync.Mutex

	statesMu sync.RWMutex
	locksMu  sync.Mutex

	summaryQueue chan topicSummaryTask
	stopCh       chan struct{}
	wg           sync.WaitGroup

	summaryMu       sync.Mutex
	summaryInFlight map[uint]*topicSummaryCall
}

func NewTopicManager(ctx context.Context, store topicMemoryStore, chatModel model.ToolCallingChatModel) *TopicManager {
	if ctx == nil {
		ctx = context.Background()
	}
	tm := &TopicManager{
		ctx:             ctx,
		store:           store,
		groupStates:     make(map[int64]*topicGroupState),
		groupLocks:      make(map[int64]*sync.Mutex),
		summaryQueue:    make(chan topicSummaryTask, 128),
		stopCh:          make(chan struct{}),
		summaryInFlight: make(map[uint]*topicSummaryCall),
	}
	if summaryExtractor, err := newTopicSummaryExtractor(chatModel); err != nil {
		zap.L().Warn("初始化话题摘要提取器失败", zap.Error(err))
	} else {
		tm.summaryExtractor = summaryExtractor
	}
	tm.summaryFn = tm.generateSummary
	tm.wg.Add(1)
	go tm.summaryWorker()
	return tm
}

func (tm *TopicManager) Close() {
	select {
	case <-tm.stopCh:
		return
	default:
		close(tm.stopCh)
	}
	tm.wg.Wait()
}

func (tm *TopicManager) WithGroupLock(groupID int64, fn func() error) error {
	lock := tm.getGroupLock(groupID)
	lock.Lock()
	defer lock.Unlock()
	return fn()
}

func (tm *TopicManager) LoadFromDB(groupIDs []int64) error {
	for _, groupID := range groupIDs {
		if groupID == 0 {
			continue
		}

		for {
			var victimTopicID uint
			if err := tm.WithGroupLock(groupID, func() error {
				activeTopics, err := tm.store.ListActiveTopicThreads(tm.ctx, groupID)
				if err != nil {
					return err
				}
				if len(activeTopics) <= memory.MaxActiveTopicThreadsPerGroup {
					victimTopicID = 0
					return nil
				}
				victimTopicID = memory.OldestActiveTopicID(activeTopics)
				return nil
			}); err != nil {
				return err
			}
			if victimTopicID == 0 {
				break
			}

			if _, err := tm.ensureTopicSummaryFresh(tm.ctx, victimTopicID); err != nil {
				return err
			}

			archived := false
			err := tm.WithGroupLock(groupID, func() error {
				activeTopics, err := tm.store.ListActiveTopicThreads(tm.ctx, groupID)
				if err != nil {
					return err
				}
				if len(activeTopics) <= memory.MaxActiveTopicThreadsPerGroup {
					return nil
				}
				if memory.OldestActiveTopicID(activeTopics) != victimTopicID {
					return memory.ErrTopicStateChanged
				}
				if err := tm.store.ArchiveTopicThreadForRepair(tm.ctx, groupID, victimTopicID); err != nil {
					return err
				}
				archived = true
				return nil
			})
			if errors.Is(err, memory.ErrTopicStateChanged) {
				continue
			}
			if err != nil {
				return err
			}
			if !archived {
				break
			}
			if err := tm.syncTopicVectors(tm.ctx, victimTopicID); err != nil {
				zap.L().Warn("启动修复后的话题向量同步失败", zap.Int64("group_id", groupID), zap.Uint("topic_id", victimTopicID), zap.Error(err))
			}
		}

		var dirtyArchivedIDs []uint
		if err := tm.WithGroupLock(groupID, func() error {
			activeTopics, err := tm.store.ListActiveTopicThreads(tm.ctx, groupID)
			if err != nil {
				return err
			}

			groupState := tm.ensureGroupState(groupID)
			groupState.topics = make(map[uint]*topicRuntimeState, len(activeTopics))
			for _, topic := range activeTopics {
				state := &topicRuntimeState{
					thread:       topic,
					tail:         tm.loadTopicTailMessages(tm.ctx, topic.ID),
					participants: tm.loadTopicParticipants(tm.ctx, topic.ID),
					dirty:        topic.SummaryUntilMessageLogID < topic.LastMessageLogID,
					pendingCount: 0,
				}
				if state.dirty {
					if count, err := tm.store.CountTopicMessagesAfterSummary(tm.ctx, topic.ID); err == nil {
						state.pendingCount = count
					}
				}
				groupState.topics[topic.ID] = state
			}

			dirtyArchived, err := tm.store.ListArchivedTopicThreadsNeedingSummary(tm.ctx, groupID)
			if err != nil {
				return err
			}
			dirtyArchivedIDs = make([]uint, 0, len(dirtyArchived))
			for _, topic := range dirtyArchived {
				dirtyArchivedIDs = append(dirtyArchivedIDs, topic.ID)
			}
			return nil
		}); err != nil {
			return err
		}
		for _, topicID := range uniqueTopicIDs(dirtyArchivedIDs) {
			tm.enqueueSummaryTask(topicSummaryTask{groupID: groupID, topicID: topicID})
		}
	}
	return nil
}

func (tm *TopicManager) Assign(ctx context.Context, input TopicAssignInput) (TopicAssignDecision, error) {
	archivedHits := tm.searchArchivedTopicHits(ctx, input.GroupID, input.Message)
	var decision TopicAssignDecision
	err := tm.WithGroupLock(input.GroupID, func() error {
		var err error
		decision, err = tm.assignLocked(ctx, input, archivedHits)
		return err
	})
	return decision, err
}

func (tm *TopicManager) BuildPromptContext(ctx context.Context, groupID int64, buffer []*onebot.GroupMessage) (TopicPromptContext, error) {
	if len(buffer) == 0 {
		return TopicPromptContext{}, nil
	}

	archivedHits := tm.searchArchivedTopicHits(ctx, groupID, buffer[len(buffer)-1])
	var selectedIDs []uint
	if err := tm.WithGroupLock(groupID, func() error {
		var err error
		selectedIDs, err = tm.selectPromptTopicIDsLocked(ctx, groupID, buffer[len(buffer)-1], archivedHits)
		return err
	}); err != nil {
		return TopicPromptContext{}, err
	}

	if len(selectedIDs) == 0 {
		return TopicPromptContext{
			RetrievalQuery: messageTopicText(buffer[len(buffer)-1]),
		}, nil
	}
	if err := tm.EnsureSummariesReady(ctx, selectedIDs); err != nil {
		return TopicPromptContext{}, err
	}

	var promptCtx TopicPromptContext
	err := tm.WithGroupLock(groupID, func() error {
		var err error
		promptCtx, err = tm.renderPromptContextLocked(ctx, groupID, buffer, selectedIDs)
		return err
	})
	return promptCtx, err
}

func (tm *TopicManager) EnsureSummariesReady(ctx context.Context, topicIDs []uint) error {
	for _, topicID := range uniqueTopicIDs(topicIDs) {
		topic, err := tm.ensureTopicSummaryFresh(ctx, topicID)
		if err != nil {
			return err
		}
		if topic != nil {
			if err := tm.refreshTopicState(ctx, topic.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

func (tm *TopicManager) PersistMessage(ctx context.Context, input PersistMessageInput) (PersistMessageResult, error) {
	if input.Message == nil {
		return PersistMessageResult{}, nil
	}

	msg := input.Message
	baseMessage := memory.MessageLog{
		MessageID:       fmt.Sprintf("%d", msg.MessageID),
		GroupID:         msg.GroupID,
		UserID:          msg.UserID,
		Nickname:        msg.Nickname,
		Content:         msg.FinalContent,
		OriginalContent: msg.Content,
		MsgType:         msg.MessageType,
		IsMentioned:     input.IsMentioned,
		CreatedAt:       msg.Time,
		Forwards:        input.ForwardsJSON,
	}
	assignInput := TopicAssignInput{
		GroupID: msg.GroupID,
		Message: cloneGroupMessage(msg),
	}

	var lastStateErr error
	for attempt := 0; attempt < maxTopicPersistAttempts; attempt++ {
		archivedHits := tm.searchArchivedTopicHits(ctx, msg.GroupID, msg)
		var (
			decision     TopicAssignDecision
			saveResult   memory.SaveMessageLogWithTopicResult
			refreshTopic uint
		)

		err := tm.WithGroupLock(msg.GroupID, func() error {
			var err error
			decision, err = tm.assignLocked(ctx, assignInput, archivedHits)
			if err != nil {
				return err
			}

			if decision.VictimTopicID != 0 {
				state := tm.lookupTopicStateLocked(msg.GroupID, decision.VictimTopicID)
				if state == nil || state.thread.SummaryUntilMessageLogID < state.thread.LastMessageLogID {
					refreshTopic = decision.VictimTopicID
					return nil
				}
			}

			saveInput := memory.SaveMessageLogWithTopicInput{
				GroupID:         msg.GroupID,
				Message:         baseMessage,
				TopicID:         decision.TopicID,
				ExpectedTopicID: decision.TopicID,
				MatchReason:     decision.MatchReason,
				MatchScore:      decision.MatchScore,
				SlotAction:      decision.SlotAction,
				VictimTopicID:   decision.VictimTopicID,
			}

			saveResult, err = tm.store.SaveMessageLogWithTopic(ctx, saveInput)
			if err != nil {
				return err
			}
			return tm.applyMessageDecisionLocked(ctx, msg.GroupID, decision, saveResult, msg)
		})
		if refreshTopic != 0 {
			if _, err := tm.ensureTopicSummaryFresh(ctx, refreshTopic); err != nil {
				zap.L().Warn("victim 话题补摘要失败，降级为普通消息入库", zap.Int64("group_id", msg.GroupID), zap.Uint("topic_id", refreshTopic), zap.Int64("message_id", msg.MessageID), zap.Error(err))
				return tm.persistMessageWithoutTopic(baseMessage)
			}
			if err := tm.refreshTopicState(ctx, refreshTopic); err != nil {
				zap.L().Warn("victim 话题运行态同步失败，降级为普通消息入库", zap.Int64("group_id", msg.GroupID), zap.Uint("topic_id", refreshTopic), zap.Int64("message_id", msg.MessageID), zap.Error(err))
				return tm.persistMessageWithoutTopic(baseMessage)
			}
			continue
		}
		if err == nil {
			if err := tm.syncMessageTopicVectors(ctx, decision, saveResult); err != nil {
				zap.L().Warn("话题向量同步失败", zap.Int64("group_id", msg.GroupID), zap.Uint("topic_id", saveResult.TopicID), zap.Error(err))
			}
			return PersistMessageResult{
				TopicID:      saveResult.TopicID,
				MessageLogID: saveResult.MessageLogID,
			}, nil
		}
		if errors.Is(err, memory.ErrTopicStateChanged) {
			lastStateErr = err
			continue
		}
		return PersistMessageResult{}, err
	}

	if lastStateErr != nil {
		zap.L().Warn("话题决策在重试后仍不稳定，降级为普通消息入库", zap.Int64("group_id", msg.GroupID), zap.Int64("message_id", msg.MessageID), zap.Error(lastStateErr))
	}
	return tm.persistMessageWithoutTopic(baseMessage)
}

func (tm *TopicManager) persistMessageWithoutTopic(message memory.MessageLog) (PersistMessageResult, error) {
	if err := tm.store.AddMessage(message); err != nil {
		return PersistMessageResult{}, err
	}
	saved, err := tm.store.GetMessageLogByID(message.MessageID)
	if err != nil {
		return PersistMessageResult{Fallback: true}, nil
	}
	return PersistMessageResult{
		MessageLogID: saved.ID,
		Fallback:     true,
	}, nil
}

func (tm *TopicManager) syncMessageTopicVectors(ctx context.Context, decision TopicAssignDecision, result memory.SaveMessageLogWithTopicResult) error {
	topicIDs := make([]uint, 0, 2)
	if result.ArchivedTopicID != 0 {
		topicIDs = append(topicIDs, result.ArchivedTopicID)
	}
	if decision.SlotAction == memory.TopicSlotActionReopen && result.TopicID != 0 {
		topicIDs = append(topicIDs, result.TopicID)
	}
	return tm.syncTopicVectors(ctx, topicIDs...)
}

func (tm *TopicManager) syncTopicVectors(ctx context.Context, topicIDs ...uint) error {
	var syncErr error
	for _, topicID := range uniqueTopicIDs(topicIDs) {
		if topicID == 0 {
			continue
		}
		if err := tm.store.SyncTopicThreadVector(ctx, topicID); err != nil {
			if syncErr == nil {
				syncErr = err
			}
		}
	}
	return syncErr
}

func (tm *TopicManager) assignLocked(ctx context.Context, input TopicAssignInput, archivedHits []memory.TopicThreadSearchHit) (TopicAssignDecision, error) {
	if input.Message == nil {
		return TopicAssignDecision{}, nil
	}

	activeTopics := tm.activeTopicsLocked(input.GroupID)
	candidates, err := tm.collectCandidatesLocked(ctx, input.GroupID, input.Message, archivedHits)
	if err != nil {
		return TopicAssignDecision{}, err
	}

	decision := chooseTopicDecision(candidates, activeTopics, topicReuseThreshold, memory.MaxActiveTopicThreadsPerGroup)
	matchReason := "new_topic"
	matchScore := 0.0
	if decision.SlotAction != memory.TopicSlotActionCreate {
		for _, candidate := range candidates {
			if candidate.TopicID != decision.TopicID {
				continue
			}
			matchScore = scoreTopicCandidate(candidate)
			switch {
			case candidate.ReplyMatched:
				matchReason = "reply_match"
			case candidate.Status == memory.TopicThreadStatusArchived:
				matchReason = "archived_match"
			default:
				matchReason = "active_match"
			}
			break
		}
	}
	return TopicAssignDecision{
		TopicID:       decision.TopicID,
		MatchReason:   matchReason,
		MatchScore:    matchScore,
		SlotAction:    decision.SlotAction,
		VictimTopicID: decision.VictimTopicID,
	}, nil
}

func (tm *TopicManager) selectPromptTopicIDsLocked(ctx context.Context, groupID int64, latest *onebot.GroupMessage, archivedHits []memory.TopicThreadSearchHit) ([]uint, error) {
	candidates, err := tm.collectCandidatesLocked(ctx, groupID, latest, archivedHits)
	if err != nil {
		return nil, err
	}
	sorted := sortTopicCandidates(candidates)

	selectedIDs := make([]uint, 0, len(sorted))
	for _, candidate := range sorted {
		score := scoreTopicCandidate(candidate)
		if !candidate.ReplyMatched && score < 0.58 {
			continue
		}
		selectedIDs = append(selectedIDs, candidate.TopicID)
	}
	return uniqueTopicIDs(selectedIDs), nil
}

func (tm *TopicManager) renderPromptContextLocked(ctx context.Context, groupID int64, buffer []*onebot.GroupMessage, selectedIDs []uint) (TopicPromptContext, error) {
	if len(buffer) == 0 {
		return TopicPromptContext{}, nil
	}

	latest := buffer[len(buffer)-1]
	var builder strings.Builder
	var injected []uint
	for _, topicID := range uniqueTopicIDs(selectedIDs) {
		state := tm.lookupTopicStateLocked(groupID, topicID)
		var topic *memory.TopicThread
		if state != nil {
			topicCopy := state.thread
			topic = &topicCopy
		} else {
			var err error
			topic, err = tm.store.GetTopicThread(ctx, topicID)
			if err != nil {
				return TopicPromptContext{}, err
			}
		}
		section := renderTopicPromptSection(topic, state)
		if section == "" {
			continue
		}
		if builder.Len() > 0 && len([]rune(builder.String()))+len([]rune(section)) > 2200 {
			break
		}
		if builder.Len() > 0 {
			builder.WriteString("\n\n")
		}
		builder.WriteString(section)
		injected = append(injected, topicID)
	}

	retrievalQuery := tm.buildRetrievalQueryLocked(ctx, groupID, injected, latest)
	mainTopicID := uint(0)
	if len(injected) > 0 {
		mainTopicID = injected[0]
	}

	return TopicPromptContext{
		Prompt:         builder.String(),
		RetrievalQuery: retrievalQuery,
		TopicIDs:       injected,
		MainTopicID:    mainTopicID,
	}, nil
}

func (tm *TopicManager) ensureTopicSummaryFresh(ctx context.Context, topicID uint) (*memory.TopicThread, error) {
	call, leader := tm.beginTopicSummaryCall(topicID)
	if !leader {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-call.done:
			return copyTopicThread(call.topic), call.err
		}
	}

	topic, err := tm.ensureTopicSummaryFreshOnce(ctx, topicID)
	tm.finishTopicSummaryCall(topicID, call, topic, err)
	return copyTopicThread(topic), err
}

func (tm *TopicManager) ensureTopicSummaryFreshOnce(ctx context.Context, topicID uint) (*memory.TopicThread, error) {
	topic, err := tm.store.GetTopicThread(ctx, topicID)
	if err != nil {
		return nil, err
	}
	if topic.SummaryUntilMessageLogID >= topic.LastMessageLogID {
		return copyTopicThread(topic), nil
	}

	current := *topic
	for current.SummaryUntilMessageLogID < current.LastMessageLogID {
		pendingLogs, err := tm.store.GetTopicMessagesAfterSummary(ctx, topicID, 100)
		if err != nil {
			return nil, err
		}
		if len(pendingLogs) == 0 {
			break
		}

		oldSummary := memory.ParseTopicSummary(current.SummaryJSON)
		summaryCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		summary, err := tm.summaryFn(summaryCtx, oldSummary, pendingLogs)
		cancel()
		if err != nil {
			return nil, err
		}
		summaryUntil := pendingLogs[len(pendingLogs)-1].ID
		if err := tm.store.UpdateTopicSummary(ctx, topicID, summary, summaryUntil, time.Now()); err != nil {
			return nil, err
		}

		refreshed, err := tm.store.GetTopicThread(ctx, topicID)
		if err != nil {
			return nil, err
		}
		current = *refreshed
	}
	if current.SummaryUntilMessageLogID < current.LastMessageLogID {
		return nil, fmt.Errorf("topic %d summary still stale after refresh: %d < %d", topicID, current.SummaryUntilMessageLogID, current.LastMessageLogID)
	}

	return copyTopicThread(&current), nil
}

func (tm *TopicManager) beginTopicSummaryCall(topicID uint) (*topicSummaryCall, bool) {
	tm.summaryMu.Lock()
	defer tm.summaryMu.Unlock()

	if call, ok := tm.summaryInFlight[topicID]; ok {
		return call, false
	}

	call := &topicSummaryCall{done: make(chan struct{})}
	tm.summaryInFlight[topicID] = call
	return call, true
}

func (tm *TopicManager) finishTopicSummaryCall(topicID uint, call *topicSummaryCall, topic *memory.TopicThread, err error) {
	tm.summaryMu.Lock()
	defer tm.summaryMu.Unlock()

	call.topic = copyTopicThread(topic)
	call.err = err
	close(call.done)
	delete(tm.summaryInFlight, topicID)
}

func (tm *TopicManager) summaryWorker() {
	defer tm.wg.Done()
	for {
		select {
		case <-tm.stopCh:
			return
		case task := <-tm.summaryQueue:
			topic, err := tm.ensureTopicSummaryFresh(tm.ctx, task.topicID)
			if err != nil {
				zap.L().Warn("刷新话题摘要失败", zap.Int64("group_id", task.groupID), zap.Uint("topic_id", task.topicID), zap.Error(err))
				_ = tm.WithGroupLock(task.groupID, func() error {
					if state := tm.lookupTopicStateLocked(task.groupID, task.topicID); state != nil {
						state.queued = false
					}
					return nil
				})
				continue
			}
			if topic != nil {
				if err := tm.refreshTopicState(tm.ctx, topic.ID); err != nil {
					zap.L().Warn("同步话题运行态失败", zap.Int64("group_id", task.groupID), zap.Uint("topic_id", task.topicID), zap.Error(err))
				}
			}
		}
	}
}

func (tm *TopicManager) syncTopicStateLocked(ctx context.Context, groupID int64, topic memory.TopicThread) {
	state := tm.ensureTopicStateLocked(groupID, topic.ID)
	state.thread = topic
	state.dirty = topic.SummaryUntilMessageLogID < topic.LastMessageLogID
	state.queued = false
	if state.dirty {
		if remainingCount, err := tm.store.CountTopicMessagesAfterSummary(ctx, topic.ID); err == nil {
			state.pendingCount = remainingCount
		}
	} else {
		state.pendingCount = 0
	}
	if topic.Status == memory.TopicThreadStatusActive {
		state.tail = tm.loadTopicTailMessages(ctx, topic.ID)
		state.participants = tm.loadTopicParticipants(ctx, topic.ID)
		return
	}
	groupState := tm.ensureGroupState(groupID)
	delete(groupState.topics, topic.ID)
}

func (tm *TopicManager) collectCandidatesLocked(ctx context.Context, groupID int64, msg *onebot.GroupMessage, archivedHits []memory.TopicThreadSearchHit) ([]topicCandidate, error) {
	query := messageTopicText(msg)
	replyTopicID := tm.resolveReplyTopicID(ctx, msg)
	candidates := make([]topicCandidate, 0)
	seen := make(map[uint]struct{})

	for _, topic := range tm.activeTopicsLocked(groupID) {
		state := tm.lookupTopicStateLocked(groupID, topic.ID)
		candidate := topicCandidate{
			TopicID:            topic.ID,
			Status:             topic.Status,
			ReplyMatched:       replyTopicID != 0 && replyTopicID == topic.ID,
			SemanticScore:      maxFloat(topicSemanticSimilarity(query, topic, state), 0.15),
			ParticipantOverlap: topicParticipantOverlap(msg, topic, state),
			KeywordContinuity:  topicKeywordContinuity(query, topic),
			LastMessageLogID:   topic.LastMessageLogID,
		}
		candidates = append(candidates, candidate)
		seen[topic.ID] = struct{}{}
	}

	if strings.TrimSpace(query) != "" {
		for _, hit := range archivedHits {
			if _, ok := seen[hit.Topic.ID]; ok {
				continue
			}
			archivedState := &topicRuntimeState{
				thread:       hit.Topic,
				participants: tm.loadTopicParticipants(ctx, hit.Topic.ID),
			}
			candidates = append(candidates, topicCandidate{
				TopicID:            hit.Topic.ID,
				Status:             hit.Topic.Status,
				ReplyMatched:       replyTopicID != 0 && replyTopicID == hit.Topic.ID,
				SemanticScore:      maxFloat(hit.Score, topicKeywordContinuity(query, hit.Topic)),
				ParticipantOverlap: topicParticipantOverlap(msg, hit.Topic, archivedState),
				KeywordContinuity:  topicKeywordContinuity(query, hit.Topic),
				LastMessageLogID:   hit.Topic.LastMessageLogID,
			})
			seen[hit.Topic.ID] = struct{}{}
		}
	}

	if replyTopicID != 0 {
		if _, ok := seen[replyTopicID]; !ok {
			topic, err := tm.store.GetTopicThread(ctx, replyTopicID)
			if err == nil && topic.GroupID == groupID {
				replyState := tm.lookupTopicStateLocked(groupID, topic.ID)
				if replyState == nil {
					replyState = &topicRuntimeState{
						thread:       *topic,
						participants: tm.loadTopicParticipants(ctx, topic.ID),
					}
				}
				candidates = append(candidates, topicCandidate{
					TopicID:            topic.ID,
					Status:             topic.Status,
					ReplyMatched:       true,
					SemanticScore:      1,
					ParticipantOverlap: topicParticipantOverlap(msg, *topic, replyState),
					KeywordContinuity:  topicKeywordContinuity(query, *topic),
					LastMessageLogID:   topic.LastMessageLogID,
				})
			}
		}
	}

	return candidates, nil
}

func (tm *TopicManager) searchArchivedTopicHits(ctx context.Context, groupID int64, msg *onebot.GroupMessage) []memory.TopicThreadSearchHit {
	query := strings.TrimSpace(messageTopicText(msg))
	if query == "" {
		return nil
	}
	hits, err := tm.store.SearchArchivedTopicThreadHits(ctx, query, groupID, 6, 0.45)
	if err != nil {
		zap.L().Warn("检索归档话题失败", zap.Int64("group_id", groupID), zap.String("query", query), zap.Error(err))
		return nil
	}
	return hits
}

func (tm *TopicManager) resolveReplyTopicID(ctx context.Context, msg *onebot.GroupMessage) uint {
	if msg == nil || msg.Reply == nil || msg.Reply.MessageID == 0 {
		return 0
	}
	log, err := tm.store.GetMessageLogByID(strconv.FormatInt(msg.Reply.MessageID, 10))
	if err != nil {
		return 0
	}
	return log.TopicThreadID
}

func (tm *TopicManager) buildRetrievalQueryLocked(ctx context.Context, groupID int64, topicIDs []uint, latest *onebot.GroupMessage) string {
	if len(topicIDs) == 0 {
		return messageTopicText(latest)
	}
	topic, err := tm.store.GetTopicThread(ctx, topicIDs[0])
	if err != nil {
		return ""
	}
	summary := memory.ParseTopicSummary(topic.SummaryJSON)
	parts := []string{strings.TrimSpace(summary.Title), strings.TrimSpace(summary.Gist)}
	if len(summary.Facts) > 0 {
		parts = append(parts, strings.Join(summary.Facts, "\n"))
	}
	if len(summary.OpenLoops) > 0 {
		parts = append(parts, strings.Join(summary.OpenLoops, "\n"))
	}
	if state := tm.lookupTopicStateLocked(groupID, topic.ID); state != nil {
		tail := renderTopicTailLines(state.tail, topicPromptTailLines)
		if tail != "" {
			parts = append(parts, tail)
		}
	}
	if latestText := messageTopicText(latest); latestText != "" {
		parts = append(parts, latestText)
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

func (tm *TopicManager) recordMessageLocked(groupID int64, topicID uint, msg *onebot.GroupMessage, messageLogID uint) {
	if topicID == 0 || msg == nil {
		return
	}
	state := tm.ensureTopicStateLocked(groupID, topicID)
	state.thread.ID = topicID
	state.thread.GroupID = groupID
	state.thread.Status = memory.TopicThreadStatusActive
	state.thread.LastMessageLogID = messageLogID
	state.dirty = true
	state.pendingCount++
	state.tail = appendTopicTail(state.tail, cloneGroupMessage(msg))
	if msg.UserID != 0 {
		updatedParticipants := make([]memory.TopicParticipantRef, 0, memory.TopicTailKeepMessages)
		updatedParticipants = append(updatedParticipants, memory.TopicParticipantRef{
			UserID:   msg.UserID,
			Nickname: msg.Nickname,
		})
		for _, participant := range state.participants {
			if participant.UserID == msg.UserID {
				continue
			}
			updatedParticipants = append(updatedParticipants, participant)
			if len(updatedParticipants) >= memory.TopicTailKeepMessages {
				break
			}
		}
		state.participants = updatedParticipants
	}
	if state.pendingCount >= memory.TopicSummaryTriggerMessages {
		tm.enqueueSummaryLocked(groupID, topicID, state)
	}
}

func (tm *TopicManager) applyMessageDecisionLocked(ctx context.Context, groupID int64, _ TopicAssignDecision, result memory.SaveMessageLogWithTopicResult, msg *onebot.GroupMessage) error {
	if result.ArchivedTopicID != 0 {
		groupState := tm.ensureGroupState(groupID)
		delete(groupState.topics, result.ArchivedTopicID)
	}

	topic, err := tm.store.GetTopicThread(ctx, result.TopicID)
	if err != nil {
		state := tm.ensureTopicStateLocked(groupID, result.TopicID)
		zap.L().Warn("刷新话题运行态失败，改用内存回填", zap.Int64("group_id", groupID), zap.Uint("topic_id", result.TopicID), zap.Error(err))
		state.thread.ID = result.TopicID
		state.thread.GroupID = groupID
		state.thread.Status = memory.TopicThreadStatusActive
		tm.recordMessageLocked(groupID, result.TopicID, msg, result.MessageLogID)
		return nil
	}

	tm.syncTopicStateLocked(ctx, groupID, *topic)
	return nil
}

func (tm *TopicManager) enqueueSummaryLocked(groupID int64, topicID uint, state *topicRuntimeState) {
	if state == nil || state.queued {
		return
	}
	state.queued = true
	tm.enqueueSummaryTask(topicSummaryTask{groupID: groupID, topicID: topicID})
}

func (tm *TopicManager) enqueueSummaryTask(task topicSummaryTask) {
	select {
	case tm.summaryQueue <- task:
		return
	default:
		zap.L().Warn("话题摘要队列已满，等待空位重试", zap.Int64("group_id", task.groupID), zap.Uint("topic_id", task.topicID))
		go func() {
			select {
			case <-tm.stopCh:
				return
			case tm.summaryQueue <- task:
			}
		}()
	}
}

func (tm *TopicManager) activeTopicsLocked(groupID int64) []memory.TopicThread {
	groupState := tm.ensureGroupState(groupID)
	topics := make([]memory.TopicThread, 0, len(groupState.topics))
	for _, state := range groupState.topics {
		if state.thread.Status == memory.TopicThreadStatusActive {
			topics = append(topics, state.thread)
		}
	}
	sort.Slice(topics, func(i, j int) bool {
		if topics[i].LastMessageLogID != topics[j].LastMessageLogID {
			return topics[i].LastMessageLogID > topics[j].LastMessageLogID
		}
		return topics[i].ID > topics[j].ID
	})
	return topics
}

func (tm *TopicManager) loadTopicTailMessages(ctx context.Context, topicID uint) []*onebot.GroupMessage {
	logs, err := tm.store.ListRecentTopicMessages(ctx, topicID, memory.TopicTailKeepMessages)
	if err != nil {
		return nil
	}
	msgs := make([]*onebot.GroupMessage, 0, len(logs))
	for _, log := range logs {
		msgs = append(msgs, messageLogToGroupMessage(log))
	}
	return msgs
}

func (tm *TopicManager) loadTopicParticipants(ctx context.Context, topicID uint) []memory.TopicParticipantRef {
	participants, err := tm.store.ListRecentTopicParticipants(ctx, topicID, memory.TopicTailKeepMessages)
	if err != nil {
		return nil
	}
	return participants
}

func (tm *TopicManager) refreshTopicState(ctx context.Context, topicID uint) error {
	topic, err := tm.store.GetTopicThread(ctx, topicID)
	if err != nil {
		return err
	}
	return tm.WithGroupLock(topic.GroupID, func() error {
		_, err := tm.reloadTopicStateLocked(ctx, topic.GroupID, topicID)
		return err
	})
}

func (tm *TopicManager) reloadTopicStateLocked(ctx context.Context, groupID int64, topicID uint) (*memory.TopicThread, error) {
	topic, err := tm.store.GetTopicThread(ctx, topicID)
	if err != nil {
		return nil, err
	}
	tm.syncTopicStateLocked(ctx, groupID, *topic)
	return copyTopicThread(topic), nil
}

func (tm *TopicManager) ensureGroupState(groupID int64) *topicGroupState {
	tm.statesMu.Lock()
	defer tm.statesMu.Unlock()
	groupState, ok := tm.groupStates[groupID]
	if !ok {
		groupState = &topicGroupState{topics: make(map[uint]*topicRuntimeState)}
		tm.groupStates[groupID] = groupState
	}
	return groupState
}

func (tm *TopicManager) lookupTopicStateLocked(groupID int64, topicID uint) *topicRuntimeState {
	tm.statesMu.RLock()
	groupState := tm.groupStates[groupID]
	tm.statesMu.RUnlock()
	if groupState == nil {
		return nil
	}
	return groupState.topics[topicID]
}

func (tm *TopicManager) ensureTopicStateLocked(groupID int64, topicID uint) *topicRuntimeState {
	groupState := tm.ensureGroupState(groupID)
	if state, ok := groupState.topics[topicID]; ok {
		return state
	}

	state := &topicRuntimeState{
		thread: memory.TopicThread{
			ID:      topicID,
			GroupID: groupID,
			Status:  memory.TopicThreadStatusActive,
		},
	}
	groupState.topics[topicID] = state
	return state
}

func (tm *TopicManager) getGroupLock(groupID int64) *sync.Mutex {
	tm.locksMu.Lock()
	defer tm.locksMu.Unlock()
	lock, ok := tm.groupLocks[groupID]
	if !ok {
		lock = &sync.Mutex{}
		tm.groupLocks[groupID] = lock
	}
	return lock
}

func (tm *TopicManager) generateSummary(ctx context.Context, oldSummary memory.TopicSummaryV1, newMessages []memory.MessageLog) (memory.TopicSummaryV1, error) {
	if tm.summaryExtractor == nil {
		return memory.TopicSummaryV1{}, fmt.Errorf("topic summary extractor not configured")
	}

	var msgLines []string
	for _, log := range newMessages {
		text := messageLogTopicText(log)
		if text == "" {
			continue
		}
		msgLines = append(msgLines, text)
	}

	oldSummaryJSON, err := memory.MarshalTopicSummary(oldSummary)
	if err != nil {
		return memory.TopicSummaryV1{}, err
	}
	target := &topicSummarySubmission{}
	summaryCtx, cancel := context.WithTimeout(withTopicSummaryTarget(ctx, target), 30*time.Second)
	defer cancel()

	prompt := fmt.Sprintf("请根据旧摘要和新增消息，调用一次 %s 工具提交最新的话题摘要，不要输出普通文本。\n字段固定为 title,gist,facts,participants,open_loops,recent_turns,keywords。\nfacts 只写已经确认且对后续有用的稳定事实；open_loops 只写尚未解决、后续可能要接上的事项；recent_turns 保留近期推进，不复述全部聊天。\nparticipants 中每项包含 nickname 和 position。\n旧摘要：%s\n新增消息：\n%s", topicSummaryToolName, oldSummaryJSON, strings.Join(msgLines, "\n"))
	_, err = tm.summaryExtractor.Generate(summaryCtx, []*schema.Message{
		schema.SystemMessage("你负责维护群聊当前话题的结构化摘要。你必须调用工具提交结果，不要输出普通文本。"),
		schema.UserMessage(prompt),
	}, buildTopicSummaryOptions()...)
	if err != nil {
		return memory.TopicSummaryV1{}, err
	}

	return normalizeTopicSummarySubmission(target), nil
}

func messageLogToGroupMessage(log memory.MessageLog) *onebot.GroupMessage {
	msg := messageLogBaseGroupMessage(log)
	msg.Content = messageLogTopicText(log)
	msg.FinalContent = strings.TrimSpace(log.Content)
	return msg
}

func cloneGroupMessage(msg *onebot.GroupMessage) *onebot.GroupMessage {
	if msg == nil {
		return nil
	}
	cloned := *msg
	if msg.Reply != nil {
		replyCopy := *msg.Reply
		cloned.Reply = &replyCopy
	}
	if len(msg.Images) > 0 {
		cloned.Images = append([]onebot.ImageInfo(nil), msg.Images...)
	}
	if len(msg.Videos) > 0 {
		cloned.Videos = append([]onebot.VideoInfo(nil), msg.Videos...)
	}
	if len(msg.Faces) > 0 {
		cloned.Faces = append([]onebot.FaceInfo(nil), msg.Faces...)
	}
	if len(msg.AtList) > 0 {
		cloned.AtList = append([]int64(nil), msg.AtList...)
	}
	if len(msg.Forwards) > 0 {
		cloned.Forwards = append([]onebot.ForwardMessage(nil), msg.Forwards...)
	}
	return &cloned
}

func appendTopicTail(tail []*onebot.GroupMessage, msg *onebot.GroupMessage) []*onebot.GroupMessage {
	if msg == nil {
		return tail
	}
	tail = append(tail, msg)
	if len(tail) > memory.TopicTailKeepMessages {
		tail = tail[len(tail)-memory.TopicTailKeepMessages:]
	}
	return tail
}

func renderTopicPromptSection(topic *memory.TopicThread, state *topicRuntimeState) string {
	if topic == nil {
		return ""
	}
	summary := memory.ParseTopicSummary(topic.SummaryJSON)
	title := strings.TrimSpace(summary.Title)
	if title == "" {
		title = fmt.Sprintf("话题 %d", topic.ID)
	}

	var lines []string
	lines = append(lines, "### "+title)
	if gist := strings.TrimSpace(summary.Gist); gist != "" {
		lines = append(lines, "概况："+gist)
	}
	if len(summary.Facts) > 0 {
		lines = append(lines, "已确认："+strings.Join(summary.Facts, "；"))
	}
	if len(summary.Participants) > 0 {
		parts := make([]string, 0, len(summary.Participants))
		for _, participant := range summary.Participants {
			if participant.Nickname == "" || participant.Position == "" {
				continue
			}
			parts = append(parts, participant.Nickname+"："+participant.Position)
		}
		if len(parts) > 0 {
			lines = append(lines, "各方立场："+strings.Join(parts, "；"))
		}
	}
	if len(summary.OpenLoops) > 0 {
		lines = append(lines, "未完事项："+strings.Join(summary.OpenLoops, "；"))
	}
	if len(summary.RecentTurns) > 0 {
		lines = append(lines, "最近摘要："+strings.Join(summary.RecentTurns, "；"))
	}
	if topic.Status == memory.TopicThreadStatusActive && state != nil {
		if tail := renderTopicTailLines(state.tail, topicPromptTailLines); tail != "" {
			lines = append(lines, "关键原文：\n"+tail)
		}
	}
	return strings.Join(lines, "\n")
}

func renderTopicTailLines(tail []*onebot.GroupMessage, limit int) string {
	if limit <= 0 || len(tail) == 0 {
		return ""
	}
	if len(tail) > limit {
		tail = tail[len(tail)-limit:]
	}
	lines := make([]string, 0, len(tail))
	for _, msg := range tail {
		text := messageTopicText(msg)
		if text == "" {
			continue
		}
		lines = append(lines, text)
	}
	return strings.Join(lines, "\n")
}

func messageTopicText(msg *onebot.GroupMessage) string {
	if msg == nil {
		return ""
	}
	return strings.TrimSpace(msg.Content)
}

func messageLogTopicText(log memory.MessageLog) string {
	return strings.TrimSpace(log.OriginalContent)
}

func topicSemanticSimilarity(query string, topic memory.TopicThread, state *topicRuntimeState) float64 {
	summary := memory.ParseTopicSummary(topic.SummaryJSON)
	summaryText := strings.TrimSpace(summary.Title + "\n" + summary.Gist + "\n" + strings.Join(summary.Facts, "\n"))
	tailText := ""
	if state != nil {
		tailText = renderTopicTailLines(state.tail, memory.TopicTailKeepMessages)
	}
	return maxFloat(textSimilarity(query, summaryText), textSimilarity(query, tailText))
}

func topicParticipantOverlap(msg *onebot.GroupMessage, topic memory.TopicThread, state *topicRuntimeState) float64 {
	if msg == nil {
		return 0
	}
	if state != nil {
		for _, participant := range state.participants {
			if participant.UserID != 0 && participant.UserID == msg.UserID {
				return 1
			}
			if participant.UserID == 0 && participant.Nickname != "" && participant.Nickname == msg.Nickname {
				return 1
			}
		}
	}
	return 0
}

func copyTopicThread(topic *memory.TopicThread) *memory.TopicThread {
	if topic == nil {
		return nil
	}
	copied := *topic
	return &copied
}

func topicKeywordContinuity(query string, topic memory.TopicThread) float64 {
	summary := memory.ParseTopicSummary(topic.SummaryJSON)
	if len(summary.Keywords) == 0 || strings.TrimSpace(query) == "" {
		return 0
	}
	matched := 0
	for _, keyword := range summary.Keywords {
		keyword = strings.TrimSpace(keyword)
		if keyword == "" {
			continue
		}
		if strings.Contains(query, keyword) {
			matched++
		}
	}
	if matched == 0 {
		return 0
	}
	return float64(matched) / float64(len(summary.Keywords))
}

func textSimilarity(left, right string) float64 {
	left = normalizeTopicText(left)
	right = normalizeTopicText(right)
	if left == "" || right == "" {
		return 0
	}
	if strings.Contains(right, left) || strings.Contains(left, right) {
		return 0.95
	}

	leftSet := runeBigramSet(left)
	rightSet := runeBigramSet(right)
	if len(leftSet) == 0 || len(rightSet) == 0 {
		return 0
	}

	intersection := 0
	for gram := range leftSet {
		if _, ok := rightSet[gram]; ok {
			intersection++
		}
	}
	if intersection == 0 {
		return 0
	}
	union := len(leftSet) + len(rightSet) - intersection
	return float64(intersection) / float64(union)
}

func normalizeTopicText(text string) string {
	var builder strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(text)) {
		if unicode.IsSpace(r) {
			continue
		}
		if unicode.IsPunct(r) {
			continue
		}
		builder.WriteRune(r)
	}
	return builder.String()
}

func runeBigramSet(text string) map[string]struct{} {
	runes := []rune(text)
	if len(runes) < 2 {
		return map[string]struct{}{text: {}}
	}
	result := make(map[string]struct{}, len(runes)-1)
	for i := 0; i < len(runes)-1; i++ {
		result[string(runes[i:i+2])] = struct{}{}
	}
	return result
}

func uniqueTopicIDs(ids []uint) []uint {
	seen := make(map[uint]struct{}, len(ids))
	result := make([]uint, 0, len(ids))
	for _, id := range ids {
		if id == 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	return result
}

func maxFloat(left, right float64) float64 {
	if left > right {
		return left
	}
	return right
}
