# 失败消息处理示例

本目录包含NSQ失败消息处理机制的示例程序。

## 目录结构

```
failed_message_handling/
├── consumer/main.go    # 带失败消息处理的消费者示例
├── viewer/main.go      # 失败消息查看器示例
└── README.md          # 本文件
```

## 功能说明

### 消费者示例 (consumer/main.go)

这个示例展示了如何创建一个带失败消息处理功能的消费者：

- **自动失败处理**：当消息处理失败时，自动发送到失败队列
- **模拟失败场景**：包含多种失败类型的模拟（数据库连接失败、API调用失败等）
- **详细日志记录**：记录消息处理过程和失败信息
- **统计信息**：提供处理成功、失败的统计数据

### 失败消息查看器 (viewer/main.go)

这个示例展示了如何查看和处理失败队列中的消息：

- **失败消息解析**：解析失败队列中的消息结构
- **详细信息显示**：显示失败原因、时间、重试次数等信息
- **分类处理**：根据失败类型进行不同的处理逻辑
- **原始消息恢复**：显示原始消息内容，便于问题排查

## 运行示例

### 1. 启动带失败消息处理的消费者

```bash
# 从项目根目录运行
go run examples/failed_message_handling/consumer/main.go config/config.yaml test_topic test_channel
```

参数说明：
- `config/config.yaml`: 配置文件路径
- `test_topic`: 要监听的主题名称
- `test_channel`: 要监听的频道名称

### 2. 启动失败消息查看器

```bash
# 从项目根目录运行
go run examples/failed_message_handling/viewer/main.go config/config.yaml test_topic
```

参数说明：
- `config/config.yaml`: 配置文件路径
- `test_topic`: 原始主题名称（失败队列会自动添加后缀）

### 3. 发送测试消息

```bash
# 从项目根目录运行
go run examples/producer_example.go config/config.yaml test_topic 20
```

这会发送20条测试消息，其中一些会故意失败以演示失败消息处理。

## 配置要求

确保在 `config/config.yaml` 中启用了失败消息处理：

```yaml
consumer:
  failed_message:
    enabled: true
    topic_suffix: "_failed_channel"
    max_retry_attempts: 3
    retry_delay: 1000
    include_stack_trace: true
```

## 观察结果

### 消费者日志示例

```
[INFO] 开始处理消息: message_id=15, content=test message, source=producer_example, attempts=1
[ERROR] 消息处理失败: message_id=15, error=数据库连接失败 - 消息ID: 15
[INFO] 失败消息已成功发送到失败队列: original_topic=test_topic, failed_topic=test_topic_failed_channel
```

### 失败消息查看器日志示例

```
=== 失败消息详情 ===
消息ID: 1234567890abcdef
原始主题: test_topic
原始频道: test_channel
失败原因: 数据库连接失败 - 消息ID: 15
失败时间: 2024-01-01 12:00:00
重试次数: 5
处理器信息: nsq-client-consumer
主机名: nsq-consumer-host
原始消息长度: 128 bytes
原始消息内容 (JSON):
{
  "id": 15,
  "content": "test message",
  "timestamp": "2024-01-01T12:00:00Z",
  "source": "producer_example"
}
=== 失败消息详情结束 ===
```

## 失败类型模拟

消费者示例模拟了以下失败类型：

1. **数据库连接失败** (15% 概率)
2. **外部API调用失败** (12% 概率)  
3. **业务逻辑验证失败** (10% 概率)
4. **网络超时** (8% 概率)

这些失败类型帮助测试不同场景下的失败消息处理。

## 自定义处理逻辑

你可以修改示例代码来实现自己的业务逻辑：

### 在消费者中自定义消息处理

```go
func (mp *MessageProcessorWithFailedHandler) ProcessMessage(message *nsq.Message) error {
    // 你的消息处理逻辑
    return processYourBusinessLogic(message)
}
```

### 在查看器中自定义失败消息处理

```go
func (fmv *FailedMessageViewer) handleFailedMessage(failedMsg *FailedMessage) error {
    // 根据失败原因进行不同处理
    switch {
    case contains(failedMsg.FailureReason, "your_error_type"):
        // 你的处理逻辑
        return handleYourErrorType(failedMsg)
    }
    return nil
}
```

## 注意事项

1. **确保NSQ服务运行**：需要NSQ服务正常运行
2. **配置文件正确**：确保配置文件路径和内容正确
3. **失败队列监控**：建议监控失败队列的消息数量
4. **日志文件管理**：注意日志文件的大小和清理

## 扩展功能

基于这些示例，你可以实现：

1. **告警通知**：当失败消息数量超过阈值时发送告警
2. **自动重试**：定时从失败队列重新处理消息
3. **数据分析**：分析失败模式，优化系统稳定性
4. **人工审核**：提供Web界面进行人工审核和处理

## 故障排查

如果遇到问题，请检查：

1. NSQ服务是否正常运行
2. 配置文件是否正确
3. 网络连接是否正常
4. 日志文件中的错误信息
