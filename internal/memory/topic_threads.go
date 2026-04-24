package memory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mumu-bot/internal/config"
	"sort"
	"strings"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"mumu-bot/internal/vector"
)

const topicSummaryVectorType = "topic_summary"

var ErrTopicStateChanged = errors.New("topic state changed")

type TopicSlotAction string

const (
	TopicSlotActionReuse  TopicSlotAction = "reuse"
	TopicSlotActionCreate TopicSlotAction = "create"
	TopicSlotActionReopen TopicSlotAction = "reopen"
)

type SaveMessageLogWithTopicInput struct {
	GroupID         int64
	Message         MessageLog
	TopicID         uint
	ExpectedTopicID uint
	MatchReason     string
	MatchScore      float64
	SlotAction      TopicSlotAction
	VictimTopicID   uint
}

type SaveMessageLogWithTopicResult struct {
	TopicID         uint
	MessageLogID    uint
	ArchivedTopicID uint
}

type TopicThreadSearchHit struct {
	Topic TopicThread
	Score float64
}

func EmptyTopicSummary() TopicSummaryV1 {
	return TopicSummaryV1{
		Version:      1,
		Facts:        []string{},
		Participants: []TopicSummaryParticipant{},
		OpenLoops:    []string{},
		RecentTurns:  []string{},
		Keywords:     []string{},
	}
}

func MarshalTopicSummary(summary TopicSummaryV1) (string, error) {
	if summary.Version == 0 {
		summary.Version = 1
	}
	if summary.Facts == nil {
		summary.Facts = []string{}
	}
	if summary.Participants == nil {
		summary.Participants = []TopicSummaryParticipant{}
	}
	if summary.OpenLoops == nil {
		summary.OpenLoops = []string{}
	}
	if summary.RecentTurns == nil {
		summary.RecentTurns = []string{}
	}
	if summary.Keywords == nil {
		summary.Keywords = []string{}
	}

	raw, err := json.Marshal(summary)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func MustMarshalTopicSummary(summary TopicSummaryV1) string {
	raw, err := MarshalTopicSummary(summary)
	if err != nil {
		return `{"version":1,"title":"","gist":"","facts":[],"participants":[],"open_loops":[],"recent_turns":[],"keywords":[]}`
	}
	return raw
}

func ParseTopicSummary(raw string) TopicSummaryV1 {
	summary := EmptyTopicSummary()
	if strings.TrimSpace(raw) == "" {
		return summary
	}
	if err := json.Unmarshal([]byte(raw), &summary); err != nil {
		return EmptyTopicSummary()
	}
	if summary.Version == 0 {
		summary.Version = 1
	}
	if summary.Facts == nil {
		summary.Facts = []string{}
	}
	if summary.Participants == nil {
		summary.Participants = []TopicSummaryParticipant{}
	}
	if summary.OpenLoops == nil {
		summary.OpenLoops = []string{}
	}
	if summary.RecentTurns == nil {
		summary.RecentTurns = []string{}
	}
	if summary.Keywords == nil {
		summary.Keywords = []string{}
	}
	return summary
}

func ParseTopicSummaryHistory(raw string) []TopicSummarySnapshot {
	if strings.TrimSpace(raw) == "" {
		return []TopicSummarySnapshot{}
	}
	var snapshots []TopicSummarySnapshot
	if err := json.Unmarshal([]byte(raw), &snapshots); err != nil {
		return []TopicSummarySnapshot{}
	}
	return snapshots
}

func DefaultTopicSummaryJSON() string {
	return MustMarshalTopicSummary(EmptyTopicSummary())
}

func DefaultTopicSummaryHistoryJSON() string {
	return "[]"
}

func AppendTopicSummaryHistory(raw string, summary TopicSummaryV1, capturedAt time.Time) (string, error) {
	history := ParseTopicSummaryHistory(raw)
	history = append(history, TopicSummarySnapshot{
		CapturedAt: capturedAt.Format(time.RFC3339),
		Summary:    ParseTopicSummary(MustMarshalTopicSummary(summary)),
	})
	if len(history) > TopicSummaryHistoryLimit {
		history = history[len(history)-TopicSummaryHistoryLimit:]
	}
	encoded, err := json.Marshal(history)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func (m *Manager) ArchiveTopicThreadForRepair(ctx context.Context, groupID int64, topicID uint) error {
	if topicID == 0 {
		return nil
	}
	return m.db.WithContext(ctx).
		Model(&TopicThread{}).
		Where("id = ? AND group_id = ? AND status = ?", topicID, groupID, TopicThreadStatusActive).
		Updates(map[string]any{
			"status":     TopicThreadStatusArchived,
			"updated_at": time.Now(),
		}).Error
}

func (m *Manager) UpdateTopicSummary(ctx context.Context, topicID uint, summary TopicSummaryV1, summaryUntil uint, capturedAt time.Time) error {
	if topicID == 0 {
		return nil
	}

	summaryJSON, err := MarshalTopicSummary(summary)
	if err != nil {
		return err
	}
	updated := false
	err = m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var topic TopicThread
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&topic, topicID).Error; err != nil {
			return err
		}
		if summaryUntil <= topic.SummaryUntilMessageLogID {
			return nil
		}

		historyJSON, err := AppendTopicSummaryHistory(topic.SummaryHistoryJSON, summary, capturedAt)
		if err != nil {
			return err
		}

		if err := tx.Model(&TopicThread{}).
			Where("id = ?", topicID).
			Updates(map[string]any{
				"summary_json":                 summaryJSON,
				"summary_history_json":         historyJSON,
				"summary_until_message_log_id": summaryUntil,
				"updated_at":                   time.Now(),
			}).Error; err != nil {
			return err
		}
		updated = true
		return nil
	})
	if err != nil || !updated {
		return err
	}

	topic, fetchErr := m.GetTopicThread(ctx, topicID)
	if fetchErr == nil && topic != nil {
		summary := ParseTopicSummary(topic.SummaryJSON)
		participants, participantsErr := m.ListRecentTopicParticipants(ctx, topic.ID, TopicTailKeepMessages)
		if participantsErr != nil {
			zap.L().Warn("读取话题参与者失败，长期记忆主体解析可能退化", zap.Uint("topic_id", topic.ID), zap.Error(participantsErr))
		}
		if len(summary.Facts) > 0 {
			_, upsertErr := m.UpsertTopicMemoryCandidate(ctx, TopicMemoryCandidateInput{
				GroupID:      topic.GroupID,
				TopicID:      topic.ID,
				SelfID:       config.Get().Persona.QQ,
				Claims:       summary.Facts,
				LegacyType:   MemoryTypeGroupFact,
				Participants: participants,
			})
			if upsertErr != nil {
				zap.L().Warn("话题事实下沉长期记忆失败", zap.Uint("topic_id", topic.ID), zap.Error(upsertErr))
			}
		}
		if len(summary.OpenLoops) > 0 {
			_, upsertErr := m.UpsertTopicMemoryCandidate(ctx, TopicMemoryCandidateInput{
				GroupID:               topic.GroupID,
				TopicID:               topic.ID,
				SelfID:                config.Get().Persona.QQ,
				Claims:                summary.OpenLoops,
				LegacyType:            MemoryTypeGroupFact,
				Participants:          participants,
				AllowedCanonicalTypes: []CanonicalMemoryType{CanonicalMemoryTypeGoal},
			})
			if upsertErr != nil {
				zap.L().Warn("话题待办下沉长期目标失败", zap.Uint("topic_id", topic.ID), zap.Error(upsertErr))
			}
		}
	}

	if err := m.SyncTopicThreadVector(ctx, topicID); err != nil {
		zap.L().Warn("更新话题摘要后同步向量失败", zap.Uint("topic_id", topicID), zap.Error(err))
	}
	return nil
}

func (m *Manager) ListArchivedTopicThreadsNeedingSummary(ctx context.Context, groupID int64) ([]TopicThread, error) {
	var topics []TopicThread
	err := m.db.WithContext(ctx).
		Where("group_id = ? AND status = ? AND summary_until_message_log_id < last_message_log_id", groupID, TopicThreadStatusArchived).
		Order("last_message_log_id ASC").
		Order("id ASC").
		Find(&topics).Error
	return topics, err
}

func (m *Manager) ListActiveTopicThreads(ctx context.Context, groupID int64) ([]TopicThread, error) {
	var topics []TopicThread
	err := m.db.WithContext(ctx).
		Where("group_id = ? AND status = ?", groupID, TopicThreadStatusActive).
		Order("last_message_log_id DESC").
		Order("id DESC").
		Find(&topics).Error
	return topics, err
}

func (m *Manager) GetTopicThread(ctx context.Context, topicID uint) (*TopicThread, error) {
	var topic TopicThread
	if err := m.db.WithContext(ctx).First(&topic, topicID).Error; err != nil {
		return nil, err
	}
	return &topic, nil
}

func (m *Manager) SearchArchivedTopicThreads(ctx context.Context, query string, groupID int64, topK int, threshold float64) ([]TopicThread, error) {
	hits, err := m.SearchArchivedTopicThreadHits(ctx, query, groupID, topK, threshold)
	if err != nil {
		return nil, err
	}
	topics := make([]TopicThread, 0, len(hits))
	for _, hit := range hits {
		topics = append(topics, hit.Topic)
	}
	return topics, nil
}

func (m *Manager) SearchArchivedTopicThreadHits(ctx context.Context, query string, groupID int64, topK int, threshold float64) ([]TopicThreadSearchHit, error) {
	if m.embedding == nil || m.topicMilvus == nil {
		return nil, nil
	}
	if topK <= 0 {
		topK = 8
	}

	results, err := m.searchArchivedTopicSummaries(ctx, query, groupID, topK, threshold)
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, nil
	}

	ids := make([]uint, 0, len(results))
	for _, result := range results {
		ids = append(ids, result.MemoryID)
	}

	var topics []TopicThread
	if err := m.db.WithContext(ctx).
		Where("id IN ? AND group_id = ? AND status = ?", ids, groupID, TopicThreadStatusArchived).
		Find(&topics).Error; err != nil {
		return nil, err
	}

	topicByID := make(map[uint]TopicThread, len(topics))
	for _, topic := range topics {
		topicByID[topic.ID] = topic
	}

	hits := make([]TopicThreadSearchHit, 0, len(results))
	for _, result := range results {
		if topic, ok := topicByID[result.MemoryID]; ok {
			hits = append(hits, TopicThreadSearchHit{
				Topic: topic,
				Score: float64(result.Score),
			})
		}
	}
	return hits, nil
}

func (m *Manager) GetTopicMessagesAfterSummary(ctx context.Context, topicID uint, limit int) ([]MessageLog, error) {
	topic, err := m.GetTopicThread(ctx, topicID)
	if err != nil {
		return nil, err
	}

	query := m.db.WithContext(ctx).
		Where("group_id = ? AND topic_thread_id = ? AND id > ?", topic.GroupID, topic.ID, topic.SummaryUntilMessageLogID).
		Order("id ASC")
	if limit > 0 {
		query = query.Limit(limit)
	}

	var logs []MessageLog
	if err := query.Find(&logs).Error; err != nil {
		return nil, err
	}
	return logs, nil
}

func (m *Manager) CountTopicMessagesAfterSummary(ctx context.Context, topicID uint) (int, error) {
	topic, err := m.GetTopicThread(ctx, topicID)
	if err != nil {
		return 0, err
	}

	var count int64
	if err := m.db.WithContext(ctx).
		Model(&MessageLog{}).
		Where("group_id = ? AND topic_thread_id = ? AND id > ?", topic.GroupID, topic.ID, topic.SummaryUntilMessageLogID).
		Count(&count).Error; err != nil {
		return 0, err
	}
	return int(count), nil
}

func (m *Manager) ListRecentTopicMessages(ctx context.Context, topicID uint, limit int) ([]MessageLog, error) {
	if limit <= 0 {
		limit = TopicTailKeepMessages
	}

	var logs []MessageLog
	if err := m.db.WithContext(ctx).
		Where("topic_thread_id = ?", topicID).
		Order("id DESC").
		Limit(limit).
		Find(&logs).Error; err != nil {
		return nil, err
	}

	for i, j := 0, len(logs)-1; i < j; i, j = i+1, j-1 {
		logs[i], logs[j] = logs[j], logs[i]
	}
	return logs, nil
}

func (m *Manager) ListRecentTopicParticipants(ctx context.Context, topicID uint, limit int) ([]TopicParticipantRef, error) {
	if limit <= 0 {
		limit = TopicTailKeepMessages
	}

	var logs []MessageLog
	if err := m.db.WithContext(ctx).
		Where("topic_thread_id = ?", topicID).
		Order("id DESC").
		Find(&logs).Error; err != nil {
		return nil, err
	}

	participants := make([]TopicParticipantRef, 0, limit)
	seen := make(map[int64]struct{}, limit)
	for _, log := range logs {
		if _, ok := seen[log.UserID]; ok {
			continue
		}
		seen[log.UserID] = struct{}{}
		participants = append(participants, TopicParticipantRef{
			UserID:   log.UserID,
			Nickname: log.Nickname,
		})
		if len(participants) >= limit {
			break
		}
	}
	return participants, nil
}

func (m *Manager) SaveMessageLogWithTopic(ctx context.Context, input SaveMessageLogWithTopicInput) (SaveMessageLogWithTopicResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	var result SaveMessageLogWithTopicResult
	err := m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		activeTopics, err := lockActiveTopics(tx, input.GroupID)
		if err != nil {
			return err
		}

		targetTopicID, archivedTopicID, err := applyTopicDecisionTx(tx, input, activeTopics)
		if err != nil {
			return err
		}
		if input.ExpectedTopicID != 0 && input.ExpectedTopicID != targetTopicID {
			return ErrTopicStateChanged
		}

		message := input.Message
		message.GroupID = input.GroupID
		message.TopicThreadID = targetTopicID
		message.TopicMatchReason = strings.TrimSpace(input.MatchReason)
		message.TopicMatchScore = input.MatchScore
		if err := tx.Create(&message).Error; err != nil {
			return err
		}

		if err := tx.Model(&TopicThread{}).
			Where("id = ?", targetTopicID).
			Updates(map[string]any{
				"last_message_log_id": message.ID,
				"updated_at":          time.Now(),
			}).Error; err != nil {
			return err
		}

		result = SaveMessageLogWithTopicResult{
			TopicID:         targetTopicID,
			MessageLogID:    message.ID,
			ArchivedTopicID: archivedTopicID,
		}
		return nil
	})
	if err != nil {
		return SaveMessageLogWithTopicResult{}, err
	}

	return result, nil
}

func (m *Manager) SyncTopicThreadVector(ctx context.Context, topicID uint) error {
	if topicID == 0 || m.topicMilvus == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	var topic TopicThread
	if err := m.db.WithContext(ctx).First(&topic, topicID).Error; err != nil {
		return err
	}

	if err := m.topicMilvus.Delete(ctx, []uint{topicID}); err != nil {
		return fmt.Errorf("删除旧话题向量失败: %w", err)
	}

	if topic.Status != TopicThreadStatusArchived || topic.SummaryUntilMessageLogID < topic.LastMessageLogID || m.embedding == nil {
		return nil
	}

	embedding, err := m.embedding.Embed(ctx, topicSummaryVectorText(ParseTopicSummary(topic.SummaryJSON)))
	if err != nil {
		return err
	}
	if _, err := m.topicMilvus.Insert(ctx, topic.ID, topic.GroupID, topicSummaryVectorType, embedding); err != nil {
		return fmt.Errorf("插入话题向量失败: %w", err)
	}
	return nil
}

func (m *Manager) protectedTopicMessageIDs(groupID int64, keepLatest int) ([]uint, error) {
	keepSet := make(map[uint]struct{})

	if keepLatest > 0 {
		var latestIDs []uint
		if err := m.db.Model(&MessageLog{}).
			Where("group_id = ?", groupID).
			Order("created_at DESC").
			Limit(keepLatest).
			Pluck("id", &latestIDs).Error; err != nil {
			return nil, err
		}
		for _, id := range latestIDs {
			keepSet[id] = struct{}{}
		}
	}

	var trackedTopics []TopicThread
	if err := m.db.Model(&TopicThread{}).
		Where("group_id = ? AND (status = ? OR summary_until_message_log_id < last_message_log_id)", groupID, TopicThreadStatusActive).
		Find(&trackedTopics).Error; err != nil {
		return nil, err
	}

	for _, topic := range trackedTopics {
		var pendingIDs []uint
		if err := m.db.Model(&MessageLog{}).
			Where("group_id = ? AND topic_thread_id = ? AND id > ?", groupID, topic.ID, topic.SummaryUntilMessageLogID).
			Pluck("id", &pendingIDs).Error; err != nil {
			return nil, err
		}
		for _, id := range pendingIDs {
			keepSet[id] = struct{}{}
		}

		if topic.Status != TopicThreadStatusActive {
			continue
		}

		var tailIDs []uint
		if err := m.db.Model(&MessageLog{}).
			Where("group_id = ? AND topic_thread_id = ?", groupID, topic.ID).
			Order("id DESC").
			Limit(TopicTailKeepMessages).
			Pluck("id", &tailIDs).Error; err != nil {
			return nil, err
		}
		for _, id := range tailIDs {
			keepSet[id] = struct{}{}
		}
	}

	ids := make([]uint, 0, len(keepSet))
	for id := range keepSet {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids, nil
}

func topicSummaryCollectionName(base string) string {
	base = strings.TrimSpace(base)
	if base == "" {
		base = "mumu_memories"
	}
	return base + "_topic_summaries"
}

func topicSummaryVectorText(summary TopicSummaryV1) string {
	parts := []string{
		strings.TrimSpace(summary.Title),
		strings.TrimSpace(summary.Gist),
		strings.Join(summary.Facts, "\n"),
	}
	if len(summary.OpenLoops) > 0 {
		parts = append(parts, strings.Join(summary.OpenLoops, "\n"))
	}
	if len(summary.RecentTurns) > 0 {
		parts = append(parts, strings.Join(summary.RecentTurns, "\n"))
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

func lockActiveTopics(tx *gorm.DB, groupID int64) ([]TopicThread, error) {
	var topics []TopicThread
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("group_id = ? AND status = ?", groupID, TopicThreadStatusActive).
		Order("last_message_log_id ASC").
		Order("id ASC").
		Find(&topics).Error
	return topics, err
}

func applyTopicDecisionTx(tx *gorm.DB, input SaveMessageLogWithTopicInput, activeTopics []TopicThread) (uint, uint, error) {
	oldestTopicID := OldestActiveTopicID(activeTopics)

	switch input.SlotAction {
	case TopicSlotActionReuse:
		if input.VictimTopicID != 0 || input.TopicID == 0 || !containsActiveTopic(activeTopics, input.TopicID) {
			return 0, 0, ErrTopicStateChanged
		}
		return input.TopicID, 0, nil
	case TopicSlotActionCreate:
		if input.TopicID != 0 {
			return 0, 0, ErrTopicStateChanged
		}
		if input.VictimTopicID != 0 {
			if len(activeTopics) < MaxActiveTopicThreadsPerGroup || oldestTopicID != input.VictimTopicID {
				return 0, 0, ErrTopicStateChanged
			}
			if err := archiveTopicThreadTx(tx, input.GroupID, input.VictimTopicID); err != nil {
				return 0, 0, err
			}
		} else if len(activeTopics) >= MaxActiveTopicThreadsPerGroup {
			return 0, 0, ErrTopicStateChanged
		}
		topic, err := createTopicThreadTx(tx, input.GroupID)
		if err != nil {
			return 0, 0, err
		}
		return topic.ID, input.VictimTopicID, nil
	case TopicSlotActionReopen:
		if input.TopicID == 0 || input.TopicID == input.VictimTopicID {
			return 0, 0, ErrTopicStateChanged
		}
		if input.VictimTopicID != 0 {
			if len(activeTopics) < MaxActiveTopicThreadsPerGroup || oldestTopicID != input.VictimTopicID {
				return 0, 0, ErrTopicStateChanged
			}
			if err := archiveTopicThreadTx(tx, input.GroupID, input.VictimTopicID); err != nil {
				return 0, 0, err
			}
		} else if len(activeTopics) >= MaxActiveTopicThreadsPerGroup {
			return 0, 0, ErrTopicStateChanged
		}
		if err := reopenTopicThreadTx(tx, input.GroupID, input.TopicID); err != nil {
			return 0, 0, err
		}
		return input.TopicID, input.VictimTopicID, nil
	default:
		return 0, 0, fmt.Errorf("不支持的话题槽位动作: %s", input.SlotAction)
	}
}

func createTopicThreadTx(tx *gorm.DB, groupID int64) (*TopicThread, error) {
	topic := &TopicThread{
		GroupID:                  groupID,
		Status:                   TopicThreadStatusActive,
		SummaryJSON:              DefaultTopicSummaryJSON(),
		SummaryHistoryJSON:       DefaultTopicSummaryHistoryJSON(),
		SummaryUntilMessageLogID: 0,
		LastMessageLogID:         0,
	}
	if err := tx.Create(topic).Error; err != nil {
		return nil, err
	}
	return topic, nil
}

func reopenTopicThreadTx(tx *gorm.DB, groupID int64, topicID uint) error {
	result := tx.Model(&TopicThread{}).
		Where("id = ? AND group_id = ? AND status = ?", topicID, groupID, TopicThreadStatusArchived).
		Updates(map[string]any{
			"status":     TopicThreadStatusActive,
			"updated_at": time.Now(),
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrTopicStateChanged
	}
	return nil
}

func archiveTopicThreadTx(tx *gorm.DB, groupID int64, topicID uint) error {
	result := tx.Model(&TopicThread{}).
		Where("id = ? AND group_id = ? AND status = ?", topicID, groupID, TopicThreadStatusActive).
		Updates(map[string]any{
			"status":     TopicThreadStatusArchived,
			"updated_at": time.Now(),
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrTopicStateChanged
	}
	return nil
}

func containsActiveTopic(topics []TopicThread, topicID uint) bool {
	for _, topic := range topics {
		if topic.ID == topicID {
			return true
		}
	}
	return false
}

func OldestActiveTopicID(topics []TopicThread) uint {
	if len(topics) == 0 {
		return 0
	}
	oldest := topics[0]
	for _, topic := range topics[1:] {
		if topic.LastMessageLogID < oldest.LastMessageLogID {
			oldest = topic
			continue
		}
		if topic.LastMessageLogID == oldest.LastMessageLogID && topic.ID < oldest.ID {
			oldest = topic
		}
	}
	return oldest.ID
}

func (m *Manager) searchArchivedTopicSummaries(ctx context.Context, query string, groupID int64, topK int, threshold float64) ([]vector.SearchResult, error) {
	if m.embedding == nil || m.topicMilvus == nil {
		return nil, nil
	}
	embedding, err := m.embedding.Embed(ctx, query)
	if err != nil {
		return nil, err
	}
	return m.topicMilvus.Search(ctx, embedding, groupID, topicSummaryVectorType, topK, threshold)
}
