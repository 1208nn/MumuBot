package views

import "mumu-bot/internal/memory"

type NavItem struct {
	Label string
	Href  string
}

type FlashMessage struct {
	Kind  string
	Title string
	Body  string
}

type AdminActionChip struct {
	Label string `json:"label"`
	Kind  string `json:"kind"`
}

type AdminActionField struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

type AdminActionHiddenField struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type AdminActionPayload struct {
	Title       string                   `json:"title"`
	Body        string                   `json:"body,omitempty"`
	Endpoint    string                   `json:"endpoint"`
	SubmitLabel string                   `json:"submitLabel"`
	SubmitClass string                   `json:"submitClass"`
	BusyLabel   string                   `json:"busyLabel,omitempty"`
	Spotlight   string                   `json:"spotlight,omitempty"`
	Chips       []AdminActionChip        `json:"chips,omitempty"`
	Fields      []AdminActionField       `json:"fields,omitempty"`
	Hidden      []AdminActionHiddenField `json:"hidden,omitempty"`
}

type RowAction struct {
	Label       string
	Value       string
	Kind        string
	BusyLabel   string
	ConfirmText string
}

type LayoutData struct {
	Title       string
	CurrentPath string
	NavItems    []NavItem
	Flash       *FlashMessage
}

type SortToolbarLink struct {
	Label  string
	Href   string
	Active bool
}

type SortToolbarData struct {
	Summary      string
	CurrentSort  string
	CurrentOrder string
	Options      []SortToolbarLink
	OrderOptions []SortToolbarLink
}

type FilterChoice struct {
	Label string
	Value string
}

type LoginPageData struct {
	Enabled bool
	Error   string
}

type DashboardPageData struct {
	BotName           string
	EnabledGroupCount int
	MemoryCount       int64
	MemberCount       int64
	JargonCount       int64
	StyleCardCount    int64
	StickerCount      int64
	OneBotConnected   bool
	SelfID            int64
	MCPToolCount      int
	LearningEnabled   bool
	CurrentMood       *memory.MoodState
	Flash             *FlashMessage
}

type ListMeta struct {
	Total    int64
	Page     int
	PageSize int
	PrevURL  string
	NextURL  string
}

type StyleCardListPageData struct {
	GroupID string
	Status  string
	Keyword string
	Sort    SortToolbarData
	Items   []memory.StyleCard
	Meta    ListMeta
	Flash   *FlashMessage
}

type JargonListPageData struct {
	GroupID string
	Status  string
	Keyword string
	Sort    SortToolbarData
	Items   []memory.Jargon
	Meta    ListMeta
	Flash   *FlashMessage
}

type StickerListPageData struct {
	Keyword string
	Sort    SortToolbarData
	Items   []memory.Sticker
	Meta    ListMeta
	Flash   *FlashMessage
}

type MemoryListPageData struct {
	GroupID string
	Type    string
	Keyword string
	Sort    SortToolbarData
	Items   []memory.Memory
	Meta    ListMeta
	Flash   *FlashMessage
}

type MemberListPageData struct {
	Keyword string
	Sort    SortToolbarData
	Items   []memory.MemberProfile
	Meta    ListMeta
	Flash   *FlashMessage
}

type SystemField struct {
	Label string
	Value string
}

type SystemSection struct {
	Title  string
	Fields []SystemField
}

type SystemPageData struct {
	Sections []SystemSection
	Flash    *FlashMessage
}
