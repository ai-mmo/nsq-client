package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/nsqio/go-nsq"
	"mlog"
	"nsq-client/client/consumer"
	"nsq-client/client/producer"
	"nsq-client/config"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// DynamicMessage 动态消息结构
type DynamicMessage struct {
	ID        int                    `json:"id"`
	Type      string                 `json:"type"`
	Content   map[string]interface{} `json:"content"`
	Timestamp time.Time              `json:"timestamp"`
}

// DynamicProcessor 动态消息处理器
type DynamicProcessor struct {
	processedCount int64
	typeStats      map[string]int64
}

// NewDynamicProcessor 创建动态消息处理器
func NewDynamicProcessor() *DynamicProcessor {
	return &DynamicProcessor{
		typeStats: make(map[string]int64),
	}
}

// ProcessDynamicMessage 处理动态消息
func (dp *DynamicProcessor) ProcessDynamicMessage(message *nsq.Message) error {
	// 解析动态消息
	var dynMsg DynamicMessage
	if err := json.Unmarshal(message.Body, &dynMsg); err != nil {
		mlog.Error("解析动态消息失败: message_body=%s, error=%v", string(message.Body), err)
		return fmt.Errorf("解析动态消息失败: %w", err)
	}

	// 记录消息处理
	mlog.Info("处理动态消息: id=%d, type=%s, content=%v",
		dynMsg.ID, dynMsg.Type, dynMsg.Content)

	// 根据消息类型进行处理
	switch dynMsg.Type {
	case "user_event":
		dp.processUserEvent(&dynMsg)
	case "system_alert":
		dp.processSystemAlert(&dynMsg)
	case "data_sync":
		dp.processDataSync(&dynMsg)
	default:
		mlog.Warn("未知消息类型: type=%s", dynMsg.Type)
	}

	// 更新统计信息
	dp.processedCount++
	dp.typeStats[dynMsg.Type]++

	mlog.Debug("动态消息处理完成: id=%d, processed_count=%d", dynMsg.ID, dp.processedCount)

	return nil
}

// processUserEvent 处理用户事件
func (dp *DynamicProcessor) processUserEvent(msg *DynamicMessage) {
	mlog.Info("处理用户事件: user_id=%v, event=%v",
		msg.Content["user_id"], msg.Content["event"])
	time.Sleep(100 * time.Millisecond)
}

// processSystemAlert 处理系统告警
func (dp *DynamicProcessor) processSystemAlert(msg *DynamicMessage) {
	mlog.Warn("处理系统告警: level=%v, message=%v",
		msg.Content["level"], msg.Content["message"])
	time.Sleep(50 * time.Millisecond)
}

// processDataSync 处理数据同步
func (dp *DynamicProcessor) processDataSync(msg *DynamicMessage) {
	mlog.Debug("处理数据同步: table=%v, operation=%v",
		msg.Content["table"], msg.Content["operation"])
	time.Sleep(200 * time.Millisecond)
}

// GetStats 获取处理统计信息
func (dp *DynamicProcessor) GetStats() map[string]interface{} {
	return map[string]interface{}{
		"processed_count": dp.processedCount,
		"type_stats":      dp.typeStats,
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
		Level:        cfg.Logging.Level,
		Prefix:       cfg.Logging.Prefix,
		Format:       cfg.Logging.Format,
		Director:     cfg.Logging.Director,
		ShowLine:     cfg.Logging.ShowLine,
		LogInConsole: cfg.Logging.LogInConsole,
	}
	mlog.InitialZap("dynamic-example", 1, cfg.Logging.Level, zapConfig)

	mlog.Info("启动动态消息示例程序")
	mlog.Info("配置加载成功: config_path=%s", configPath)

	// 创建生产者
	prod, err := producer.NewProducer(cfg)
	if err != nil {
		mlog.Error("创建生产者失败: %v", err)
		panic(err)
	}
	defer prod.Stop()

	// 创建动态消息处理器
	processor := NewDynamicProcessor()

	// 创建消费者
	topic := "dynamic_messages"
	channel := "dynamic_processor"
	cons, err := consumer.NewConsumer(cfg, topic, channel, processor.ProcessDynamicMessage)
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
	mlog.Info("动态消费者启动成功，开始监听消息...")

	// 启动消息生成协程（用于演示）
	go func() {
		generateDynamicMessages(ctx, prod, topic)
	}()

	// 启动统计信息打印协程
	go func() {
		printDynamicStats(ctx, processor)
	}()

	// 等待退出信号
	<-sigChan
	mlog.Info("接收到退出信号，正在关闭动态示例程序...")
	cancel()

	// 等待一段时间让消息处理完成
	time.Sleep(2 * time.Second)
	mlog.Info("动态示例程序已退出")
}

// generateDynamicMessages 生成动态消息（用于演示）
func generateDynamicMessages(ctx context.Context, prod *producer.Producer, topic string) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	messageID := 1
	messageTypes := []string{"user_event", "system_alert", "data_sync"}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for _, msgType := range messageTypes {
				var content map[string]interface{}

				switch msgType {
				case "user_event":
					content = map[string]interface{}{
						"user_id": fmt.Sprintf("user_%d", messageID%10),
						"event":   "login",
					}
				case "system_alert":
					content = map[string]interface{}{
						"level":   "warning",
						"message": fmt.Sprintf("系统告警_%d", messageID),
					}
				case "data_sync":
					content = map[string]interface{}{
						"table":     "users",
						"operation": "update",
					}
				}

				message := DynamicMessage{
					ID:        messageID,
					Type:      msgType,
					Content:   content,
					Timestamp: time.Now(),
				}

				messageBytes, err := json.Marshal(message)
				if err != nil {
					mlog.Error("序列化动态消息失败: %v", err)
					continue
				}

				if err := prod.Publish(topic, messageBytes); err != nil {
					mlog.Error("发送动态消息失败: %v", err)
				} else {
					mlog.Debug("生成动态消息: id=%d, type=%s", message.ID, message.Type)
				}

				messageID++
			}
		}
	}
}

// printDynamicStats 打印动态统计信息
func printDynamicStats(ctx context.Context, processor *DynamicProcessor) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			stats := processor.GetStats()
			mlog.Info("动态处理统计: stats=%v", stats)
		}
	}
}
