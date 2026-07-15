// adapter-manager/manager.go
package adapter_manager

import (
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/yeswearebot/yes-core/core"
)

// ========================================================
// 1. 消息段 定义与构造器
// ========================================================

type SegmentType string

const (
	SegText    SegmentType = "text"
	SegImage   SegmentType = "image"
	SegAt      SegmentType = "at"
	SegReply   SegmentType = "reply"
	SegFace    SegmentType = "face"
	SegFile    SegmentType = "file"
	SegUnknown SegmentType = "unknown"
)

type MessageSegment struct {
	Type SegmentType    `json:"type"`
	Data map[string]any `json:"data"`
}

func Text(text string) MessageSegment {
	return MessageSegment{Type: SegText, Data: map[string]any{"text": text}}
}

func Image(url string) MessageSegment {
	return MessageSegment{Type: SegImage, Data: map[string]any{"url": url}}
}

func At(userID int64) MessageSegment {
	return MessageSegment{Type: SegAt, Data: map[string]any{"user_id": userID}}
}

func (m MessageSegment) GetString(key string) string {
	if val, ok := m.Data[key]; ok {
		if s, ok := val.(string); ok {
			return s
		}
	}
	return ""
}

func toInt64(val any) int64 {
	switch v := val.(type) {
	case int64:
		return v
	case float64:
		return int64(v)
	case int:
		return int64(v)
	case string:
		if i, err := strconv.ParseInt(v, 10, 64); err == nil {
			return i
		}
	}
	return 0
}

func (m MessageSegment) GetInt64(key string) int64 {
	if val, ok := m.Data[key]; ok {
		switch v := val.(type) {
		case int64:
			return v
		case float64:
			return int64(v)
		case int:
			return int64(v)
		case string:
			if i, err := strconv.ParseInt(v, 10, 64); err == nil {
				return i
			}
		}
	}
	return 0
}

// ========================================================
// 2. 渲染器接口与注册中心 (允许用户自定义)
// ========================================================

// LLMRenderer 定义如何将消息段渲染为 LLM 可读文本
type LLMRenderer interface {
	Render(segments []MessageSegment) string
}

type defaultLLMRenderer struct{}

func (r *defaultLLMRenderer) Render(segments []MessageSegment) string {
	var sb strings.Builder
	for _, seg := range segments {
		switch seg.Type {
		case SegText:
			sb.WriteString(seg.GetString("text"))
		case SegImage:
			sb.WriteString(fmt.Sprintf("[图片:%s]", seg.GetString("url")))
		case SegAt:
			sb.WriteString(fmt.Sprintf("[提及用户:%d]", seg.GetInt64("user_id")))
		case SegReply:
			sb.WriteString(fmt.Sprintf("[回复消息:%d]", seg.GetInt64("message_id")))
		default:
			sb.WriteString(fmt.Sprintf("[%s]", seg.Type))
		}
	}
	return sb.String()
}

var (
	currentLLMRenderer LLMRenderer = &defaultLLMRenderer{}
	rendererMu                     = sync.RWMutex{}
)

// RegisterLLMRenderer 允许业务插件自定义渲染器，覆盖默认行为
func RegisterLLMRenderer(r LLMRenderer) {
	rendererMu.Lock()
	defer rendererMu.Unlock()
	currentLLMRenderer = r
}

func getLLMRenderer() LLMRenderer {
	rendererMu.RLock()
	defer rendererMu.RUnlock()
	return currentLLMRenderer
}

// PlainTextRenderer 纯文本渲染器 (通常用于指令匹配)
type PlainTextRenderer interface {
	Render(segments []MessageSegment) string
}

type defaultPlainTextRenderer struct{}

func (r *defaultPlainTextRenderer) Render(segments []MessageSegment) string {
	var sb strings.Builder
	for _, seg := range segments {
		if seg.Type == SegText {
			sb.WriteString(seg.GetString("text"))
		}
	}
	return sb.String()
}

var (
	currentPlainTextRenderer PlainTextRenderer = &defaultPlainTextRenderer{}
	plainTextMu                                = sync.RWMutex{}
)

func RegisterPlainTextRenderer(r PlainTextRenderer) {
	plainTextMu.Lock()
	defer plainTextMu.Unlock()
	currentPlainTextRenderer = r
}

func getPlainTextRenderer() PlainTextRenderer {
	plainTextMu.RLock()
	defer plainTextMu.RUnlock()
	return currentPlainTextRenderer
}

// ========================================================
// 3. 核心接口与结构体定义
// ========================================================

// Adapter 定义适配器必须实现的接口
type Adapter interface {
	// 解析与发送
	ParseMessage(rawEvent any) []MessageSegment
	SendGroupMsg(groupID int64, message []MessageSegment) error
	SendPrivateMsg(userID int64, message []MessageSegment) error

	// 通用操作
	DeleteMsg(messageID int32) error
	SetGroupBan(groupID, userID, duration int64) error

	// 信息获取 (统一使用 any，由 Manager 内部转为强类型)
	GetGroupInfo(groupID int64, noCache bool) (any, error)
	GetGroupMemberInfo(groupID, userID int64, noCache bool) (any, error)
	GetGroupMemberList(groupID int64) (any, error)
}

// GroupInfo 强类型群信息
type GroupInfo struct {
	GroupID        int64
	GroupName      string
	MemberCount    int64
	MaxMemberCount int64
}

// GroupMemberInfo 强类型群成员信息
type GroupMemberInfo struct {
	GroupID  int64
	UserID   int64
	Nickname string
	Card     string // 群名片
	Role     string // owner, admin, member
}

// MessageEvent 标准化消息事件结构
type MessageEvent struct {
	Platform  string
	EventType string
	Scene     string
	SceneID   int64
	UserID    int64
	MessageID int32
	Nickname  string

	// 🌟 三种维度的消息数据
	Segments  []MessageSegment // 结构化数据
	PlainText string           // 纯文本 (指令匹配用)
	LLMText   string           // LLM 友好文本 (大模型识别用)

	RawEvent any

	// 行为闭包
	Reply     func(msg []MessageSegment) error
	ReplyText func(msg string) error // 快捷纯文本回复
	Delete    func() error
	BanSender func(duration int64) error
}

type AdapterManager struct {
	ctx *core.SystemContext
}

func init() {
	core.Register(func() core.Plugin { return &AdapterManager{} })
}

func (m *AdapterManager) Name() string        { return "adapter-manager" }
func (m *AdapterManager) DependsOn() []string { return nil }

// ========================================================
// 4. 生命周期与事件分发
// ========================================================

func (m *AdapterManager) Init(ctx *core.SystemContext) error {
	m.ctx = ctx
	fmt.Println("[AdapterManager] 初始化完成，正在监听 adapter.raw.message ...")

	ctx.Events.Subscribe("adapter.raw.message", func(payload any) {
		rawData, ok := payload.(map[string]any)
		if !ok {
			return
		}

		eventType, _ := rawData["event_type"].(string)
		if eventType == "group_msg" || eventType == "private_msg" {
			m.handleMessageEvent(rawData)
		} else {
			// 非消息事件，直接转发给业务插件
			ctx.Events.Publish("adapter.event", rawData)
		}
	})

	return nil
}

func (m *AdapterManager) Start(ctx *core.SystemContext) error { return nil }
func (m *AdapterManager) Stop(ctx *core.SystemContext) error  { return nil }

func (m *AdapterManager) handleMessageEvent(rawData map[string]any) {
	platform, _ := rawData["platform"].(string)
	scene, _ := rawData["scene"].(string)
	sceneID, _ := rawData["scene_id"].(int64)
	userID, _ := rawData["user_id"].(int64)
	msgID := int32(toInt64(rawData["message_id"]))

	adapter, err := m.getAdapter(platform)
	if err != nil {
		fmt.Printf("[AdapterManager] 获取适配器失败: %v\n", err)
		return
	}

	// 1. 调用适配器解析出标准消息段
	segments := adapter.ParseMessage(rawData["raw_event"])

	// 2. 调用注册中心获取渲染器，生成多维度文本
	llmText := getLLMRenderer().Render(segments)
	plainText := getPlainTextRenderer().Render(segments)

	event := &MessageEvent{
		Platform:  platform,
		EventType: rawData["event_type"].(string),
		Scene:     scene,
		SceneID:   sceneID,
		UserID:    userID,
		MessageID: msgID,
		Nickname:  rawData["nickname"].(string),
		Segments:  segments,
		LLMText:   llmText,
		PlainText: plainText,
		RawEvent:  rawData["raw_event"],
	}

	// 3. 绑定行为闭包
	event.Reply = func(msg []MessageSegment) error {
		if scene == "group" {
			return adapter.SendGroupMsg(sceneID, msg)
		}
		return adapter.SendPrivateMsg(userID, msg)
	}

	event.ReplyText = func(msg string) error {
		return event.Reply([]MessageSegment{Text(msg)})
	}

	event.Delete = func() error {
		return adapter.DeleteMsg(msgID)
	}

	event.BanSender = func(duration int64) error {
		if scene != "group" {
			return fmt.Errorf("私聊无法禁言")
		}
		return adapter.SetGroupBan(sceneID, userID, duration)
	}

	// 4. 发布标准化事件
	m.ctx.Events.Publish("adapter.message", event)
}

// ========================================================
// 5. 对外暴露的全局方法与辅助函数
// ========================================================

func GetManager(ctx *core.SystemContext) *AdapterManager {
	if raw, ok := ctx.Registry.Get("adapter-manager"); ok {
		if m, ok := raw.(*AdapterManager); ok {
			return m
		}
	}
	return nil
}

// 内部辅助：获取适配器
func (m *AdapterManager) getAdapter(platform string) (Adapter, error) {
	rawAdapter, exists := m.ctx.Registry.Get(platform)
	if !exists {
		return nil, fmt.Errorf("适配器 %s 未找到", platform)
	}
	adapter, ok := rawAdapter.(Adapter)
	if !ok {
		return nil, fmt.Errorf("适配器 %s 未实现 Adapter 接口", platform)
	}
	return adapter, nil
}

// 主动发送消息
func (m *AdapterManager) SendGroupMessage(platform string, groupID int64, msg []MessageSegment) error {
	adapter, err := m.getAdapter(platform)
	if err != nil {
		return err
	}
	return adapter.SendGroupMsg(groupID, msg)
}

func (m *AdapterManager) SendPrivateMessage(platform string, userID int64, msg []MessageSegment) error {
	adapter, err := m.getAdapter(platform)
	if err != nil {
		return err
	}
	return adapter.SendPrivateMsg(userID, msg)
}

func (m *AdapterManager) BanGroupMember(platform string, groupID, userID, duration int64) error {
	adapter, err := m.getAdapter(platform)
	if err != nil {
		return err
	}
	return adapter.SetGroupBan(groupID, userID, duration)
}

// 内部辅助：从 any 中提取 map (兼容各种底层返回格式)
func extractMap(raw any) map[string]any {
	if m, ok := raw.(map[string]any); ok {
		return m
	}
	if wrapper, ok := raw.(map[string]any); ok {
		if data, ok := wrapper["data"].(map[string]any); ok {
			return data
		}
	}
	return nil
}

// 获取群信息
func (m *AdapterManager) GetGroupInfo(platform string, groupID int64, noCache bool) (*GroupInfo, error) {
	adapter, err := m.getAdapter(platform)
	if err != nil {
		return nil, err
	}

	rawRes, err := adapter.GetGroupInfo(groupID, noCache)
	if err != nil {
		return nil, err
	}

	resMap := extractMap(rawRes)
	if resMap == nil {
		return nil, fmt.Errorf("解析群信息失败，未知格式: %T", rawRes)
	}

	info := &GroupInfo{}
	if val, ok := resMap["group_id"].(float64); ok {
		info.GroupID = int64(val)
	}
	if val, ok := resMap["group_name"].(string); ok {
		info.GroupName = val
	}
	if val, ok := resMap["member_count"].(float64); ok {
		info.MemberCount = int64(val)
	}
	if val, ok := resMap["max_member_count"].(float64); ok {
		info.MaxMemberCount = int64(val)
	}

	return info, nil
}

// 获取单个群成员信息
func (m *AdapterManager) GetGroupMemberInfo(platform string, groupID, userID int64, noCache bool) (*GroupMemberInfo, error) {
	adapter, err := m.getAdapter(platform)
	if err != nil {
		return nil, err
	}

	rawRes, err := adapter.GetGroupMemberInfo(groupID, userID, noCache)
	if err != nil {
		return nil, err
	}

	resMap := extractMap(rawRes)
	if resMap == nil {
		return nil, fmt.Errorf("解析群成员信息失败，未知格式: %T", rawRes)
	}

	return parseMemberInfo(resMap), nil
}

// 获取群成员列表
func (m *AdapterManager) GetGroupMemberList(platform string, groupID int64) ([]GroupMemberInfo, error) {
	adapter, err := m.getAdapter(platform)
	if err != nil {
		return nil, err
	}

	rawList, err := adapter.GetGroupMemberList(groupID)
	if err != nil {
		return nil, err
	}

	memberList := make([]GroupMemberInfo, 0)

	list, ok := rawList.([]any)
	if !ok {
		// 兼容 go-cqhttp 返回的 {"data": [...]} 格式
		if wrapper, ok := rawList.(map[string]any); ok {
			if dataArr, ok := wrapper["data"].([]any); ok {
				list = dataArr
			}
		}
	}

	for _, item := range list {
		if memberMap, ok := item.(map[string]any); ok {
			memberList = append(memberList, *parseMemberInfo(memberMap))
		}
	}

	return memberList, nil
}

// 内部辅助：解析 map 为 GroupMemberInfo 结构体
func parseMemberInfo(data map[string]any) *GroupMemberInfo {
	info := &GroupMemberInfo{}
	if val, ok := data["group_id"].(float64); ok {
		info.GroupID = int64(val)
	}
	if val, ok := data["user_id"].(float64); ok {
		info.UserID = int64(val)
	}
	if val, ok := data["nickname"].(string); ok {
		info.Nickname = val
	}
	if val, ok := data["card"].(string); ok {
		info.Card = val
	}
	if val, ok := data["role"].(string); ok {
		info.Role = val
	}
	return info
}
