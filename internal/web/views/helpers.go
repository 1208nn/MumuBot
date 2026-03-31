package views

import (
	"encoding/json"
	"fmt"
	"mumu-bot/internal/memory"
	neturl "net/url"
	"strings"
	"time"
	"unicode"
)

func NavItems() []NavItem {
	return []NavItem{
		{Label: "总览", Href: "/admin"},
		{Label: "风格卡片", Href: "/admin/style-cards"},
		{Label: "黑话", Href: "/admin/jargons"},
		{Label: "表情包", Href: "/admin/stickers"},
		{Label: "记忆", Href: "/admin/memories"},
		{Label: "成员", Href: "/admin/members"},
		{Label: "系统", Href: "/admin/system"},
	}
}

func adminCSSHref() string {
	return "/assets/admin.css"
}

func adminJSHref() string {
	return "/assets/admin.js"
}

func joinClasses(parts ...string) string {
	filtered := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		filtered = append(filtered, part)
	}
	return strings.Join(filtered, " ")
}

func navClass(currentPath string, href string) string {
	base := "group inline-flex w-full items-center gap-3 rounded-2xl px-4 py-3 text-sm font-semibold transition duration-300 ease-out focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-cyan-300 focus-visible:ring-offset-2 focus-visible:ring-offset-white"
	if currentPath == href {
		return joinClasses(base, "bg-[linear-gradient(135deg,#101a32_0%,#1e2c4f_58%,#0b7285_120%)] text-white shadow-[0_20px_36px_rgba(15,23,42,0.18)]")
	}
	return joinClasses(base, "text-slate-600 hover:bg-white/90 hover:text-slate-900")
}

func boolText(v bool) string {
	if v {
		return "已启用"
	}
	return "未启用"
}

func connectionText(v bool) string {
	if v {
		return "已连接"
	}
	return "未连接"
}

func flashClass(kind string) string {
	base := "pointer-events-auto w-full rounded-[1.45rem] border bg-white/96 p-4 text-slate-900 shadow-[0_24px_60px_rgba(8,17,31,0.16)] ring-1 backdrop-blur-xl sm:w-[24rem]"
	switch strings.TrimSpace(kind) {
	case "error":
		return joinClasses(base, "border-rose-200/90 ring-rose-100/80")
	case "warn":
		return joinClasses(base, "border-amber-200/90 ring-amber-100/80")
	default:
		return joinClasses(base, "border-emerald-200/90 ring-emerald-100/80")
	}
}

func flashIconName(kind string) string {
	switch strings.TrimSpace(kind) {
	case "error":
		return "flash-error"
	case "warn":
		return "flash-warn"
	default:
		return "flash-success"
	}
}

func flashIconClass(kind string) string {
	base := "inline-flex size-10 shrink-0 items-center justify-center rounded-2xl ring-1"
	switch strings.TrimSpace(kind) {
	case "error":
		return joinClasses(base, "bg-rose-50 text-rose-700 ring-rose-200/80")
	case "warn":
		return joinClasses(base, "bg-amber-50 text-amber-700 ring-amber-200/80")
	default:
		return joinClasses(base, "bg-emerald-50 text-emerald-700 ring-emerald-200/80")
	}
}

func flashCloseButtonClass(kind string) string {
	base := "inline-flex size-9 shrink-0 items-center justify-center rounded-full text-slate-500 transition duration-200 ease-out hover:bg-slate-950/6 hover:text-slate-700 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-offset-2 focus-visible:ring-offset-white"
	switch strings.TrimSpace(kind) {
	case "error":
		return joinClasses(base, "focus-visible:ring-rose-300")
	case "warn":
		return joinClasses(base, "focus-visible:ring-amber-300")
	default:
		return joinClasses(base, "focus-visible:ring-emerald-300")
	}
}

func adminActionJSON(payload AdminActionPayload) string {
	data, err := json.Marshal(payload)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func flashRole(kind string) string {
	switch strings.TrimSpace(kind) {
	case "error", "warn":
		return "alert"
	default:
		return "status"
	}
}

func flashLive(kind string) string {
	switch strings.TrimSpace(kind) {
	case "error", "warn":
		return "assertive"
	default:
		return "polite"
	}
}

func navIconName(href string) string {
	switch strings.TrimSpace(href) {
	case "/admin":
		return "overview"
	case "/admin/style-cards":
		return "style-cards"
	case "/admin/jargons":
		return "jargons"
	case "/admin/stickers":
		return "stickers"
	case "/admin/memories":
		return "memories"
	case "/admin/members":
		return "members"
	case "/admin/system":
		return "system"
	default:
		return "overview"
	}
}

func systemSectionIconName(title string) string {
	switch strings.TrimSpace(title) {
	case "人格设定":
		return "persona"
	case "群配置", "启用群聊":
		return "group-config"
	case "行为与学习":
		return "behavior"
	case "模型接入", "智能能力", "能力概览":
		return "model"
	case "OneBot 连接", "连接服务", "消息连接", "连接状态":
		return "connection"
	case "存储", "数据存储", "数据与检索", "数据状态":
		return "storage"
	case "后台服务", "后台访问", "后台安全", "登录与扩展":
		return "backend"
	default:
		return "system"
	}
}

func systemFieldCardClass(field SystemField) string {
	base := "rounded-[1.15rem] bg-slate-50/90 p-4 ring-1 ring-slate-200/80"
	if systemFieldNeedsWide(field) {
		return joinClasses(base, "sm:col-span-2")
	}
	return base
}

func systemFieldValueClass(field SystemField) string {
	base := "mt-2 break-words whitespace-pre-line text-sm leading-6 text-slate-700"
	if systemFieldNeedsWide(field) {
		return joinClasses(base, "font-normal")
	}
	return joinClasses(base, "font-medium")
}

func systemFieldNeedsWide(field SystemField) bool {
	switch strings.TrimSpace(field.Label) {
	case "说话风格", "已启用群聊", "消息接入", "数据存储", "检索能力":
		return true
	}
	return len([]rune(strings.TrimSpace(field.Value))) > 32
}

func sortOrderIconName(label string) string {
	switch strings.TrimSpace(label) {
	case "正序":
		return "sort-asc"
	case "倒序":
		return "sort-desc"
	default:
		return "sort"
	}
}

func sortOrderAriaLabel(label string) string {
	label = strings.TrimSpace(label)
	if label == "" {
		return "切换排序顺序"
	}
	return "切换为" + label
}

func styleCardStatusText(status memory.StyleCardStatus) string {
	switch status {
	case memory.StyleCardStatusActive:
		return "已启用"
	case memory.StyleCardStatusRejected:
		return "已拒绝"
	default:
		return "候选"
	}
}

func styleCardStatusClass(status memory.StyleCardStatus) string {
	switch status {
	case memory.StyleCardStatusActive:
		return "inline-flex items-center rounded-full bg-emerald-100 px-3 py-1 text-xs font-semibold text-emerald-700 ring-1 ring-emerald-200"
	case memory.StyleCardStatusRejected:
		return "inline-flex items-center rounded-full bg-rose-100 px-3 py-1 text-xs font-semibold text-rose-700 ring-1 ring-rose-200"
	default:
		return "inline-flex items-center rounded-full bg-amber-100 px-3 py-1 text-xs font-semibold text-amber-700 ring-1 ring-amber-200"
	}
}

func jargonStatusText(item memory.Jargon) string {
	switch {
	case !item.Checked:
		return "待审核"
	case item.Rejected:
		return "已拒绝"
	default:
		return "已通过"
	}
}

func jargonStatusValue(item memory.Jargon) string {
	switch {
	case !item.Checked:
		return "pending"
	case item.Rejected:
		return "rejected"
	default:
		return "approved"
	}
}

func jargonStatusClass(item memory.Jargon) string {
	switch jargonStatusValue(item) {
	case "approved":
		return "inline-flex items-center rounded-full bg-emerald-100 px-3 py-1 text-xs font-semibold text-emerald-700 ring-1 ring-emerald-200"
	case "rejected":
		return "inline-flex items-center rounded-full bg-rose-100 px-3 py-1 text-xs font-semibold text-rose-700 ring-1 ring-rose-200"
	default:
		return "inline-flex items-center rounded-full bg-amber-100 px-3 py-1 text-xs font-semibold text-amber-700 ring-1 ring-amber-200"
	}
}

func formatTime(ts time.Time) string {
	if ts.IsZero() {
		return "-"
	}
	return ts.Format("2006-01-02 15:04")
}

func formatOptionalTime(ts *time.Time) string {
	if ts == nil || ts.IsZero() {
		return "-"
	}
	return ts.Format("2006-01-02 15:04")
}

func emptyDash(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return value
}

func isPlaceholderText(value string) bool {
	normalized := strings.ToLower(strings.TrimSpace(strings.Trim(value, "\"'`")))
	switch normalized {
	case "", "-", "--", "null", "nil", "none", "n/a", "na", "undefined", "unknown", "[]", "{}":
		return true
	default:
		return false
	}
}

func displayText(value string, fallback string) string {
	trimmed := strings.TrimSpace(strings.Trim(value, "\"'`"))
	if isPlaceholderText(trimmed) {
		return fallback
	}
	return trimmed
}

func faviconHref() string {
	return "/favicon.ico"
}

func FaviconSVG() string {
	return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64"><defs><linearGradient id="g" x1="0%" y1="0%" x2="100%" y2="100%"><stop offset="0%" stop-color="#0f766e"/><stop offset="100%" stop-color="#0f172a"/></linearGradient></defs><rect width="64" height="64" rx="18" fill="url(#g)"/><path d="M18 46V18h8l6 10 6-10h8v28h-7V30l-5 8h-4l-5-8v16z" fill="white"/></svg>`
}

func metaSummary(meta ListMeta) string {
	if meta.Total == 0 {
		return "暂无数据"
	}
	start := (meta.Page-1)*meta.PageSize + 1
	end := meta.Page * meta.PageSize
	if int64(end) > meta.Total {
		end = int(meta.Total)
	}
	return fmt.Sprintf("第 %d 页，显示 %d-%d / %d", meta.Page, start, end, meta.Total)
}

func stickerPreviewText(description string) string {
	cleaned := stickerDescriptionText(description)
	preview := firstReadableRunes(cleaned, 2)
	if preview == "" {
		return "贴图"
	}
	return preview
}

func stickerFileURL(fileName string) string {
	fileName = strings.TrimSpace(fileName)
	if fileName == "" {
		return ""
	}
	return "/admin/stickers/files/" + neturl.PathEscape(fileName)
}

func previewText(text string, limit int) string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) <= limit {
		return string(runes)
	}
	return string(runes[:limit]) + "…"
}

func memoryTypeText(kind memory.MemoryType) string {
	switch kind {
	case memory.MemoryTypeGroupFact:
		return "群长期事实"
	case memory.MemoryTypeSelfExperience:
		return "自身经历"
	case memory.MemoryTypeConversation:
		return "重要对话"
	default:
		if strings.TrimSpace(string(kind)) == "" {
			return "未分类"
		}
		return "未分类"
	}
}

func avatarText(value string) string {
	if isPlaceholderText(value) {
		return "友"
	}
	preview := firstReadableRunes(value, 1)
	if preview == "" {
		return "友"
	}
	return preview
}

func memberDisplayName(value string) string {
	return displayText(value, "未填写昵称")
}

func memberFieldText(value string, fallback string) string {
	return displayText(value, fallback)
}

func memberTags(raw string, limit int) []string {
	items := normalizedListItems(raw)
	if limit > 0 && len(items) > limit {
		return items[:limit]
	}
	return items
}

func memberTagOverflow(raw string, limit int) int {
	items := normalizedListItems(raw)
	if limit <= 0 || len(items) <= limit {
		return 0
	}
	return len(items) - limit
}

func rowActionClass(action RowAction) string {
	switch action.Kind {
	case "danger":
		return "inline-flex w-full items-center justify-center gap-2 rounded-2xl bg-rose-50 px-3.5 py-2.5 text-[13px] font-semibold text-rose-700 ring-1 ring-rose-200/80 transition duration-200 ease-out hover:bg-rose-100 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-rose-300 focus-visible:ring-offset-2 focus-visible:ring-offset-white disabled:cursor-wait disabled:opacity-70"
	case "ghost":
		return "inline-flex w-full items-center justify-center gap-2 rounded-2xl border border-slate-200/80 bg-white/82 px-3.5 py-2.5 text-[13px] font-semibold text-slate-700 shadow-[inset_0_1px_0_rgba(255,255,255,0.85)] transition duration-200 ease-out hover:bg-white focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-cyan-300 focus-visible:ring-offset-2 focus-visible:ring-offset-white disabled:cursor-wait disabled:opacity-70"
	default:
		return "inline-flex w-full items-center justify-center gap-2 rounded-2xl bg-teal-50 px-3.5 py-2.5 text-[13px] font-semibold text-teal-700 ring-1 ring-teal-200/80 transition duration-200 ease-out hover:bg-teal-100 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-cyan-300 focus-visible:ring-offset-2 focus-visible:ring-offset-white disabled:cursor-wait disabled:opacity-70"
	}
}

func modalActionClass(action RowAction) string {
	switch action.Kind {
	case "danger":
		return "inline-flex items-center justify-center gap-2 rounded-2xl bg-rose-50 px-4 py-2.5 text-[13px] font-semibold text-rose-700 ring-1 ring-rose-200/80 transition duration-200 ease-out hover:bg-rose-100 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-rose-300 focus-visible:ring-offset-2 focus-visible:ring-offset-white disabled:cursor-wait disabled:opacity-70"
	case "ghost":
		return "inline-flex items-center justify-center gap-2 rounded-2xl border border-slate-200/80 bg-white/82 px-4 py-2.5 text-[13px] font-semibold text-slate-700 shadow-[inset_0_1px_0_rgba(255,255,255,0.85)] transition duration-200 ease-out hover:bg-white focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-cyan-300 focus-visible:ring-offset-2 focus-visible:ring-offset-white disabled:cursor-wait disabled:opacity-70"
	default:
		return "inline-flex items-center justify-center gap-2 rounded-2xl bg-[linear-gradient(135deg,#101a32_0%,#1e2c4f_58%,#0b7285_120%)] px-4 py-2.5 text-[13px] font-semibold text-white shadow-[0_18px_32px_rgba(15,23,42,0.16)] transition duration-200 ease-out hover:brightness-105 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-cyan-300 focus-visible:ring-offset-2 focus-visible:ring-offset-white disabled:cursor-wait disabled:opacity-70"
	}
}

func sortToolbarLinkClass(active bool) string {
	base := "inline-flex items-center justify-center rounded-full px-3 py-1.5 text-sm font-semibold transition duration-200 ease-out focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-cyan-300 focus-visible:ring-offset-2 focus-visible:ring-offset-white"
	if active {
		return joinClasses(base, "bg-[linear-gradient(135deg,#101a32_0%,#1e2c4f_58%,#0b7285_120%)] text-white shadow-[0_12px_24px_rgba(15,23,42,0.14)]")
	}
	return joinClasses(base, "border border-slate-200/80 bg-white/88 text-slate-600 hover:bg-white hover:text-slate-900")
}

func filterChoiceClass(active bool) string {
	base := "inline-flex cursor-pointer items-center justify-center rounded-full px-3 py-1.5 text-sm font-semibold transition duration-200 ease-out focus-within:outline-none focus-within:ring-2 focus-within:ring-cyan-300 focus-within:ring-offset-2 focus-within:ring-offset-white"
	if active {
		return joinClasses(base, "bg-[linear-gradient(135deg,#101a32_0%,#1e2c4f_58%,#0b7285_120%)] text-white shadow-[0_12px_24px_rgba(15,23,42,0.14)]")
	}
	return joinClasses(base, "border border-slate-200/80 bg-white/88 text-slate-600 hover:bg-white hover:text-slate-900")
}

func boolAttrText(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func ternaryString(condition bool, whenTrue string, whenFalse string) string {
	if condition {
		return whenTrue
	}
	return whenFalse
}

func equalTrimmed(left string, right string) bool {
	return strings.TrimSpace(left) == strings.TrimSpace(right)
}

func styleCardActions(status memory.StyleCardStatus) []RowAction {
	switch status {
	case memory.StyleCardStatusActive:
		return []RowAction{
			{Label: "设为拒绝", Value: string(memory.StyleCardStatusRejected), Kind: "danger", BusyLabel: "处理中", ConfirmText: "确认将这张风格卡片设为拒绝状态？"},
		}
	case memory.StyleCardStatusRejected:
		return []RowAction{
			{Label: "重新启用", Value: string(memory.StyleCardStatusActive), Kind: "approve", BusyLabel: "启用中", ConfirmText: "确认重新启用这张风格卡片？"},
		}
	default:
		return []RowAction{
			{Label: "通过", Value: string(memory.StyleCardStatusActive), Kind: "approve", BusyLabel: "通过中", ConfirmText: "确认通过这张候选风格卡片？"},
			{Label: "拒绝", Value: string(memory.StyleCardStatusRejected), Kind: "danger", BusyLabel: "拒绝中", ConfirmText: "确认拒绝这张候选风格卡片？"},
		}
	}
}

func jargonActions(status string) []RowAction {
	switch strings.TrimSpace(status) {
	case "approved":
		return []RowAction{
			{Label: "设为拒绝", Value: "rejected", Kind: "danger", BusyLabel: "处理中", ConfirmText: "确认将这条黑话改为拒绝状态？"},
		}
	case "rejected":
		return []RowAction{
			{Label: "重新通过", Value: "approved", Kind: "approve", BusyLabel: "处理中", ConfirmText: "确认重新通过这条黑话？"},
		}
	default:
		return []RowAction{
			{Label: "通过", Value: "approved", Kind: "approve", BusyLabel: "通过中", ConfirmText: "确认通过这条黑话？"},
			{Label: "拒绝", Value: "rejected", Kind: "danger", BusyLabel: "拒绝中", ConfirmText: "确认拒绝这条黑话？"},
		}
	}
}

func stickerDescriptionText(description string) string {
	cleaned := strings.TrimSpace(description)
	for _, marker := range []string{"<|begin_of_box|>", "<|end_of_box|>", "<|box_start|>", "<|box_end|>"} {
		cleaned = strings.ReplaceAll(cleaned, marker, " ")
	}
	cleaned = strings.Trim(cleaned, "[]【】()（）<>《》「」『』\"'`")
	prefixes := []string{
		"图片:", "图片：", "image:", "Image:",
		"这是一张", "这是一幅", "这是一只", "这是一个", "这是", "一张", "一个", "关于",
	}
	changed := true
	for changed {
		changed = false
		for _, prefix := range prefixes {
			trimmed := strings.TrimSpace(strings.TrimPrefix(cleaned, prefix))
			if trimmed != cleaned {
				cleaned = trimmed
				changed = true
			}
		}
	}
	cleaned = strings.Join(strings.Fields(cleaned), " ")
	if cleaned == "" {
		return "暂无描述"
	}
	return cleaned
}

func firstReadableRunes(text string, limit int) string {
	if limit <= 0 {
		return ""
	}
	var builder []rune
	for _, r := range text {
		if unicode.IsSpace(r) || strings.ContainsRune("[]【】()（）<>《》「」『』:：;；,.，。!！?？'\"`~·-_/\\|", r) {
			continue
		}
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			continue
		}
		builder = append(builder, unicode.ToUpper(r))
		if len(builder) == limit {
			break
		}
	}
	return string(builder)
}

func normalizedListItems(raw string) []string {
	text := strings.TrimSpace(raw)
	if text == "" {
		return nil
	}

	var items []string
	if strings.HasPrefix(text, "[") {
		var parsed []string
		if err := json.Unmarshal([]byte(text), &parsed); err == nil {
			items = parsed
		}
	}
	if len(items) == 0 {
		items = strings.FieldsFunc(text, func(r rune) bool {
			switch r {
			case '\n', '\r', '\t', ',', '，', '、':
				return true
			default:
				return false
			}
		})
	}

	seen := make(map[string]struct{}, len(items))
	normalized := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(strings.Trim(item, "\""))
		if isPlaceholderText(item) {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		normalized = append(normalized, item)
	}
	if len(normalized) == 0 {
		return nil
	}
	return normalized
}
