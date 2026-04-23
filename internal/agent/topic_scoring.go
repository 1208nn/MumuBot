package agent

import (
	"sort"

	"mumu-bot/internal/memory"
)

const (
	topicReuseThreshold = 0.75
)

type topicCandidate struct {
	TopicID            uint
	Status             memory.TopicThreadStatus
	ReplyMatched       bool
	SemanticScore      float64
	ParticipantOverlap float64
	KeywordContinuity  float64
	LastMessageLogID   uint
}

type topicDecision struct {
	TopicID       uint
	SlotAction    memory.TopicSlotAction
	VictimTopicID uint
}

func chooseTopicDecision(candidates []topicCandidate, activeTopics []memory.TopicThread, reuseThreshold float64, maxActive int) topicDecision {
	if maxActive <= 0 {
		maxActive = memory.MaxActiveTopicThreadsPerGroup
	}

	sortedCandidates := sortTopicCandidates(candidates)
	var (
		best topicCandidate
		ok   bool
	)
	for _, candidate := range sortedCandidates {
		score := scoreTopicCandidate(candidate)
		if candidate.ReplyMatched || score >= reuseThreshold {
			best = candidate
			ok = true
			break
		}
	}
	if !ok {
		if len(activeTopics) >= maxActive {
			return topicDecision{
				SlotAction:    memory.TopicSlotActionCreate,
				VictimTopicID: memory.OldestActiveTopicID(activeTopics),
			}
		}
		return topicDecision{SlotAction: memory.TopicSlotActionCreate}
	}

	if best.Status == memory.TopicThreadStatusActive {
		return topicDecision{
			TopicID:    best.TopicID,
			SlotAction: memory.TopicSlotActionReuse,
		}
	}

	if len(activeTopics) >= maxActive {
		return topicDecision{
			TopicID:       best.TopicID,
			SlotAction:    memory.TopicSlotActionReopen,
			VictimTopicID: memory.OldestActiveTopicID(activeTopics),
		}
	}

	return topicDecision{
		TopicID:    best.TopicID,
		SlotAction: memory.TopicSlotActionReopen,
	}
}

func sortTopicCandidates(candidates []topicCandidate) []topicCandidate {
	sortedCandidates := make([]topicCandidate, len(candidates))
	copy(sortedCandidates, candidates)
	sort.SliceStable(sortedCandidates, func(i, j int) bool {
		left := sortedCandidates[i]
		right := sortedCandidates[j]
		if left.ReplyMatched != right.ReplyMatched {
			return left.ReplyMatched
		}
		leftScore := scoreTopicCandidate(left)
		rightScore := scoreTopicCandidate(right)
		if leftScore != rightScore {
			return leftScore > rightScore
		}
		if left.LastMessageLogID != right.LastMessageLogID {
			return left.LastMessageLogID > right.LastMessageLogID
		}
		return left.TopicID < right.TopicID
	})
	return sortedCandidates
}

func scoreTopicCandidate(candidate topicCandidate) float64 {
	score := candidate.SemanticScore
	score += candidate.ParticipantOverlap * 0.08
	score += candidate.KeywordContinuity * 0.07
	if candidate.ReplyMatched {
		score += 1.0
	}
	return score
}
