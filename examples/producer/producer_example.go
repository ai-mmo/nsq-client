package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	nsq_client "github.com/ai-mmo/nsq-client"

	"mlog"
)

// ExampleMessage 示例消息结构
type ExampleMessage struct {
	ID        int       `json:"id"`
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
	Source    string    `json:"source"`
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
	mlog.InitialZap("nsq-producer-example", 1, cfg.Logging.Level, zapConfig)

	mlog.Info("启动 NSQ 生产者示例程序")
	mlog.Info("配置加载成功: config_path=%s", configPath)

	// 创建生产者
	prod, err := nsq_client.NewProducer(cfg)
	if err != nil {
		mlog.Error("创建生产者失败: %v", err)
		panic(err)
	}
	defer prod.Stop()

	// 测试连接
	if err := prod.Ping(); err != nil {
		mlog.Error("连接 NSQ 服务器失败: %v", err)
		panic(err)
	}
	mlog.Info("成功连接到 NSQ 服务器")

	// 设置信号处理
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// 创建上下文
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 启动消息发送协程
	go func() {
		if err := sendMessages(ctx, prod, cfg); err != nil {
			mlog.Error("发送消息失败: %v", err)
		}
	}()

	// 启动统计信息打印协程
	go func() {
		printStats(ctx, prod)
	}()

	// 等待退出信号
	<-sigChan
	mlog.Info("接收到退出信号，正在关闭生产者...")
	cancel()

	// 等待一段时间让消息发送完成
	time.Sleep(2 * time.Second)
	mlog.Info("生产者示例程序已退出")
}

// sendMessages 发送消息
func sendMessages(ctx context.Context, prod *nsq_client.Producer, cfg *nsq_client.Config) error {
	topic := cfg.Producer.DefaultTopic
	messageCount := 0

	// 发送单条消息示例
	mlog.Info("开始发送单条消息示例...")
	for i := 0; i < 5; i++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		message := ExampleMessage{
			ID:        messageCount + 1,
			Content:   fmt.Sprintf("这是第 %d 条单条消息", i+1),
			Timestamp: time.Now(),
			Source:    "producer_example",
		}

		messageBytes, err := json.Marshal(message)
		if err != nil {
			mlog.Error("序列化消息失败: %v", err)
			continue
		}

		// 发送消息
		if err := prod.Publish(topic, messageBytes); err != nil {
			mlog.Error("发送消息失败: %v", err)
			continue
		}

		messageCount++
		mlog.Info("发送消息成功: id=%d", message.ID)
		time.Sleep(100 * time.Millisecond)
	}

	// 批量发送消息示例
	mlog.Info("开始发送批量消息示例...")
	var batchMessages [][]byte
	for i := 0; i < 10; i++ {
		message := ExampleMessage{
			ID:        messageCount + i + 1,
			Content:   fmt.Sprintf("这是批量消息中的第 %d 条", i+1),
			Timestamp: time.Now(),
			Source:    "producer_example_batch",
		}

		messageBytes, err := json.Marshal(message)
		if err != nil {
			mlog.Error("序列化批量消息失败: %v", err)
			continue
		}

		batchMessages = append(batchMessages, messageBytes)
	}

	if len(batchMessages) > 0 {
		if err := prod.MultiPublish(topic, batchMessages); err != nil {
			mlog.Error("批量发送消息失败: %v", err)
		} else {
			messageCount += len(batchMessages)
			mlog.Info("批量消息发送成功: batch_size=%d", len(batchMessages))
		}
	}

	// 持续发送消息
	mlog.Info("开始持续发送消息...")
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			mlog.Info("停止发送消息")
			return ctx.Err()
		case <-ticker.C:
			// 发送心跳消息
			message := ExampleMessage{
				ID:        messageCount + 1,
				Content:   "心跳消息 - " + time.Now().Format("15:04:05"),
				Timestamp: time.Now(),
				Source:    "producer_example_heartbeat",
			}

			messageBytes, err := json.Marshal(message)
			if err != nil {
				mlog.Error("序列化心跳消息失败: %v", err)
				continue
			}

			if err := prod.Publish(topic, messageBytes); err != nil {
				mlog.Error("发送心跳消息失败: %v", err)
			} else {
				messageCount++
				mlog.Debug("心跳消息发送成功: message_id=%d", message.ID)
			}
		}
	}
}

// printStats 打印统计信息
func printStats(ctx context.Context, prod *nsq_client.Producer) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			stats := prod.GetStats()
			mlog.Info("生产者统计信息: stats=%v", stats)

			// 检查连接状态
			if !prod.IsConnected() {
				mlog.Warn("生产者连接已断开")
			}
		}
	}
}

// 环境变量配置示例
func init() {
	// 可以通过环境变量覆盖配置
	// export NSQ_NSQD_ADDRESSES="172.16.1.40:4150"
	// export NSQ_PRODUCER_TOPIC="my_topic"
	// export LOG_LEVEL="debug"

	// 从命令行参数获取配置
	if len(os.Args) > 2 {
		if topic := os.Args[2]; topic != "" {
			os.Setenv("NSQ_PRODUCER_TOPIC", topic)
		}
	}

	if len(os.Args) > 3 {
		if count := os.Args[3]; count != "" {
			if _, err := strconv.Atoi(count); err == nil {
				// 可以用于控制发送消息数量
			}
		}
	}
}
