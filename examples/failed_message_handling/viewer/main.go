package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"nsq-client/client/consumer"
	"nsq-client/config"

	"mlog"

	"github.com/nsqio/go-nsq"
)

// FailedMessage 失败消息结构（与失败消息处理器中的结构保持一致）
type FailedMessage struct {
	// 原始消息信息
	OriginalTopic   string    `json:"original_topic"`   // 原始主题
	OriginalChannel string    `json:"original_channel"` // 原始频道
	OriginalBody    []byte    `json:"original_body"`    // 原始消息内容
	MessageID       string    `json:"message_id"`       // 消息ID
	
	// 失败信息
	FailureReason   string    `json:"failure_reason"`   // 失败原因
	FailureTime     time.Time `json:"failure_time"`     // 失败时间
	AttemptCount    uint16    `json:"attempt_count"`    // 重试次数
	StackTrace      string    `json:"stack_trace"`      // 错误堆栈（可选）
	
	// 元数据
	ProcessorInfo   string    `json:"processor_info"`   // 处理器信息
	Hostname        string    `json:"hostname"`         // 主机名
	Timestamp       int64     `json:"timestamp"`        // 时间戳
}

// FailedMessageViewer 失败消息查看器
type FailedMessageViewer struct {
	viewedCount   int64
	processedCount int64
	errorCount    int64
}

// NewFailedMessageViewer 创建失败消息查看器
func NewFailedMessageViewer() *FailedMessageViewer {
	return &FailedMessageViewer{}
}

// ProcessFailedMessage 处理失败消息
func (fmv *FailedMessageViewer) ProcessFailedMessage(message *nsq.Message) error {
	// 解析失败消息
	var failedMsg FailedMessage
	if err := json.Unmarshal(message.Body, &failedMsg); err != nil {
		mlog.Error("解析失败消息失败: message_body=%s, error=%v", string(message.Body), err)
		fmv.errorCount++
		return fmt.Errorf("解析失败消息失败: %w", err)
	}

	fmv.viewedCount++

	// 记录失败消息详细信息
	mlog.Info("=== 失败消息详情 ===")
	mlog.Info("消息ID: %s", failedMsg.MessageID)
	mlog.Info("原始主题: %s", failedMsg.OriginalTopic)
	mlog.Info("原始频道: %s", failedMsg.OriginalChannel)
	mlog.Info("失败原因: %s", failedMsg.FailureReason)
	mlog.Info("失败时间: %s", failedMsg.FailureTime.Format("2006-01-02 15:04:05"))
	mlog.Info("重试次数: %d", failedMsg.AttemptCount)
	mlog.Info("处理器信息: %s", failedMsg.ProcessorInfo)
	mlog.Info("主机名: %s", failedMsg.Hostname)
	mlog.Info("原始消息长度: %d bytes", len(failedMsg.OriginalBody))
	
	// 如果有堆栈跟踪信息，记录它
	if failedMsg.StackTrace != "" {
		mlog.Debug("错误堆栈:\n%s", failedMsg.StackTrace)
	}

	// 显示原始消息内容（如果是JSON格式，尝试格式化）
	if len(failedMsg.OriginalBody) > 0 {
		var originalMsg interface{}
		if err := json.Unmarshal(failedMsg.OriginalBody, &originalMsg); err == nil {
			// 原始消息是有效的JSON，格式化显示
			if formattedJSON, err := json.MarshalIndent(originalMsg, "", "  "); err == nil {
				mlog.Info("原始消息内容 (JSON):\n%s", string(formattedJSON))
			} else {
				mlog.Info("原始消息内容: %s", string(failedMsg.OriginalBody))
			}
		} else {
			// 原始消息不是JSON，直接显示
			mlog.Info("原始消息内容: %s", string(failedMsg.OriginalBody))
		}
	}

	mlog.Info("=== 失败消息详情结束 ===")

	// 模拟失败消息处理逻辑
	if err := fmv.handleFailedMessage(&failedMsg); err != nil {
		mlog.Error("处理失败消息时发生错误: message_id=%s, error=%v", failedMsg.MessageID, err)
		fmv.errorCount++
		return err
	}

	fmv.processedCount++
	mlog.Info("失败消息处理完成: message_id=%s, viewed_count=%d", failedMsg.MessageID, fmv.viewedCount)

	return nil
}

// handleFailedMessage 处理失败消息的具体逻辑
func (fmv *FailedMessageViewer) handleFailedMessage(failedMsg *FailedMessage) error {
	// 这里可以实现具体的失败消息处理逻辑，例如：
	// 1. 记录到数据库
	// 2. 发送告警通知
	// 3. 尝试重新处理
	// 4. 人工审核标记
	
	// 模拟处理时间
	time.Sleep(100 * time.Millisecond)

	// 根据失败原因进行不同的处理
	switch {
	case contains(failedMsg.FailureReason, "数据库连接失败"):
		mlog.Info("检测到数据库连接失败，建议检查数据库状态: message_id=%s", failedMsg.MessageID)
		// 可以在这里实现数据库连接检查逻辑
		
	case contains(failedMsg.FailureReason, "外部API调用失败"):
		mlog.Info("检测到外部API调用失败，建议检查API服务状态: message_id=%s", failedMsg.MessageID)
		// 可以在这里实现API健康检查逻辑
		
	case contains(failedMsg.FailureReason, "业务逻辑验证失败"):
		mlog.Info("检测到业务逻辑验证失败，建议人工审核: message_id=%s", failedMsg.MessageID)
		// 可以在这里实现人工审核流程
		
	case contains(failedMsg.FailureReason, "网络超时"):
		mlog.Info("检测到网络超时，建议检查网络连接: message_id=%s", failedMsg.MessageID)
		// 可以在这里实现网络连接检查逻辑
		
	default:
		mlog.Info("未知失败类型，记录用于后续分析: message_id=%s", failedMsg.MessageID)
	}

	return nil
}

// contains 检查字符串是否包含子字符串
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || 
		(len(s) > len(substr) && 
			(s[:len(substr)] == substr || s[len(s)-len(substr):] == substr || 
				func() bool {
					for i := 1; i <= len(s)-len(substr); i++ {
						if s[i:i+len(substr)] == substr {
							return true
						}
					}
					return false
				}())))
}

// GetStats 获取查看器统计信息
func (fmv *FailedMessageViewer) GetStats() map[string]interface{} {
	return map[string]interface{}{
		"viewed_count":    fmv.viewedCount,
		"processed_count": fmv.processedCount,
		"error_count":     fmv.errorCount,
	}
}

func main() {
	// 加载配置
	configPath := "config/config.yaml"
	if len(os.Args) > 1 {
		configPath = os.Args[1]
	}

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		panic(fmt.Sprintf("加载配置失败: %v", err))
	}

	// 初始化 mlog
	zapConfig := mlog.ZapConfig{
		Level:           cfg.Logging.Level,
		Prefix:          cfg.Logging.Prefix,
		Format:          cfg.Logging.Format,
		Director:        cfg.Logging.Director,
		EncodeLevel:     cfg.Logging.EncodeLevel,
		StacktraceKey:   cfg.Logging.StacktraceKey,
		ShowLine:        cfg.Logging.ShowLine,
		LogInConsole:    cfg.Logging.LogInConsole,
		RetentionDay:    cfg.Logging.RetentionDay,
		MaxSize:         cfg.Logging.MaxSize,
		MaxBackups:      cfg.Logging.MaxBackups,
		EnableSplit:     cfg.Logging.EnableSplit,
		EnableCompress:  cfg.Logging.EnableCompress,
		EnableAsync:     cfg.Logging.EnableAsync,
		AsyncBufferSize: cfg.Logging.AsyncBufferSize,
		AsyncDropOnFull: cfg.Logging.AsyncDropOnFull,
		UseRelativePath: cfg.Logging.UseRelativePath,
		BuildRootPath:   cfg.Logging.BuildRootPath,
	}
	mlog.InitialZap("nsq-failed-message-viewer", 1, cfg.Logging.Level, zapConfig)

	mlog.Info("启动 NSQ 失败消息查看器")
	mlog.Info("配置加载成功: config_path=%s", configPath)

	// 创建失败消息查看器
	viewer := NewFailedMessageViewer()

	// 获取失败队列主题和频道配置
	originalTopic := cfg.Consumer.DefaultTopic
	if len(os.Args) > 2 {
		originalTopic = os.Args[2]
	}

	// 生成失败队列主题名称
	failedTopicSuffix := cfg.Consumer.FailedMessage.TopicSuffix
	if failedTopicSuffix == "" {
		failedTopicSuffix = "_failed_channel"
	}
	failedTopic := originalTopic + failedTopicSuffix
	
	// 使用专门的频道来查看失败消息
	viewerChannel := "failed_message_viewer"

	mlog.Info("失败消息查看器配置: original_topic=%s, failed_topic=%s, viewer_channel=%s",
		originalTopic, failedTopic, viewerChannel)

	// 创建消费者来监听失败队列
	cons, err := consumer.NewConsumer(cfg, failedTopic, viewerChannel, viewer.ProcessFailedMessage)
	if err != nil {
		mlog.Error("创建失败消息查看器消费者失败: %v", err)
		panic(err)
	}
	defer cons.Stop()

	// 设置信号处理
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// 创建上下文
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 启动消费者
	if err := cons.Start(); err != nil {
		mlog.Error("启动失败消息查看器失败: %v", err)
		panic(err)
	}
	mlog.Info("失败消息查看器启动成功，开始监听失败消息...")

	// 启动统计信息打印协程
	go func() {
		printViewerStats(ctx, cons, viewer)
	}()

	// 等待退出信号
	<-sigChan
	mlog.Info("接收到退出信号，正在关闭失败消息查看器...")
	cancel()

	// 等待一段时间让消息处理完成
	time.Sleep(2 * time.Second)
	mlog.Info("失败消息查看器已退出")
}

// printViewerStats 打印查看器统计信息
func printViewerStats(ctx context.Context, cons *consumer.Consumer, viewer *FailedMessageViewer) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// 打印消费者统计信息
			consumerStats := cons.GetStats()
			mlog.Info("失败消息查看器消费者统计: stats=%v", consumerStats)

			// 打印查看器统计信息
			viewerStats := viewer.GetStats()
			mlog.Info("失败消息查看器统计: stats=%v", viewerStats)

			// 检查连接状态
			if !cons.IsConnected() {
				mlog.Warn("失败消息查看器连接已断开")
			}
		}
	}
}
