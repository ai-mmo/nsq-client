package nsq_client

import (
	"context"
	"fmt"
	"mlog"
	"net/http"
	"net/url"
	"sync"

	"github.com/nsqio/go-nsq"
	"go.uber.org/zap"
)

// Producer NSQ 消息生产者
type Producer struct {
	producer *nsq.Producer
	config   *Config
	mu       sync.RWMutex
	closed   bool
}

// Message 消息结构
type Message struct {
	Topic string      `json:"topic"`
	Body  interface{} `json:"body"`
}

// NewProducer 创建新的生产者实例
func NewProducer(cfg *Config, logger *zap.Logger) (*Producer, error) {
	if cfg == nil {
		return nil, fmt.Errorf("配置不能为空")
	}

	if logger == nil {
		// 初始化 mlog
		zapConfig := &mlog.ZapConfig{
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
		// 初始化 mlog
		mlog.InitialZap("nsq-producer", 1, cfg.Logging.Level, zapConfig)
	}

	// 创建 NSQ 配置
	nsqConfig := nsq.NewConfig()

	// 设置连接参数
	nsqConfig.DialTimeout = cfg.GetDialTimeout()
	nsqConfig.ReadTimeout = cfg.GetReadTimeout()
	nsqConfig.WriteTimeout = cfg.GetWriteTimeout()
	nsqConfig.HeartbeatInterval = cfg.GetHeartbeatInterval()
	nsqConfig.MaxAttempts = uint16(cfg.NSQ.Connection.MaxAttempts)

	// 设置生产者特定参数
	nsqConfig.MaxInFlight = cfg.Producer.MaxInFlight

	// 获取 NSQD 地址
	nsqdAddresses := cfg.GetNSQDAddresses()
	if len(nsqdAddresses) == 0 {
		return nil, fmt.Errorf("未配置 NSQD 地址")
	}

	// 创建 NSQ 生产者（连接到第一个可用的 NSQD）
	var nsqProducer *nsq.Producer
	var err error

	for _, addr := range nsqdAddresses {
		nsqProducer, err = nsq.NewProducer(addr, nsqConfig)
		if err != nil {
			mlog.Warn("连接到 NSQD %s 失败，尝试下一个地址: %v", addr, err)
			continue
		}

		// 测试连接
		if err = nsqProducer.Ping(); err != nil {
			mlog.Warn("NSQD %s 连接测试失败，尝试下一个地址: %v", addr, err)
			nsqProducer.Stop()
			continue
		}

		mlog.Info("成功连接到 NSQD: %s", addr)
		break
	}

	if nsqProducer == nil {
		return nil, fmt.Errorf("无法连接到任何 NSQD 服务器")
	}

	producer := &Producer{
		producer: nsqProducer,
		config:   cfg,
		closed:   false,
	}

	mlog.Info("NSQ 生产者初始化成功")
	return producer, nil
}

// Publish 发布消息到指定主题（动态创建主题）
func (p *Producer) Publish(topic string, message []byte) error {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.closed {
		return fmt.Errorf("生产者已关闭")
	}

	if topic == "" {
		topic = p.config.Producer.DefaultTopic
	}

	// 动态创建主题（如果不存在）
	if err := p.ensureTopicExists(topic); err != nil {
		mlog.Error("确保主题存在失败: topic=%s, error=%v", topic, err)
		return fmt.Errorf("确保主题存在失败: %w", err)
	}

	// 记录发送日志
	mlog.Debug("开始发送消息: topic=%s, message_len=%d", topic, len(message))

	// 发送消息
	err := p.producer.Publish(topic, message)
	if err != nil {
		mlog.Error("消息发送失败: topic=%s, message_len=%d, error=%v", topic, len(message), err)
		return fmt.Errorf("发送消息失败: %w", err)
	}

	mlog.Info("消息发送成功: topic=%s, message_len=%d", topic, len(message))

	return nil
}

// PublishAsync 异步发布消息
func (p *Producer) PublishAsync(topic string, message []byte, doneChan chan *nsq.ProducerTransaction) error {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.closed {
		return fmt.Errorf("生产者已关闭")
	}

	if topic == "" {
		topic = p.config.Producer.DefaultTopic
	}

	// 记录发送日志
	mlog.Debug("开始异步发送消息: topic=%s, message_len=%d", topic, len(message))

	// 异步发送消息
	err := p.producer.PublishAsync(topic, message, doneChan)
	if err != nil {
		mlog.Error("异步消息发送失败: topic=%s, message_len=%d, error=%v", topic, len(message), err)
		return fmt.Errorf("异步发送消息失败: %w", err)
	}

	return nil
}

// PublishWithTimeout 带超时的消息发布
func (p *Producer) PublishWithTimeout(ctx context.Context, topic string, message []byte) error {
	// 创建带超时的上下文
	timeoutCtx, cancel := context.WithTimeout(ctx, p.config.GetPublishTimeout())
	defer cancel()

	// 创建结果通道
	doneChan := make(chan *nsq.ProducerTransaction, 1)
	defer close(doneChan)

	// 异步发送消息
	if err := p.PublishAsync(topic, message, doneChan); err != nil {
		return err
	}

	// 等待结果或超时
	select {
	case transaction := <-doneChan:
		if transaction.Error != nil {
			mlog.Error("消息发送事务失败: topic=%s, message_len=%d, error=%v", topic, len(message), transaction.Error)
			return fmt.Errorf("消息发送事务失败: %w", transaction.Error)
		}

		mlog.Info("消息发送事务成功: topic=%s, message_len=%d", topic, len(message))
		return nil

	case <-timeoutCtx.Done():
		mlog.Error("消息发送超时: topic=%s, message_len=%d, timeout=%v", topic, len(message), p.config.GetPublishTimeout())
		return fmt.Errorf("消息发送超时")
	}
}

// MultiPublish 批量发布消息到指定主题
func (p *Producer) MultiPublish(topic string, messages [][]byte) error {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.closed {
		return fmt.Errorf("生产者已关闭")
	}

	if topic == "" {
		topic = p.config.Producer.DefaultTopic
	}

	if len(messages) == 0 {
		return fmt.Errorf("消息列表不能为空")
	}

	// 记录批量发送日志
	mlog.Debug("开始批量发送消息: topic=%s, message_count=%d", topic, len(messages))

	// 批量发送消息
	err := p.producer.MultiPublish(topic, messages)
	if err != nil {
		mlog.Error("批量消息发送失败: topic=%s, message_count=%d, error=%v", topic, len(messages), err)
		return fmt.Errorf("批量发送消息失败: %w", err)
	}

	mlog.Info("批量消息发送成功: topic=%s, message_count=%d", topic, len(messages))

	return nil
}

// Ping 测试连接
func (p *Producer) Ping() error {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.closed {
		return fmt.Errorf("生产者已关闭")
	}

	err := p.producer.Ping()
	if err != nil {
		mlog.Error("连接测试失败: %v", err)
		return fmt.Errorf("连接测试失败: %w", err)
	}

	mlog.Debug("连接测试成功")
	return nil
}

// Stop 停止生产者
func (p *Producer) Stop() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return
	}

	mlog.Info("正在停止 NSQ 生产者...")

	// 停止生产者
	p.producer.Stop()
	p.closed = true

	mlog.Info("NSQ 生产者已停止")
}

// IsConnected 检查连接状态
func (p *Producer) IsConnected() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.closed {
		return false
	}

	// 通过 Ping 检查连接状态
	return p.producer.Ping() == nil
}

// ensureTopicExists 确保主题存在，如果不存在则动态创建
func (p *Producer) ensureTopicExists(topic string) error {
	// 获取 NSQD 地址列表
	nsqdAddresses := p.config.GetNSQDAddresses()
	if len(nsqdAddresses) == 0 {
		return fmt.Errorf("未配置 NSQD 地址")
	}

	// 尝试向第一个可用的 NSQD 创建主题
	for _, addr := range nsqdAddresses {
		if err := p.createTopicOnNSQD(addr, topic); err != nil {
			mlog.Warn("在 NSQD 上创建主题失败，尝试下一个地址: nsqd_addr=%s, topic=%s, error=%v", addr, topic, err)
			continue
		}

		mlog.Debug("主题创建成功或已存在: nsqd_addr=%s, topic=%s", addr, topic)
		return nil
	}

	return fmt.Errorf("无法在任何 NSQD 上创建主题: %s", topic)
}

// createTopicOnNSQD 在指定的 NSQD 上创建主题
func (p *Producer) createTopicOnNSQD(nsqdAddr, topic string) error {
	// 构建创建主题的 URL
	createURL := fmt.Sprintf("http://%s/topic/create?topic=%s",
		p.getNSQDHTTPAddr(nsqdAddr), url.QueryEscape(topic))

	// 发送 HTTP POST 请求创建主题
	resp, err := http.Post(createURL, "", nil)
	if err != nil {
		return fmt.Errorf("发送创建主题请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("创建主题失败，状态码: %d", resp.StatusCode)
	}

	return nil
}

// getNSQDHTTPAddr 将 TCP 地址转换为 HTTP 地址
func (p *Producer) getNSQDHTTPAddr(tcpAddr string) string {
	// 将 TCP 端口 4150 转换为 HTTP 端口 4151
	// 例如: "172.16.1.40:4150" -> "172.16.1.40:4151"
	if len(tcpAddr) > 4 && tcpAddr[len(tcpAddr)-4:] == "4150" {
		return tcpAddr[:len(tcpAddr)-4] + "4151"
	}
	return tcpAddr
}

// CreateTopic 动态创建主题的公共方法
func (p *Producer) CreateTopic(topic string) error {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.closed {
		return fmt.Errorf("生产者已关闭")
	}

	if topic == "" {
		return fmt.Errorf("主题名称不能为空")
	}

	mlog.Info("开始创建主题: topic=%s", topic)

	if err := p.ensureTopicExists(topic); err != nil {
		mlog.Error("创建主题失败: topic=%s, error=%v", topic, err)
		return fmt.Errorf("创建主题失败: %w", err)
	}

	mlog.Info("主题创建成功: topic=%s", topic)
	return nil
}

// GetStats 获取生产者统计信息
func (p *Producer) GetStats() map[string]interface{} {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.closed {
		return map[string]interface{}{
			"status": "closed",
		}
	}

	// NSQ Producer 没有内置的统计信息，返回基本状态
	return map[string]interface{}{
		"status":       "connected",
		"is_connected": p.IsConnected(),
	}
}
