# adapter-manager
> `yes-core` 框架的核心中间件 —— 让你的业务插件脱离底层协议，实现真正的跨平台运行。

`adapter-manager` 扮演着“翻译官”和“调度中心”的角色。它监听底层适配器（如 `adapter-onebot`、`adapter-telegram`）抛出的原始事件，将其转换为**强类型的标准事件**，并注入丰富的操作闭包，让业务插件用最少的代码实现消息回复、撤回、禁言等功能。

## 安装与引入
在你的机器人 `main.go` 中匿名引入本插件，并确保它在业务插件之前加载：
```go
package main
import (
    "github.com/yeswearebot/yes-core/core"
    _ "github.com/yeswearebot/plugin_config"      // 基础配置中心
    _ "github.com/yeswearebot/adapter-onebot"     // 底层 QQ 协议端
    _ "github.com/yeswearebot/adapter-manager"    // 🌟 引入本管理器
    
    _ "github.com/yourname/my-plugin"             // 你的业务插件
)
func main() {
    app := core.NewApp()
    if err := app.Run(); err != nil {
        panic(err)
    }
}
```
---
## 快速开始：编写一个业务插件
只需依赖 `adapter-manager`，你就可以写出跨平台的机器人逻辑。以下是一个简单的 Ping-Pong 与群管示例：
```go
package my_plugin

import (
    "fmt"
    "time"
    adapter_manager "github.com/yeswearebot/adapter-manager"
    "github.com/yeswearebot/yes-core/core"
)

type MyPlugin struct{}
func init() {
    core.Register(func() core.Plugin { return &MyPlugin{} })
}

func (p *MyPlugin) Name() string        { return "my-plugin" }
func (p *MyPlugin) DependsOn() []string { return []string{"adapter-manager"} } // 依赖本插件

func (p *MyPlugin) Init(ctx *core.SystemContext) error {
    // 1. 监听标准化消息事件
    ctx.Events.Subscribe("adapter.message", func(payload any) {
        event, ok := payload.(*adapter_manager.MessageEvent)

        if !ok {
            return
        }

        fmt.Printf("[%s][%s] %s(%d): %s\n", 
            event.Platform, event.Scene, event.Nickname, event.UserID, event.Message)

        // 🌟 极简回复：直接调用闭包
        if event.Message == "ping" {
            event.Reply("pong!")
        }

        // 🌟 跨平台群管操作
        if event.Message == "违禁词" {
            event.Delete()           // 撤回消息
            event.BanSender(60)      // 禁言发送者 60 秒
            event.Reply("已处理违禁词")
        }
    })

    // 2. 主动调用示例 (定时任务等)
    go func() {
        time.Sleep(5 * time.Second)
        
        // 获取 Manager 实例
        manager := adapter_manager.GetManager(ctx)
        if manager != nil {
            // 主动给某个群发消息
            manager.SendGroupMessage("adapter-onebot", 123456789, "早安！")
        }
    }()
    
    return nil
}

func (p *MyPlugin) Start(ctx *core.SystemContext) error { return nil }

func (p *MyPlugin) Stop(ctx *core.SystemContext) error  { return nil }
```
---
## API 手册
### 1. 消息事件 (`MessageEvent`)
当收到消息时，`adapter-manager` 会将原始数据封装为 `MessageEvent` 并发布到 `adapter.message` 频道。
**字段说明：**
| 字段名 | 类型 | 说明 |
| :--- | :--- | :--- |
| `Platform` | `string` | 适配器名称 (如 `"adapter-onebot"`) |
| `EventType` | `string` | 事件类型 (`"group_msg"` 或 `"private_msg"`) |
| `Scene` | `string` | 场景 (`"group"` 群聊 或 `"private"` 私聊) |
| `SceneID` | `int64` | 场景 ID (群号 或 私聊对方 QQ) |
| `UserID` | `int64` | 发送者 QQ |
| `MessageID` | `int32` | 消息 ID (供撤回使用) |
| `Nickname` | `string` | 发送者昵称 |
| `Message` | `string` | 解析后的消息内容 |
| `RawMessage` | `string` | 原始消息 (如 CQ 码字符串) |
| `RawEvent` | `any` | 底层原始事件结构体，可断言为特定类型获取特殊字段 |
**绑定方法 (闭包)：**
| 方法 | 参数 | 说明 |
| :--- | :--- | :--- |
| `Reply(msg)` | `string` | 回复当前消息发送者 (自动判断群聊/私聊) |
| `Delete()` | - | 撤回当前消息 |
| `BanSender(duration)` | `int64` | 禁言当前发送者 (秒，仅群聊有效，0为解禁) |
---
### 2. 主动操作 (`AdapterManager`)
在非消息上下文（如定时任务、Webhook）中，你可以通过 `GetManager(ctx)` 获取管理器实例，进行主动操作。
```go
manager := adapter_manager.GetManager(ctx)
```
**通用操作：**
- `manager.SendGroupMessage(platform, groupID, msg)` - 主动发群消息
- `manager.SendPrivateMessage(platform, userID, msg)` - 主动发私聊消息
- `manager.BanGroupMember(platform, groupID, userID, duration)` - 主动禁言某人
**信息获取 (返回强类型结构体)：**
- `manager.GetGroupInfo(platform, groupID, noCache)` 
  返回 `*GroupInfo` (包含 `GroupID`, `GroupName`, `MemberCount`, `MaxMemberCount`)
- `manager.GetGroupMemberInfo(platform, groupID, userID, noCache)` 
  返回 `*GroupMemberInfo` (包含 `GroupID`, `UserID`, `Nickname`, `Card`, `Role`)
- `manager.GetGroupMemberList(platform, groupID)` 
  返回 `[]GroupMemberInfo`

**使用示例：**
```go
info, err := manager.GetGroupInfo(event.Platform, event.SceneID, false)

if err == nil {
    msg := fmt.Sprintf("群名: %s\n成员数: %d / %d", info.GroupName, info.MemberCount, info.MaxMemberCount)
    event.Reply(msg)
}
```
---
## 进阶：处理平台特有逻辑
虽然 `adapter-manager` 抽象了通用逻辑，但如果你确实需要读取某个平台特有的字段（例如 Onebot 的群成员等级 `Level`），可以通过 `RawEvent` 进行断言获取：
```go
ctx.Events.Subscribe("adapter.message", func(payload any) {
    event, _ := payload.(*adapter_manager.MessageEvent)
    // 仅在 onebot 平台下执行
    if event.Platform == "adapter-onebot" {
        // 假设你在 adapter-onebot 中抛出的 raw_event 是 Pichubot.MessageGroup
        // 你需要引入 Pichubot 包进行断言
        /*
        rawGroupEvent, ok := event.RawEvent.(Pichubot.MessageGroup)
        if ok {
            fmt.Println("发送者群角色:", rawGroupEvent.Sender.Role)
            fmt.Println("发送者群等级:", rawGroupEvent.Sender.Level)
        }
        */
    }
})
```
---
## 适配器开发者指南
如果你想为 `yes-core` 编写一个新的适配器（例如 `adapter-telegram`），只需实现 `adapter-manager` 定义的 `Adapter` 接口，并在内核中注册即可自动接入：
```go
// 你的适配器必须实现以下所有方法：
type Adapter interface {
    SendGroupMsg(groupID int64, message string) error
    SendPrivateMsg(userID int64, message string) error
    DeleteMsg(messageID int32) error
    SetGroupBan(groupID, userID, duration int64) error
    GetGroupInfo(groupID int64, noCache bool) (map[string]any, error)
    GetGroupMemberInfo(groupID, userID int64, noCache bool) (map[string]any, error)
    GetGroupMemberList(groupID int64) (any, error)
}
```
同时，在你的适配器事件监听器中，将原始事件打包成包含 `platform`, `event_type`, `scene`, `scene_id` 等字段的 `map[string]any`，并发布到 `adapter.raw.message` 频道即可。
