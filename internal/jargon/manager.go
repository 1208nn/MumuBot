package jargon

import (
	"mumu-bot/internal/memory"
	"strings"
	"sync"

	ahocorasick "github.com/petar-dambovaliev/aho-corasick"
	"go.uber.org/zap"
)

// Manager 黑话管理器
type Manager struct {
	memMgr  *memory.Manager
	machine ahocorasick.AhoCorasick
	// 使用 Pattern Index 映射含义
	patterns []string // 存储原始 Pattern，index 对应 Match.Pattern()
	meanings []string // 存储含义，index 对应 Match.Pattern()
	mu       sync.RWMutex
}

func New(memMgr *memory.Manager) *Manager {
	m := &Manager{
		memMgr: memMgr,
	}
	m.Reload()
	return m
}

// Reload 从数据库重新加载所有黑话并构建全局 AC 自动机
func (m *Manager) Reload() {
	jargons, err := m.memMgr.GetAllVerifiedJargons()
	if err != nil {
		zap.L().Error("加载黑话失败", zap.Error(err))
		return
	}

	var patterns []string
	var meanings []string

	// 构建字典和模式列表
	for _, j := range jargons {
		patterns = append(patterns, j.Content)
		meanings = append(meanings, j.Meaning)
	}

	builder := ahocorasick.NewAhoCorasickBuilder(ahocorasick.Opts{
		AsciiCaseInsensitive: true,
		MatchOnlyWholeWords:  false,
		MatchKind:            ahocorasick.LeftMostLongestMatch,
		DFA:                  true,
	})

	machine := builder.Build(patterns)

	m.mu.Lock()
	m.machine = machine
	m.patterns = patterns
	m.meanings = meanings
	m.mu.Unlock()

	zap.L().Info("黑话系统加载完成", zap.Int("total_jargons", len(jargons)))
}

// AddJargon 动态添加黑话并更新 AC 自动机
func (m *Manager) AddJargon(content, meaning string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 检查是否已存在
	for i, p := range m.patterns {
		if strings.EqualFold(p, content) {
			// 更新含义
			m.meanings[i] = meaning
			// 自动机结构不需要变，因为 pattern 没变
			return
		}
	}

	// 新增
	m.patterns = append(m.patterns, content)
	m.meanings = append(m.meanings, meaning)

	// 重建自动机（petar-dambovaliev 库不支持增量添加，需要重建）
	builder := ahocorasick.NewAhoCorasickBuilder(ahocorasick.Opts{
		AsciiCaseInsensitive: true,
		MatchOnlyWholeWords:  false,
		MatchKind:            ahocorasick.LeftMostLongestMatch,
		DFA:                  true,
	})
	m.machine = builder.Build(m.patterns)

	zap.L().Info("已动态更新黑话自动机", zap.String("content", content))
}

// Match 匹配文本中的黑话
// 返回 map[original_content]meaning
func (m *Manager) Match(text string) map[string]string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.patterns) == 0 {
		return nil
	}

	matches := m.machine.FindAll(text)
	if len(matches) == 0 {
		return nil
	}

	result := make(map[string]string)
	for _, match := range matches {
		idx := match.Pattern()
		if idx >= 0 && idx < len(m.patterns) {
			pattern := m.patterns[idx]
			meaning := m.meanings[idx]
			result[pattern] = meaning
		}
	}

	return result
}
