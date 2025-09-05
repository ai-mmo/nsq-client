package nsq_client

import (
	"context"
	"fmt"
	"mlog"
	"net/http"
	"net/url"
	"runtime"
	"sync"
	"time"

	"github.com/nsqio/go-nsq"
)

// MessageHandler 消息处理函数类型
type MessageHandler func(message *nsq.Message) error

// Consumer NSQ 消息消费者
type Consumer struct {
	consumer         *nsq.Consumer
	config           *Config
	handler          MessageHandler
	topic            string
	channel          string
	mu               sync.RWMutex
	closed           bool
	wg               sync.WaitGroup
	failedMsgHandler *FailedMessageHandler // 失败消息处理器
}

// NewConsumer 创建新的消费者实例
func NewConsumer(cfg *Config, topic, channel string, handler MessageHandler) (*Consumer, error) {
	return NewConsumerWithFailedMessageHandler(cfg, topic, channel, handler, nil)
}

// NewConsumerWithFailedMessageHandler 创建带失败消息处理器的消费者实例
func NewConsumerWithFailedMessageHandler(cfg *Config, topic, channel string, handler MessageHandler, producer MessageProducer) (*Consumer, error) {
	if cfg == nil {
		return nil, fmt.Errorf("配置不能为空")
	}
	if handler == nil {
		return nil, fmt.Errorf("消息处理函数不能为空")
	}

	// 初始化 mlog（如果还没有初始化）
	if mlog.GLOG() == nil {
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
		mlog.InitialZap("nsq-consumer", 1, cfg.Logging.Level, zapConfig)
	}

	// 使用默认值
	if topic == "" {
		topic = cfg.Consumer.DefaultTopic
	}
	if channel == "" {
		channel = cfg.Consumer.DefaultChannel
	}

	// 创建 NSQ 配置
	nsqConfig := nsq.NewConfig()

	// 设置连接参数
	nsqConfig.DialTimeout = cfg.GetDialTimeout()
	nsqConfig.ReadTimeout = cfg.GetReadTimeout()
	nsqConfig.WriteTimeout = cfg.GetWriteTimeout()
	nsqConfig.HeartbeatInterval = cfg.GetHeartbeatInterval()
	nsqConfig.MaxAttempts = uint16(cfg.Consumer.MaxAttempts)

	// 设置消费者特定参数
	nsqConfig.MaxInFlight = cfg.Consumer.MaxInFlight
	nsqConfig.MsgTimeout = cfg.GetMsgTimeout()
	nsqConfig.MaxRequeueDelay = cfg.GetRequeueDelay()

	// 创建 NSQ 消费者
	nsqConsumer, err := nsq.NewConsumer(topic, channel, nsqConfig)
	if err != nil {
		return nil, fmt.Errorf("创建 NSQ 消费者失败: %w", err)
	}

	consumer := &Consumer{
		consumer: nsqConsumer,
		config:   cfg,
		handler:  handler,
		topic:    topic,
		channel:  channel,
		closed:   false,
	}

	// 如果提供了生产者，创建失败消息处理器
	if producer != nil {
		consumer.failedMsgHandler = NewFailedMessageHandler(cfg, producer)
		mlog.Info("失败消息处理器已启用: topic=%s, channel=%s", topic, channel)
	} else if cfg.Consumer.FailedMessage.Enabled {
		mlog.Warn("失败消息处理已启用但未提供生产者，将跳过失败消息处理: topic=%s, channel=%s", topic, channel)
	}

	// 设置消息处理器
	nsqConsumer.AddHandler(consumer)

	mlog.Info("NSQ 消费者初始化成功: topic=%s, channel=%s", topic, channel)

	return consumer, nil
}

// HandleMessage 实现 nsq.Handler 接口
func (c *Consumer) HandleMessage(message *nsq.Message) error {
	// 防护措施：捕获panic，避免整个程序崩溃
	defer func() {
		if r := recover(); r != nil {
			mlog.Error("消息处理发生panic，已恢复: message_id=%s, panic=%v",
				string(message.ID[:]), r)
			// 记录堆栈信息
			buf := make([]byte, 4096)
			n := runtime.Stack(buf, false)
			mlog.Error("panic堆栈信息:\n%s", string(buf[:n]))
			// panic情况下也要完成消息，避免无限重试
			message.Finish()
		}
	}()

	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.closed {
		return fmt.Errorf("消费者已关闭")
	}

	// 安全检查：确保message不为nil
	if message == nil {
		mlog.Error("接收到空消息，跳过处理")
		return fmt.Errorf("消息为空")
	}

	// 记录消息接收日志
	mlog.Debug("接收到消息: message_id=%s, message_len=%d, attempts=%d, timestamp=%d",
		string(message.ID[:]), len(message.Body), message.Attempts, message.Timestamp)

	// 处理消息
	startTime := time.Now()
	err := c.handler(message)
	processingTime := time.Since(startTime)

	if err != nil {
		// 处理失败，记录错误日志
		mlog.Error("消息处理失败: message_id=%s, message_len=%d, attempts=%d, processing_time=%v, error=%v",
			string(message.ID[:]), len(message.Body), message.Attempts, processingTime, err)

		// 根据重试次数决定是否重新排队
		if message.Attempts < uint16(c.config.Consumer.MaxAttempts) {
			mlog.Warn("消息处理失败，将重新排队: message_id=%s, attempts=%d, max_attempts=%d",
				string(message.ID[:]), message.Attempts, c.config.Consumer.MaxAttempts)

			// 延迟重新排队
			message.RequeueWithoutBackoff(c.config.GetRequeueDelay())
			return nil
		} else {
			mlog.Error("消息处理失败次数超过最大重试次数: message_id=%s, attempts=%d",
				string(message.ID[:]), message.Attempts)

			// 处理失败消息（发送到失败队列）
			if c.failedMsgHandler != nil {
				if failedErr := c.failedMsgHandler.HandleFailedMessage(c.topic, c.channel, message, err); failedErr != nil {
					mlog.Error("处理失败消息时发生错误: message_id=%s, error=%v", string(message.ID[:]), failedErr)
					// 即使失败消息处理失败，也要完成原消息以避免无限循环
				} else {
					mlog.Info("失败消息已处理: message_id=%s", string(message.ID[:]))
				}
			} else {
				mlog.Warn("未配置失败消息处理器，消息将被丢弃: message_id=%s", string(message.ID[:]))
			}

			// 完成消息（避免重复处理）
			message.Finish()
			return nil
		}
	}

	// 处理成功，记录日志并完成消息
	mlog.Info("消息处理成功: message_id=%s, message_len=%d, processing_time=%v",
		string(message.ID[:]), len(message.Body), processingTime)

	message.Finish()
	return nil
}

// Start 启动消费者（动态创建主题和频道）
func (c *Consumer) Start() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return fmt.Errorf("消费者已关闭")
	}

	// 动态创建主题和频道
	if err := c.ensureTopicAndChannelExist(); err != nil {
		mlog.Error("确保主题和频道存在失败: topic=%s, channel=%s, error=%v", c.topic, c.channel, err)
		return fmt.Errorf("确保主题和频道存在失败: %w", err)
	}

	// 获取连接地址
	nsqdAddresses := c.config.GetNSQDAddresses()
	lookupdAddresses := c.config.GetNSQLookupDAddresses()

	if len(nsqdAddresses) == 0 && len(lookupdAddresses) == 0 {
		return fmt.Errorf("未配置 NSQD 或 NSQLookupD 地址")
	}

	var err error

	// 优先使用 NSQLookupD 进行服务发现
	if len(lookupdAddresses) > 0 {
		mlog.Info("通过 NSQLookupD 启动消费者: lookupd_addresses=%v", lookupdAddresses)

		err = c.consumer.ConnectToNSQLookupds(lookupdAddresses)
		if err != nil {
			mlog.Error("连接到 NSQLookupD 失败: %v", err)
			return fmt.Errorf("连接到 NSQLookupD 失败: %w", err)
		}
	} else {
		// 直接连接到 NSQD
		mlog.Info("直接连接到 NSQD 启动消费者: nsqd_addresses=%v", nsqdAddresses)

		err = c.consumer.ConnectToNSQDs(nsqdAddresses)
		if err != nil {
			mlog.Error("连接到 NSQD 失败: %v", err)
			return fmt.Errorf("连接到 NSQD 失败: %w", err)
		}
	}

	mlog.Info("NSQ 消费者启动成功")
	return nil
}

// StartWithContext 带上下文启动消费者
func (c *Consumer) StartWithContext(ctx context.Context) error {
	// 启动消费者
	if err := c.Start(); err != nil {
		return err
	}

	// 启动监控协程
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		c.monitorContext(ctx)
	}()

	return nil
}

// monitorContext 监控上下文取消信号
func (c *Consumer) monitorContext(ctx context.Context) {
	<-ctx.Done()
	mlog.Info("接收到上下文取消信号，正在停止消费者...")
	c.Stop()
}

// Stop 停止消费者
func (c *Consumer) Stop() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		mlog.Debug("消费者已经关闭，跳过停止操作")
		return
	}

	mlog.Info("正在停止 NSQ 消费者...")

	// 设置关闭标志，防止新的消息处理
	c.closed = true

	// 安全停止NSQ消费者
	if c.consumer != nil {
		// 先暂停消费，等待当前消息处理完成
		c.consumer.ChangeMaxInFlight(0)
		time.Sleep(100 * time.Millisecond) // 给当前处理中的消息一些时间完成

		// 停止消费者
		c.consumer.Stop()
	} else {
		mlog.Warn("NSQ消费者实例为空，跳过停止操作")
	}

	// 释放锁后等待协程结束，避免死锁
	c.mu.Unlock()

	// 等待所有协程结束，设置超时防止无限等待
	done := make(chan struct{})
	go func() {
		c.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		mlog.Info("所有协程已正常结束")
	case <-time.After(30 * time.Second):
		mlog.Warn("等待协程结束超时，强制退出")
	}

	// 重新获取锁
	c.mu.Lock()

	mlog.Info("NSQ 消费者已停止")
}

// IsConnected 检查连接状态
func (c *Consumer) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.closed {
		return false
	}

	stats := c.consumer.Stats()
	return stats.Connections > 0
}

// GetStats 获取消费者统计信息
func (c *Consumer) GetStats() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.closed {
		return map[string]interface{}{
			"status": "closed",
		}
	}

	stats := c.consumer.Stats()
	result := map[string]interface{}{
		"status":           "running",
		"connection_count": stats.Connections,
		"message_count":    stats.MessagesReceived,
		"message_finished": stats.MessagesFinished,
		"message_requeued": stats.MessagesRequeued,
		"is_connected":     c.IsConnected(),
	}

	// 添加失败消息处理统计信息
	if c.failedMsgHandler != nil {
		result["failed_message_handler"] = c.failedMsgHandler.GetFailedMessageStats()
	}

	return result
}

// ChangeMaxInFlight 动态调整最大并发处理数
func (c *Consumer) ChangeMaxInFlight(maxInFlight int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		mlog.Warn("消费者已关闭，无法调整并发数")
		return
	}

	// 参数验证
	if maxInFlight < 0 {
		mlog.Error("无效的并发数: %d，必须大于等于0", maxInFlight)
		return
	}

	// 限制最大并发数，防止资源耗尽
	const maxAllowedInFlight = 1000
	if maxInFlight > maxAllowedInFlight {
		mlog.Warn("并发数%d超过最大限制%d，将调整为最大限制", maxInFlight, maxAllowedInFlight)
		maxInFlight = maxAllowedInFlight
	}

	// 安全检查：确保consumer不为nil
	if c.consumer == nil {
		mlog.Error("NSQ消费者实例为空，无法调整并发数")
		return
	}

	c.consumer.ChangeMaxInFlight(maxInFlight)
	mlog.Info("调整最大并发处理数: max_in_flight=%d", maxInFlight)
}

// Pause 暂停消费者
func (c *Consumer) Pause() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		mlog.Warn("消费者已关闭，无法暂停")
		return
	}

	// 安全检查：确保consumer不为nil
	if c.consumer == nil {
		mlog.Error("NSQ消费者实例为空，无法暂停")
		return
	}

	c.consumer.ChangeMaxInFlight(0)
	mlog.Info("消费者已暂停")
}

// Resume 恢复消费者
func (c *Consumer) Resume() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		mlog.Warn("消费者已关闭，无法恢复")
		return
	}

	// 安全检查：确保consumer和config不为nil
	if c.consumer == nil {
		mlog.Error("NSQ消费者实例为空，无法恢复")
		return
	}

	if c.config == nil {
		mlog.Error("消费者配置为空，无法恢复")
		return
	}

	c.consumer.ChangeMaxInFlight(c.config.Consumer.MaxInFlight)
	mlog.Info("消费者已恢复: max_in_flight=%d", c.config.Consumer.MaxInFlight)
}

// ensureTopicAndChannelExist 确保主题和频道存在，如果不存在则动态创建
func (c *Consumer) ensureTopicAndChannelExist() error {
	// 获取 NSQD 地址列表
	nsqdAddresses := c.config.GetNSQDAddresses()
	if len(nsqdAddresses) == 0 {
		return fmt.Errorf("未配置 NSQD 地址")
	}

	// 尝试在所有 NSQD 上创建主题和频道
	var lastErr error
	successCount := 0

	for _, addr := range nsqdAddresses {
		// 创建主题
		if err := c.createTopicOnNSQD(addr, c.topic); err != nil {
			mlog.Warn("在 NSQD 上创建主题失败: nsqd_addr=%s, topic=%s, error=%v", addr, c.topic, err)
			lastErr = err
			continue
		}

		// 创建频道
		if err := c.createChannelOnNSQD(addr, c.topic, c.channel); err != nil {
			mlog.Warn("在 NSQD 上创建频道失败: nsqd_addr=%s, topic=%s, channel=%s, error=%v",
				addr, c.topic, c.channel, err)
			lastErr = err
			continue
		}

		mlog.Debug("主题和频道创建成功或已存在: nsqd_addr=%s, topic=%s, channel=%s",
			addr, c.topic, c.channel)
		successCount++
	}

	if successCount == 0 {
		return fmt.Errorf("无法在任何 NSQD 上创建主题和频道: %w", lastErr)
	}

	mlog.Info("主题和频道确保存在完成: topic=%s, channel=%s, success_count=%d, total_count=%d",
		c.topic, c.channel, successCount, len(nsqdAddresses))

	return nil
}

// createTopicOnNSQD 在指定的 NSQD 上创建主题
func (c *Consumer) createTopicOnNSQD(nsqdAddr, topic string) error {
	// 构建创建主题的 URL
	createURL := fmt.Sprintf("http://%s/topic/create?topic=%s",
		c.getNSQDHTTPAddr(nsqdAddr), url.QueryEscape(topic))

	// 创建带超时的HTTP客户端，防止长时间阻塞
	client := &http.Client{
		Timeout: 10 * time.Second, // 10秒超时
	}

	// 发送 HTTP POST 请求创建主题
	resp, err := client.Post(createURL, "", nil)
	if err != nil {
		return fmt.Errorf("发送创建主题请求失败: %w", err)
	}
	defer func() {
		if resp != nil && resp.Body != nil {
			resp.Body.Close()
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("创建主题失败，状态码: %d", resp.StatusCode)
	}

	return nil
}

// createChannelOnNSQD 在指定的 NSQD 上创建频道
func (c *Consumer) createChannelOnNSQD(nsqdAddr, topic, channel string) error {
	// 构建创建频道的 URL
	createURL := fmt.Sprintf("http://%s/channel/create?topic=%s&channel=%s",
		c.getNSQDHTTPAddr(nsqdAddr), url.QueryEscape(topic), url.QueryEscape(channel))

	// 创建带超时的HTTP客户端，防止长时间阻塞
	client := &http.Client{
		Timeout: 10 * time.Second, // 10秒超时
	}

	// 发送 HTTP POST 请求创建频道
	resp, err := client.Post(createURL, "", nil)
	if err != nil {
		return fmt.Errorf("发送创建频道请求失败: %w", err)
	}
	defer func() {
		if resp != nil && resp.Body != nil {
			resp.Body.Close()
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("创建频道失败，状态码: %d", resp.StatusCode)
	}

	return nil
}

// getNSQDHTTPAddr 将 TCP 地址转换为 HTTP 地址
func (c *Consumer) getNSQDHTTPAddr(tcpAddr string) string {
	// 将 TCP 端口 4150 转换为 HTTP 端口 4151
	// 例如: "172.16.1.40:4150" -> "172.16.1.40:4151"
	if len(tcpAddr) > 4 && tcpAddr[len(tcpAddr)-4:] == "4150" {
		return tcpAddr[:len(tcpAddr)-4] + "4151"
	}
	return tcpAddr
}

// CreateTopicAndChannel 动态创建主题和频道的公共方法
func (c *Consumer) CreateTopicAndChannel(topic, channel string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return fmt.Errorf("消费者已关闭")
	}

	if topic == "" || channel == "" {
		return fmt.Errorf("主题名称和频道名称不能为空")
	}

	// 临时保存原始值
	originalTopic := c.topic
	originalChannel := c.channel

	// 设置新的主题和频道
	c.topic = topic
	c.channel = channel

	mlog.Info("开始创建主题和频道: topic=%s, channel=%s", topic, channel)

	if err := c.ensureTopicAndChannelExist(); err != nil {
		// 恢复原始值
		c.topic = originalTopic
		c.channel = originalChannel

		mlog.Error("创建主题和频道失败: topic=%s, channel=%s, error=%v", topic, channel, err)
		return fmt.Errorf("创建主题和频道失败: %w", err)
	}

	mlog.Info("主题和频道创建成功: topic=%s, channel=%s", topic, channel)
	return nil
}

// HealthCheck 健康检查
func (c *Consumer) HealthCheck() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	health := map[string]interface{}{
		"status":    "unknown",
		"timestamp": time.Now().Unix(),
		"checks":    make(map[string]interface{}),
	}

	checks := health["checks"].(map[string]interface{})

	// 检查消费者状态
	if c.closed {
		health["status"] = "stopped"
		checks["consumer_status"] = "stopped"
		return health
	}

	checks["consumer_status"] = "running"

	// 检查NSQ连接状态
	if c.consumer != nil {
		stats := c.consumer.Stats()
		checks["connection_count"] = stats.Connections
		checks["message_count"] = stats.MessagesReceived
		checks["message_finished"] = stats.MessagesFinished
		checks["message_requeued"] = stats.MessagesRequeued

		if stats.Connections > 0 {
			checks["nsq_connection"] = "healthy"
		} else {
			checks["nsq_connection"] = "unhealthy"
		}
	} else {
		checks["nsq_connection"] = "not_initialized"
	}

	// 检查失败消息处理器状态
	if c.failedMsgHandler != nil {
		failedStats := c.failedMsgHandler.GetFailedMessageStats()
		checks["failed_message_handler"] = failedStats
	}

	// 综合健康状态
	if checks["consumer_status"] == "running" && checks["nsq_connection"] == "healthy" {
		health["status"] = "healthy"
	} else {
		health["status"] = "unhealthy"
	}

	return health
}

// GetDetailedStats 获取详细统计信息
func (c *Consumer) GetDetailedStats() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	stats := map[string]interface{}{
		"basic_info": map[string]interface{}{
			"topic":     c.topic,
			"channel":   c.channel,
			"closed":    c.closed,
			"timestamp": time.Now().Unix(),
		},
	}

	if c.closed {
		stats["status"] = "closed"
		return stats
	}

	// NSQ统计信息
	if c.consumer != nil {
		nsqStats := c.consumer.Stats()
		stats["nsq_stats"] = map[string]interface{}{
			"connections":       nsqStats.Connections,
			"messages_received": nsqStats.MessagesReceived,
			"messages_finished": nsqStats.MessagesFinished,
			"messages_requeued": nsqStats.MessagesRequeued,
			"is_connected":      nsqStats.Connections > 0,
		}
	}

	// 配置信息
	if c.config != nil {
		stats["config"] = map[string]interface{}{
			"max_in_flight": c.config.Consumer.MaxInFlight,
			"max_attempts":  c.config.Consumer.MaxAttempts,
			"msg_timeout":   c.config.Consumer.MsgTimeout,
			"requeue_delay": c.config.Consumer.RequeueDelay,
		}
	}

	// 失败消息处理器统计
	if c.failedMsgHandler != nil {
		stats["failed_message_handler"] = c.failedMsgHandler.GetFailedMessageStats()
	}

	return stats
}
