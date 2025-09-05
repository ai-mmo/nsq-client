# NSQ Client

一个功能完整的 NSQ 客户端库，支持生产者、消费者以及失败消息处理机制。

## 功能特性

### 核心功能
- **生产者 (Producer)**：支持同步/异步消息发送
- **消费者 (Consumer)**：支持消息消费和处理
- **动态主题/频道创建**：自动创建不存在的主题和频道
- **连接管理**：支持多个 NSQD 节点和服务发现
- **配置管理**：灵活的 YAML 配置文件支持

### 高级功能
- **失败消息处理**：自动捕获失败消息并发送到失败队列 ⭐ **新功能**
- **重试机制**：可配置的消息重试策略
- **监控统计**：详细的运行时统计信息
- **日志记录**：完整的操作日志和错误跟踪
- **优雅关闭**：支持优雅的服务关闭

## 失败消息处理机制 ⭐

当消息消费失败时，系统会自动：

1. **捕获失败消息**：记录失败原因、时间戳、重试次数等信息
2. **发送到失败队列**：将失败消息发送到专门的失败队列
3. **提供查看工具**：可以查看和分析失败消息
4. **支持重新处理**：可以从失败队列重新处理消息

### 失败消息结构
```json
{
  "original_topic": "user_events",
  "original_channel": "user_processor", 
  "original_body": "原始消息内容",
  "message_id": "消息ID",
  "failure_reason": "具体失败原因",
  "failure_time": "2024-01-01T12:00:00Z",
  "attempt_count": 5,
  "stack_trace": "错误堆栈（可选）",
  "processor_info": "处理器信息",
  "hostname": "主机名",
  "timestamp": 1704110400
}
```

## 快速开始

### 1. 安装依赖

```bash
go mod tidy
```

### 2. 配置文件

复制并修改配置文件：

```bash
cp config.yaml config/my_config.yaml
```

在配置文件中启用失败消息处理：

```yaml
consumer:
  # ... 其他配置 ...
  failed_message:
    enabled: true
    topic_suffix: "_failed_channel"
    max_retry_attempts: 3
    retry_delay: 1000
    include_stack_trace: true
```

### 3. 运行示例

#### 启动带失败消息处理的消费者
```bash
go run examples/failed_message_handling/consumer/main.go config.yaml my_topic my_channel
```

#### 启动失败消息查看器
```bash
go run examples/failed_message_handling/viewer/main.go config.yaml my_topic
```

#### 发送测试消息
```bash
go run examples/producer_example.go config.yaml my_topic
```

### 4. 自动化测试

运行完整的失败消息处理测试：

```bash
./scripts/test_failed_message_handling.sh
```

## 使用示例

### 创建带失败消息处理的消费者

```go
package main

import (
    "nsq-client/client/consumer"
    "nsq-client/client/producer"
    "nsq-client/config"
)

func main() {
    // 加载配置
    cfg, err := config.LoadConfig("config.yaml")
    if err != nil {
        panic(err)
    }

    // 创建生产者用于发送失败消息
    prod, err := producer.NewProducer(cfg)
    if err != nil {
        panic(err)
    }
    defer prod.Stop()

    // 创建带失败消息处理器的消费者
    cons, err := consumer.NewConsumerWithFailedMessageHandler(
        cfg, 
        "my_topic", 
        "my_channel", 
        messageHandler, 
        prod, // 传入生产者用于发送失败消息
    )
    if err != nil {
        panic(err)
    }
    defer cons.Stop()

    // 启动消费者
    if err := cons.Start(); err != nil {
        panic(err)
    }

    // ... 等待退出信号 ...
}

func messageHandler(message *nsq.Message) error {
    // 你的消息处理逻辑
    // 如果返回错误，消息会在重试次数用完后被发送到失败队列
    return processMessage(message)
}
```

## 项目结构

```
nsq-client/
├── client/
│   ├── consumer/           # 消费者实现
│   │   ├── consumer.go
│   │   └── failed_message_handler.go  # 失败消息处理器 ⭐
│   └── producer/           # 生产者实现
│       └── producer.go
├── config/                 # 配置管理
│   ├── config.go
│   └── config.yaml
├── examples/               # 示例程序
│   ├── consumer_example.go
│   ├── failed_message_handling/                 # 失败消息处理示例 ⭐
│   │   ├── consumer/main.go                     # 带失败处理的消费者
│   │   └── viewer/main.go                       # 失败消息查看器
│   └── producer_example.go
├── docs/                   # 文档
│   └── failed_message_handling.md               # 失败消息处理文档 ⭐
└── scripts/                # 脚本
    └── test_failed_message_handling.sh          # 测试脚本 ⭐
```

## 配置说明

### 基础配置

```yaml
# NSQ 服务器配置
nsq:
  nsqd_addresses:
    - "172.16.1.40:4150"
  nsqlookupd_addresses:
    - "172.16.1.40:4161"

# 生产者配置
producer:
  default_topic: "test_topic"
  publish_timeout: 5000
  max_in_flight: 200

# 消费者配置
consumer:
  default_topic: "test_topic"
  default_channel: "test_channel"
  max_in_flight: 200
  msg_timeout: 60
  max_attempts: 5
  requeue_delay: 90000
```

### 失败消息处理配置 ⭐

```yaml
consumer:
  failed_message:
    enabled: true                    # 启用失败消息处理
    topic_suffix: "_failed_channel"  # 失败队列后缀
    max_retry_attempts: 3            # 发送失败消息的重试次数
    retry_delay: 1000               # 重试延迟（毫秒）
    include_stack_trace: true       # 包含错误堆栈
```

## 监控和运维

### 统计信息

消费者提供详细的统计信息，包括失败消息处理统计：

```go
stats := consumer.GetStats()
// 输出包含：
// - connection_count: 连接数
// - message_count: 消息总数
// - message_finished: 完成消息数
// - message_requeued: 重新排队消息数
// - failed_message_handler: 失败消息处理统计 ⭐
```

### 日志记录

系统提供完整的日志记录：
- 消息处理日志
- 失败消息处理日志 ⭐
- 连接状态日志
- 错误和异常日志

### 失败队列监控 ⭐

建议对失败队列进行监控：
- 失败消息数量告警
- 失败原因分析
- 处理时间监控
- 重试成功率统计

## 最佳实践

1. **失败消息处理**：
   - 启用失败消息处理功能
   - 定期检查失败队列
   - 分析失败原因并优化代码

2. **配置优化**：
   - 根据业务需求调整重试次数
   - 合理设置消息超时时间
   - 配置适当的并发数

3. **监控告警**：
   - 监控失败队列消息数量
   - 设置连接状态告警
   - 跟踪消息处理性能

4. **错误处理**：
   - 实现幂等性消息处理
   - 区分临时性和永久性错误
   - 提供人工干预机制

## 文档

- [失败消息处理详细文档](docs/failed_message_handling.md) ⭐
- [配置文件说明](config.yaml)
- [示例程序说明](examples/)

## 依赖

- Go 1.19+
- github.com/nsqio/go-nsq
- gopkg.in/yaml.v3
- mlog (日志库)

## 许可证

MIT License

## 贡献

欢迎提交 Issue 和 Pull Request！

## 更新日志

### v1.1.0 ⭐ 新版本
- ✨ 新增失败消息处理机制
- ✨ 新增失败消息查看器
- ✨ 新增失败消息处理配置
- ✨ 新增自动化测试脚本
- 📚 完善文档和示例

### v1.0.0
- 🎉 初始版本发布
- ✨ 支持生产者和消费者
- ✨ 支持动态主题/频道创建
- ✨ 支持配置管理和日志记录
