package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	nsq_client "github.com/ai-mmo/nsq-client"

	"mlog"

	"github.com/nsqio/go-nsq"
)

// ConsumerMessageWithFailedHandler 示例消息结构（带失败处理）
type ConsumerMessageWithFailedHandler struct {
	ID        int       `json:"id"`
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
	Source    string    `json:"source"`
}

// MessageProcessorWithFailedHandler 带失败消息处理的消息处理器
type MessageProcessorWithFailedHandler struct {
	processedCount int64
	errorCount     int64
	failedCount    int64
}

// NewMessageProcessorWithFailedHandler 创建消息处理器
func NewMessageProcessorWithFailedHandler() *MessageProcessorWithFailedHandler {
	return &MessageProcessorWithFailedHandler{}
}

// ProcessMessage 处理消息的具体逻辑
func (mp *MessageProcessorWithFailedHandler) ProcessMessage(message *nsq.Message) error {
	// 解析消息
	var exampleMsg ConsumerMessageWithFailedHandler
	if err := json.Unmarshal(message.Body, &exampleMsg); err != nil {
		mlog.Error("解析消息失败: message_body=%s, error=%v", string(message.Body), err)
		mp.errorCount++
		return fmt.Errorf("解析消息失败: %w", err)
	}

	// 记录消息处理开始
	mlog.Info("开始处理消息: message_id=%d, content=%s, source=%s, timestamp=%v, attempts=%d",
		exampleMsg.ID, exampleMsg.Content, exampleMsg.Source, exampleMsg.Timestamp, message.Attempts)

	// 模拟消息处理逻辑
	if err := mp.simulateProcessing(&exampleMsg); err != nil {
		mlog.Error("消息处理失败: message_id=%d, error=%v", exampleMsg.ID, err)
		mp.errorCount++
		return err
	}

	// 处理成功
	mp.processedCount++
	mlog.Info("消息处理成功: message_id=%d, processed_count=%d",
		exampleMsg.ID, mp.processedCount)

	return nil
}

// simulateProcessing 模拟消息处理逻辑（包含更多失败场景）
func (mp *MessageProcessorWithFailedHandler) simulateProcessing(msg *ConsumerMessageWithFailedHandler) error {
	// 模拟不同类型消息的处理时间
	switch msg.Source {
	case "producer_example":
		// 普通消息，快速处理
		time.Sleep(100 * time.Millisecond)
	case "producer_example_batch":
		// 批量消息，稍慢处理
		time.Sleep(200 * time.Millisecond)
	case "producer_example_heartbeat":
		// 心跳消息，立即处理
		time.Sleep(10 * time.Millisecond)
	default:
		// 未知类型消息
		time.Sleep(500 * time.Millisecond)
	}

	// 模拟不同类型的处理失败情况
	switch {
	case msg.ID%15 == 0:
		// 15% 概率：数据库连接失败
		return fmt.Errorf("数据库连接失败 - 消息ID: %d", msg.ID)
	case msg.ID%12 == 0:
		// 12% 概率：外部API调用失败
		return fmt.Errorf("外部API调用失败 - 消息ID: %d", msg.ID)
	case msg.ID%10 == 0:
		// 10% 概率：业务逻辑验证失败
		return fmt.Errorf("业务逻辑验证失败 - 消息ID: %d", msg.ID)
	case msg.ID%8 == 0:
		// 8% 概率：网络超时
		return fmt.Errorf("网络超时 - 消息ID: %d", msg.ID)
	}

	// 模拟业务逻辑
	mlog.Debug("执行业务逻辑: message_id=%d, content=%s", msg.ID, msg.Content)

	return nil
}

// GetStats 获取处理统计信息
func (mp *MessageProcessorWithFailedHandler) GetStats() map[string]interface{} {
	return map[string]interface{}{
		"processed_count": mp.processedCount,
		"error_count":     mp.errorCount,
		"failed_count":    mp.failedCount,
	}
}

func main() {
	// 加载配置
	configPath := "config.yaml"
	if len(os.Args) > 1 {
		configPath = os.Args[1]
	}

	cfg, err := nsq_client.LoadConfig(configPath)
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
	mlog.InitialZap("nsq-consumer-failed-handler-example", 1, cfg.Logging.Level, zapConfig)

	mlog.Info("启动带失败消息处理的 NSQ 消费者示例程序")
	mlog.Info("配置加载成功: config_path=%s", configPath)

	// 创建生产者用于发送失败消息
	prod, err := nsq_client.NewProducer(cfg, nil)
	if err != nil {
		mlog.Error("创建生产者失败: %v", err)
		panic(err)
	}
	defer prod.Stop()

	// 创建消息处理器
	processor := NewMessageProcessorWithFailedHandler()

	// 获取主题和频道配置
	topic := cfg.Consumer.DefaultTopic
	channel := cfg.Consumer.DefaultChannel
	if len(os.Args) > 2 {
		topic = os.Args[2]
	}
	if len(os.Args) > 3 {
		channel = os.Args[3]
	}

	mlog.Info("消费者配置: topic=%s, channel=%s, failed_message_enabled=%v",
		topic, channel, cfg.Consumer.FailedMessage.Enabled)

	// 创建带失败消息处理器的消费者
	cons, err := nsq_client.NewConsumerWithFailedMessageHandler(cfg, topic, channel, processor.ProcessMessage, prod)
	if err != nil {
		mlog.Error("创建消费者失败: %v", err)
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
		mlog.Error("启动消费者失败: %v", err)
		panic(err)
	}
	mlog.Info("消费者启动成功，开始监听消息...")

	// 启动统计信息打印协程
	go func() {
		printConsumerStatsWithFailedHandler(ctx, cons, processor)
	}()

	// 等待退出信号
	<-sigChan
	mlog.Info("接收到退出信号，正在关闭消费者...")
	cancel()

	// 等待一段时间让消息处理完成
	time.Sleep(2 * time.Second)
	mlog.Info("带失败消息处理的消费者示例程序已退出")
}

// printConsumerStatsWithFailedHandler 打印统计信息（包含失败消息处理统计）
func printConsumerStatsWithFailedHandler(ctx context.Context, cons *nsq_client.Consumer, processor *MessageProcessorWithFailedHandler) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// 打印消费者统计信息（包含失败消息处理统计）
			consumerStats := cons.GetStats()
			mlog.Info("消费者统计信息: stats=%v", consumerStats)

			// 打印处理器统计信息
			processorStats := processor.GetStats()
			mlog.Info("处理器统计信息: stats=%v", processorStats)

			// 检查连接状态
			if !cons.IsConnected() {
				mlog.Warn("消费者连接已断开")
			}
		}
	}
}

// 环境变量配置示例
func init() {
	// 可以通过环境变量覆盖配置
	// export NSQ_NSQD_ADDRESSES="172.16.1.40:4150"
	// export NSQ_CONSUMER_TOPIC="my_topic"
	// export NSQ_CONSUMER_CHANNEL="my_channel"
	// export LOG_LEVEL="debug"
	// export NSQ_CONSUMER_FAILED_MESSAGE_ENABLED="true"

	// 从命令行参数获取配置
	if len(os.Args) > 2 {
		if topic := os.Args[2]; topic != "" {
			os.Setenv("NSQ_CONSUMER_TOPIC", topic)
		}
	}

	if len(os.Args) > 3 {
		if channel := os.Args[3]; channel != "" {
			os.Setenv("NSQ_CONSUMER_CHANNEL", channel)
		}
	}
}
