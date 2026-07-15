// adapter-manager/manager.go
package adapter_manager

import (
	"fmt"

	"github.com/yeswearebot/yes-core/core"
)

// Adapter 定义适配器必须实现的接口
type Adapter interface {
	SendGroupMsg(groupID int64, message string) error
	SendPrivateMsg(userID int64, message string) error
	DeleteMsg(messageID int32) error
	SetGroupBan(groupID, userID, duration int64) error
	GetGroupInfo(groupID int64, noCache bool) (map[string]any, error)
	GetGroupMemberInfo(groupID, userID int64, noCache bool) (map[string]any, error)
	GetGroupMemberList(groupID int64) (any, error) // 注意：这里用 any，因为返回的是数组
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
	Platform   string
	EventType  string
	Scene      string
	SceneID    int64
	UserID     int64
	MessageID  int32
	Nickname   string
	Message    string
	RawMessage string
	RawEvent   any

	Reply     func(msg string) error
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
	msgID, _ := rawData["message_id"].(int32)

	event := &MessageEvent{
		Platform:   platform,
		EventType:  rawData["event_type"].(string),
		Scene:      scene,
		SceneID:    sceneID,
		UserID:     userID,
		MessageID:  msgID,
		Nickname:   rawData["nickname"].(string),
		Message:    rawData["message"].(string),
		RawMessage: rawData["raw_message"].(string),
		RawEvent:   rawData["raw_event"],
	}

	adapter, err := m.getAdapter(platform)
	if err != nil {
		fmt.Printf("[AdapterManager] 获取适配器失败: %v\n", err)
		return
	}

	event.Reply = func(msg string) error {
		if scene == "group" {
			return adapter.SendGroupMsg(sceneID, msg)
		}
		return adapter.SendPrivateMsg(userID, msg)
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

	m.ctx.Events.Publish("adapter.message", event)
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

func GetManager(ctx *core.SystemContext) *AdapterManager {
	if raw, ok := ctx.Registry.Get("adapter-manager"); ok {
		if m, ok := raw.(*AdapterManager); ok {
			return m
		}
	}
	return nil
}

func (m *AdapterManager) SendGroupMessage(platform string, groupID int64, msg string) error {
	adapter, err := m.getAdapter(platform)
	if err != nil {
		return err
	}
	return adapter.SendGroupMsg(groupID, msg)
}

func (m *AdapterManager) SendPrivateMessage(platform string, userID int64, msg string) error {
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

// 获取群信息
func (m *AdapterManager) GetGroupInfo(platform string, groupID int64, noCache bool) (*GroupInfo, error) {
	adapter, err := m.getAdapter(platform)
	if err != nil {
		return nil, err
	}

	resMap, err := adapter.GetGroupInfo(groupID, noCache)
	if err != nil {
		return nil, err
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

	resMap, err := adapter.GetGroupMemberInfo(groupID, userID, noCache)
	if err != nil {
		return nil, err
	}

	return parseMemberInfo(resMap), nil
}

// 获取群成员列表
func (m *AdapterManager) GetGroupMemberList(platform string, groupID int64) ([]GroupMemberInfo, error) {
	adapter, err := m.getAdapter(platform)
	if err != nil {
		return nil, err
	}

	// 底层返回的是 any (可能是 []interface{})
	rawList, err := adapter.GetGroupMemberList(groupID)
	if err != nil {
		return nil, err
	}

	memberList := make([]GroupMemberInfo, 0)

	// 断言为切片
	list, ok := rawList.([]any)
	if !ok {
		// 兼容某些框架直接返回 map 带有 data 字段的情况
		if mapData, ok := rawList.(map[string]any); ok {
			if dataArr, ok := mapData["data"].([]any); ok {
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
