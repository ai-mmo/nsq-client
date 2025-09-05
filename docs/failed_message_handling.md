# NSQ 失败消息处理机制

## 概述

NSQ 客户端现在支持失败消息处理机制，当消息消费失败时，会自动将失败的消息重新投递到指定的失败队列中，便于后续的失败消息处理逻辑（如人工检查、重新处理、告警等）。

## 功能特性

1. **自动失败消息捕获**：捕获消息处理过程中的异常或错误
2. **失败队列投递**：将失败的消息重新投递到指定的失败队列中
3. **丰富的失败信息**：包含原始消息内容、失败原因、失败时间戳、重试次数等
4. **可配置的失败队列命名**：支持自定义失败队列的命名规则
5. **重试机制**：失败消息投递支持重试机制，确保投递成功
6. **堆栈跟踪**：可选择包含错误堆栈信息，便于问题排查
7. **统计信息**：提供失败消息处理的统计信息

## 配置说明

在 `config.yaml` 中添加失败消息处理配置：

```yaml
consumer:
  # ... 其他消费者配置 ...
  
  # 失败消息处理配置
  failed_message:
    # 是否启用失败消息处理
    enabled: true
    # 失败队列主题后缀
    topic_suffix: "_failed_channel"
    # 发送失败消息的最大重试次数
    max_retry_attempts: 3
    # 发送失败消息的重试延迟（毫秒）
    retry_delay: 1000
    # 是否包含错误堆栈信息
    include_stack_trace: true
```

### 配置参数说明

- `enabled`: 是否启用失败消息处理功能
- `topic_suffix`: 失败队列主题后缀，默认为 `_failed_channel`
- `max_retry_attempts`: 发送失败消息到失败队列的最大重试次数
- `retry_delay`: 发送失败消息的重试延迟时间（毫秒）
- `include_stack_trace`: 是否在失败消息中包含错误堆栈信息

## 失败队列命名规则

失败队列的主题名称 = 原始主题名称 + 失败队列后缀

例如：
- 原始主题：`user_events`
- 失败队列主题：`user_events_failed_channel`

## 失败消息结构

失败消息包含以下信息：

```json
{
  "original_topic": "user_events",
  "original_channel": "user_processor",
  "original_body": "原始消息内容的字节数组",
  "message_id": "消息ID",
  "failure_reason": "具体的失败原因",
  "failure_time": "2024-01-01T12:00:00Z",
  "attempt_count": 5,
  "stack_trace": "错误堆栈信息（可选）",
  "processor_info": "nsq-client-consumer",
  "hostname": "处理器主机名",
  "timestamp": 1704110400
}
```

## 使用方法

### 1. 创建带失败消息处理的消费者

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

### 2. 运行示例程序

#### 启动带失败消息处理的消费者

```bash
go run examples/consumer_with_failed_handler_example.go config.yaml my_topic my_channel
```

#### 启动失败消息查看器

```bash
go run examples/failed_message_viewer.go config.yaml my_topic
```

#### 启动生产者发送测试消息

```bash
go run examples/producer_example.go config.yaml my_topic
```

### 3. 查看失败消息

失败消息查看器会监听失败队列，并显示详细的失败消息信息：

```
=== 失败消息详情 ===
消息ID: 1234567890abcdef
原始主题: my_topic
原始频道: my_channel
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

## 最佳实践

### 1. 失败消息处理策略

- **监控告警**：设置失败队列的监控告警，及时发现问题
- **分类处理**：根据失败原因进行分类处理
- **重新处理**：对于临时性错误，可以尝试重新处理
- **人工审核**：对于业务逻辑错误，需要人工审核

### 2. 配置建议

- **生产环境**：建议启用堆栈跟踪，便于问题排查
- **重试次数**：根据业务需求合理设置重试次数
- **延迟时间**：设置适当的重试延迟，避免过于频繁的重试

### 3. 性能考虑

- **失败消息处理**：不会影响正常消息的处理性能
- **存储空间**：失败消息会占用额外的存储空间，需要定期清理
- **网络开销**：失败消息投递会产生额外的网络开销

## 故障排查

### 1. 失败消息未生成

- 检查配置文件中 `failed_message.enabled` 是否为 `true`
- 确认创建消费者时传入了生产者实例
- 查看日志中是否有相关错误信息

### 2. 失败消息投递失败

- 检查生产者连接状态
- 确认 NSQD 服务正常运行
- 查看重试次数和延迟配置是否合理

### 3. 失败队列消息堆积

- 检查失败消息查看器是否正常运行
- 确认失败消息处理逻辑是否存在问题
- 考虑增加失败消息处理的并发数

## 注意事项

1. **生产者依赖**：使用失败消息处理功能需要提供生产者实例
2. **配置一致性**：确保所有消费者使用相同的失败队列配置
3. **存储管理**：定期清理失败队列中的历史消息
4. **监控重要性**：建议对失败队列进行监控和告警
5. **错误处理**：即使失败消息处理失败，原消息也会被标记为完成，避免无限循环

## 扩展功能

可以基于失败消息处理机制实现更多功能：

1. **死信队列**：对于多次处理失败的消息，可以发送到死信队列
2. **自动重试**：实现定时从失败队列重新处理消息的机制
3. **告警通知**：集成告警系统，及时通知相关人员
4. **数据分析**：分析失败消息的模式，优化系统稳定性
