package nsq_client

import (
	"encoding/json"
	"fmt"
	"mlog"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/nsqio/go-nsq"
)

// FailedMessage 失败消息结构
type FailedMessage struct {
	// 原始消息信息
	OriginalTopic   string `json:"original_topic"`   // 原始主题
	OriginalChannel string `json:"original_channel"` // 原始频道
	OriginalBody    []byte `json:"original_body"`    // 原始消息内容
	MessageID       string `json:"message_id"`       // 消息ID

	// 失败信息
	FailureReason string    `json:"failure_reason"` // 失败原因
	FailureTime   time.Time `json:"failure_time"`   // 失败时间
	AttemptCount  uint16    `json:"attempt_count"`  // 重试次数
	StackTrace    string    `json:"stack_trace"`    // 错误堆栈（可选）

	// 元数据
	ProcessorInfo string `json:"processor_info"` // 处理器信息
	Hostname      string `json:"hostname"`       // 主机名
	Timestamp     int64  `json:"timestamp"`      // 时间戳
}

// FailedMessageHandler 失败消息处理器
type FailedMessageHandler struct {
	config          *Config
	producer        MessageProducer // 用于发送失败消息的生产者接口
	mu              sync.RWMutex    // 保护并发访问
	failureCount    int64           // 连续失败次数
	lastFailureTime time.Time       // 最后失败时间
	circuitOpen     bool            // 熔断器状态
}

// MessageProducer 消息生产者接口，用于解耦
type MessageProducer interface {
	Publish(topic string, message []byte) error
}

// NewFailedMessageHandler 创建失败消息处理器
func NewFailedMessageHandler(cfg *Config, producer MessageProducer) *FailedMessageHandler {
	return &FailedMessageHandler{
		config:   cfg,
		producer: producer,
	}
}

// HandleFailedMessage 处理失败的消息
func (fmh *FailedMessageHandler) HandleFailedMessage(
	originalTopic, originalChannel string,
	message *nsq.Message,
	failureReason error,
) error {
	// 防护措施：捕获panic，确保失败消息处理不会导致程序崩溃
	defer func() {
		if r := recover(); r != nil {
			mlog.Error("失败消息处理发生panic，已恢复: topic=%s, channel=%s, panic=%v",
				originalTopic, originalChannel, r)
			// 记录堆栈信息
			buf := make([]byte, 4096)
			n := runtime.Stack(buf, false)
			mlog.Error("失败消息处理panic堆栈信息:\n%s", string(buf[:n]))
		}
	}()

	// 参数安全检查
	if fmh == nil {
		mlog.Error("失败消息处理器为空，跳过处理")
		return fmt.Errorf("失败消息处理器为空")
	}

	if fmh.config == nil {
		mlog.Error("失败消息处理器配置为空，跳过处理")
		return fmt.Errorf("失败消息处理器配置为空")
	}

	if message == nil {
		mlog.Error("消息为空，跳过失败消息处理")
		return fmt.Errorf("消息为空")
	}

	if failureReason == nil {
		mlog.Error("失败原因为空，跳过失败消息处理")
		return fmt.Errorf("失败原因为空")
	}

	// 检查是否启用失败消息处理
	if !fmh.config.Consumer.FailedMessage.Enabled {
		mlog.Debug("失败消息处理未启用，跳过处理: topic=%s, channel=%s", originalTopic, originalChannel)
		return nil
	}

	// 记录失败消息处理开始
	mlog.Info("开始处理失败消息: topic=%s, channel=%s, message_id=%s, attempts=%d, reason=%v",
		originalTopic, originalChannel, string(message.ID[:]), message.Attempts, failureReason)

	// 构建失败消息
	failedMsg, err := fmh.buildFailedMessage(originalTopic, originalChannel, message, failureReason)
	if err != nil {
		mlog.Error("构建失败消息失败: %v", err)
		return fmt.Errorf("构建失败消息失败: %w", err)
	}

	// 序列化失败消息
	failedMsgBytes, err := json.Marshal(failedMsg)
	if err != nil {
		mlog.Error("序列化失败消息失败: %v", err)
		return fmt.Errorf("序列化失败消息失败: %w", err)
	}

	// 生成失败队列主题名称
	failedTopic := fmh.generateFailedTopicName(originalTopic)

	// 检查熔断器状态
	if fmh.isCircuitOpen() {
		mlog.Warn("失败消息处理熔断器已开启，跳过处理: topic=%s, failed_topic=%s", originalTopic, failedTopic)
		return fmt.Errorf("失败消息处理熔断器已开启")
	}

	// 发送失败消息到失败队列，带重试机制
	if err := fmh.publishFailedMessageWithRetry(failedTopic, failedMsgBytes); err != nil {
		// 记录失败并更新熔断器状态
		fmh.recordFailure()
		mlog.Error("发送失败消息到失败队列失败: topic=%s, failed_topic=%s, error=%v",
			originalTopic, failedTopic, err)
		return fmt.Errorf("发送失败消息到失败队列失败: %w", err)
	}

	// 记录成功并重置熔断器
	fmh.recordSuccess()

	mlog.Info("失败消息已成功发送到失败队列: original_topic=%s, failed_topic=%s, message_id=%s",
		originalTopic, failedTopic, string(message.ID[:]))

	return nil
}

// buildFailedMessage 构建失败消息
func (fmh *FailedMessageHandler) buildFailedMessage(
	originalTopic, originalChannel string,
	message *nsq.Message,
	failureReason error,
) (*FailedMessage, error) {
	// 获取主机名
	hostname := fmh.getHostname()

	// 构建失败消息
	failedMsg := &FailedMessage{
		OriginalTopic:   originalTopic,
		OriginalChannel: originalChannel,
		OriginalBody:    message.Body,
		MessageID:       string(message.ID[:]),
		FailureReason:   failureReason.Error(),
		FailureTime:     time.Now(),
		AttemptCount:    message.Attempts,
		ProcessorInfo:   "nsq-client-consumer",
		Hostname:        hostname,
		Timestamp:       time.Now().Unix(),
	}

	// 如果配置启用了堆栈跟踪，添加堆栈信息
	if fmh.config.Consumer.FailedMessage.IncludeStackTrace {
		failedMsg.StackTrace = fmh.getStackTrace()
	}

	return failedMsg, nil
}

// generateFailedTopicName 生成失败队列主题名称
func (fmh *FailedMessageHandler) generateFailedTopicName(originalTopic string) string {
	suffix := fmh.config.Consumer.FailedMessage.TopicSuffix
	if suffix == "" {
		suffix = "_failed_channel" // 默认后缀
	}
	return originalTopic + suffix
}

// publishFailedMessageWithRetry 带重试机制发送失败消息
func (fmh *FailedMessageHandler) publishFailedMessageWithRetry(topic string, message []byte) error {
	maxRetries := fmh.config.Consumer.FailedMessage.MaxRetryAttempts
	retryDelay := time.Duration(fmh.config.Consumer.FailedMessage.RetryDelay) * time.Millisecond

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		// 尝试发送消息
		err := fmh.producer.Publish(topic, message)
		if err == nil {
			// 发送成功
			if attempt > 0 {
				mlog.Info("失败消息发送成功（重试%d次后）: topic=%s", attempt, topic)
			}
			return nil
		}

		lastErr = err
		mlog.Warn("发送失败消息失败（第%d次尝试）: topic=%s, error=%v", attempt+1, topic, err)

		// 如果不是最后一次尝试，等待后重试
		if attempt < maxRetries {
			mlog.Debug("等待%v后重试发送失败消息: topic=%s", retryDelay, topic)
			time.Sleep(retryDelay)
		}
	}

	return fmt.Errorf("发送失败消息重试%d次后仍然失败: %w", maxRetries, lastErr)
}

// getHostname 获取主机名
func (fmh *FailedMessageHandler) getHostname() string {
	// 这里可以根据需要获取主机名或其他标识信息
	// 为了简化，返回一个固定值，实际使用中可以获取真实主机名
	return "nsq-consumer-host"
}

// getStackTrace 获取错误堆栈跟踪
func (fmh *FailedMessageHandler) getStackTrace() string {
	// 获取调用栈信息
	buf := make([]byte, 4096)
	n := runtime.Stack(buf, false)
	stackTrace := string(buf[:n])

	// 过滤掉不相关的堆栈信息，只保留业务相关的部分
	lines := strings.Split(stackTrace, "\n")
	var filteredLines []string

	for i, line := range lines {
		// 跳过 runtime 和 nsq 内部的堆栈信息
		if strings.Contains(line, "runtime.") ||
			strings.Contains(line, "github.com/nsqio/go-nsq") ||
			strings.Contains(line, "failed_message_handler.go") {
			continue
		}

		// 保留业务相关的堆栈信息
		if strings.Contains(line, "examples/") ||
			strings.Contains(line, "client/consumer") ||
			i < 10 { // 保留前10行以确保有足够的上下文
			filteredLines = append(filteredLines, line)
		}
	}

	return strings.Join(filteredLines, "\n")
}

// GetFailedMessageStats 获取失败消息处理统计信息
func (fmh *FailedMessageHandler) GetFailedMessageStats() map[string]interface{} {
	fmh.mu.RLock()
	defer fmh.mu.RUnlock()

	return map[string]interface{}{
		"enabled":             fmh.config.Consumer.FailedMessage.Enabled,
		"topic_suffix":        fmh.config.Consumer.FailedMessage.TopicSuffix,
		"max_retry_attempts":  fmh.config.Consumer.FailedMessage.MaxRetryAttempts,
		"retry_delay_ms":      fmh.config.Consumer.FailedMessage.RetryDelay,
		"include_stack_trace": fmh.config.Consumer.FailedMessage.IncludeStackTrace,
		"failure_count":       fmh.failureCount,
		"circuit_open":        fmh.circuitOpen,
		"last_failure_time":   fmh.lastFailureTime.Format("2006-01-02 15:04:05"),
	}
}

// isCircuitOpen 检查熔断器是否开启
func (fmh *FailedMessageHandler) isCircuitOpen() bool {
	fmh.mu.RLock()
	defer fmh.mu.RUnlock()

	// 如果熔断器已开启，检查是否可以尝试恢复
	if fmh.circuitOpen {
		// 熔断器开启后，等待30秒再尝试恢复
		if time.Since(fmh.lastFailureTime) > 30*time.Second {
			mlog.Info("熔断器恢复尝试，重置状态")
			fmh.mu.RUnlock()
			fmh.mu.Lock()
			fmh.circuitOpen = false
			fmh.failureCount = 0
			fmh.mu.Unlock()
			fmh.mu.RLock()
		}
	}

	return fmh.circuitOpen
}

// recordFailure 记录失败并更新熔断器状态
func (fmh *FailedMessageHandler) recordFailure() {
	fmh.mu.Lock()
	defer fmh.mu.Unlock()

	fmh.failureCount++
	fmh.lastFailureTime = time.Now()

	// 连续失败5次后开启熔断器
	if fmh.failureCount >= 5 {
		fmh.circuitOpen = true
		mlog.Warn("失败消息处理连续失败%d次，开启熔断器", fmh.failureCount)
	}
}

// recordSuccess 记录成功并重置熔断器
func (fmh *FailedMessageHandler) recordSuccess() {
	fmh.mu.Lock()
	defer fmh.mu.Unlock()

	if fmh.failureCount > 0 || fmh.circuitOpen {
		mlog.Info("失败消息处理成功，重置熔断器状态")
		fmh.failureCount = 0
		fmh.circuitOpen = false
		fmh.lastFailureTime = time.Time{}
	}
}
