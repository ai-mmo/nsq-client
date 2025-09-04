package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config 应用程序配置结构
type Config struct {
	NSQ      NSQConfig      `yaml:"nsq"`
	Producer ProducerConfig `yaml:"producer"`
	Consumer ConsumerConfig `yaml:"consumer"`
	Logging  LoggingConfig  `yaml:"logging"`
}

// NSQConfig NSQ 服务器配置
type NSQConfig struct {
	NSQDAddresses       []string         `yaml:"nsqd_addresses"`
	NSQLookupDAddresses []string         `yaml:"nsqlookupd_addresses"`
	Connection          ConnectionConfig `yaml:"connection"`
}

// ConnectionConfig 连接配置
type ConnectionConfig struct {
	DialTimeout       int `yaml:"dial_timeout"`       // 连接超时时间（秒）
	ReadTimeout       int `yaml:"read_timeout"`       // 读取超时时间（秒）
	WriteTimeout      int `yaml:"write_timeout"`      // 写入超时时间（秒）
	HeartbeatInterval int `yaml:"heartbeat_interval"` // 心跳间隔（秒）
	MaxAttempts       int `yaml:"max_attempts"`       // 最大重连尝试次数
	ReconnectDelay    int `yaml:"reconnect_delay"`    // 重连间隔（秒）
}

// ProducerConfig 生产者配置
type ProducerConfig struct {
	DefaultTopic   string `yaml:"default_topic"`   // 默认主题名称
	PublishTimeout int    `yaml:"publish_timeout"` // 消息发送超时时间（毫秒）
	MaxInFlight    int    `yaml:"max_in_flight"`   // 最大内存中的消息数量
}

// ConsumerConfig 消费者配置
type ConsumerConfig struct {
	DefaultTopic   string `yaml:"default_topic"`   // 默认主题名称
	DefaultChannel string `yaml:"default_channel"` // 默认频道名称
	MaxInFlight    int    `yaml:"max_in_flight"`   // 并发处理的消息数量
	MsgTimeout     int    `yaml:"msg_timeout"`     // 消息处理超时时间（秒）
	MaxAttempts    int    `yaml:"max_attempts"`    // 最大重试次数
	RequeueDelay   int    `yaml:"requeue_delay"`   // 重新排队延迟时间（毫秒）
}

// LoggingConfig 日志配置 (mlog)
type LoggingConfig struct {
	Level          string `yaml:"level"`           // 日志级别
	Prefix         string `yaml:"prefix"`          // 日志前缀
	Format         string `yaml:"format"`          // 日志格式
	Director       string `yaml:"director"`        // 日志文件夹
	EncodeLevel    string `yaml:"encode_level"`    // 编码级别
	StacktraceKey  string `yaml:"stacktrace_key"`  // 栈名
	ShowLine       bool   `yaml:"show_line"`       // 显示行号
	LogInConsole   bool   `yaml:"log_in_console"`  // 输出到控制台
	RetentionDay   int    `yaml:"retention_day"`   // 日志保留天数
	MaxSize        int    `yaml:"max_size"`        // 日志文件最大大小（MB）
	MaxBackups     int    `yaml:"max_backups"`     // 日志文件数量
	EnableSplit    bool   `yaml:"enable_split"`    // 启用日志分片
	EnableCompress bool   `yaml:"enable_compress"` // 启用日志压缩
	// 异步日志配置
	EnableAsync     bool `yaml:"enable_async"`       // 启用异步日志
	AsyncBufferSize int  `yaml:"async_buffer_size"`  // 异步日志缓冲区大小
	AsyncDropOnFull bool `yaml:"async_drop_on_full"` // 缓冲区满时是否丢弃日志
	// 路径显示配置
	UseRelativePath bool   `yaml:"use_relative_path"` // 使用相对路径显示
	BuildRootPath   string `yaml:"build_root_path"`   // 编译根目录路径
}

var globalConfig *Config

// LoadConfig 加载配置文件
// 支持通过环境变量覆盖配置项
func LoadConfig(configPath string) (*Config, error) {
	// 读取配置文件
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("读取配置文件失败: %w", err)
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("解析配置文件失败: %w", err)
	}

	// 使用环境变量覆盖配置
	overrideWithEnv(&config)

	// 验证配置
	if err := validateConfig(&config); err != nil {
		return nil, fmt.Errorf("配置验证失败: %w", err)
	}

	globalConfig = &config
	return &config, nil
}

// GetConfig 获取全局配置
func GetConfig() *Config {
	return globalConfig
}

// overrideWithEnv 使用环境变量覆盖配置
func overrideWithEnv(config *Config) {
	// NSQ 服务器地址
	if nsqdAddrs := os.Getenv("NSQ_NSQD_ADDRESSES"); nsqdAddrs != "" {
		config.NSQ.NSQDAddresses = strings.Split(nsqdAddrs, ",")
	}
	if lookupdAddrs := os.Getenv("NSQ_LOOKUPD_ADDRESSES"); lookupdAddrs != "" {
		config.NSQ.NSQLookupDAddresses = strings.Split(lookupdAddrs, ",")
	}

	// 连接配置
	if dialTimeout := os.Getenv("NSQ_DIAL_TIMEOUT"); dialTimeout != "" {
		if val, err := strconv.Atoi(dialTimeout); err == nil {
			config.NSQ.Connection.DialTimeout = val
		}
	}
	if readTimeout := os.Getenv("NSQ_READ_TIMEOUT"); readTimeout != "" {
		if val, err := strconv.Atoi(readTimeout); err == nil {
			config.NSQ.Connection.ReadTimeout = val
		}
	}

	// 生产者配置
	if topic := os.Getenv("NSQ_PRODUCER_TOPIC"); topic != "" {
		config.Producer.DefaultTopic = topic
	}
	if timeout := os.Getenv("NSQ_PRODUCER_TIMEOUT"); timeout != "" {
		if val, err := strconv.Atoi(timeout); err == nil {
			config.Producer.PublishTimeout = val
		}
	}

	// 消费者配置
	if topic := os.Getenv("NSQ_CONSUMER_TOPIC"); topic != "" {
		config.Consumer.DefaultTopic = topic
	}
	if channel := os.Getenv("NSQ_CONSUMER_CHANNEL"); channel != "" {
		config.Consumer.DefaultChannel = channel
	}

	// 日志配置
	if level := os.Getenv("LOG_LEVEL"); level != "" {
		config.Logging.Level = level
	}
	if format := os.Getenv("LOG_FORMAT"); format != "" {
		config.Logging.Format = format
	}
}

// validateConfig 验证配置
func validateConfig(config *Config) error {
	// 验证 NSQ 地址
	if len(config.NSQ.NSQDAddresses) == 0 && len(config.NSQ.NSQLookupDAddresses) == 0 {
		return fmt.Errorf("必须配置至少一个 NSQD 或 NSQLookupD 地址")
	}

	// 验证连接配置
	if config.NSQ.Connection.DialTimeout <= 0 {
		config.NSQ.Connection.DialTimeout = 30
	}
	if config.NSQ.Connection.ReadTimeout <= 0 {
		config.NSQ.Connection.ReadTimeout = 60
	}
	if config.NSQ.Connection.WriteTimeout <= 0 {
		config.NSQ.Connection.WriteTimeout = 30
	}

	// 验证生产者配置
	if config.Producer.DefaultTopic == "" {
		config.Producer.DefaultTopic = "default_topic"
	}
	if config.Producer.PublishTimeout <= 0 {
		config.Producer.PublishTimeout = 5000
	}

	// 验证消费者配置
	if config.Consumer.DefaultTopic == "" {
		config.Consumer.DefaultTopic = "default_topic"
	}
	if config.Consumer.DefaultChannel == "" {
		config.Consumer.DefaultChannel = "default_channel"
	}

	// 验证日志配置
	if config.Logging.Level == "" {
		config.Logging.Level = "info"
	}
	if config.Logging.Format == "" {
		config.Logging.Format = "text"
	}
	if config.Logging.Director == "" {
		config.Logging.Director = "./logs"
	}
	if config.Logging.EncodeLevel == "" {
		config.Logging.EncodeLevel = "LowercaseLevelEncoder"
	}
	if config.Logging.StacktraceKey == "" {
		config.Logging.StacktraceKey = "stacktrace"
	}
	// 设置默认值
	if config.Logging.RetentionDay == 0 {
		config.Logging.RetentionDay = 7
	}
	if config.Logging.MaxSize == 0 {
		config.Logging.MaxSize = 100
	}
	if config.Logging.MaxBackups == 0 {
		config.Logging.MaxBackups = 5
	}
	if config.Logging.AsyncBufferSize == 0 {
		config.Logging.AsyncBufferSize = 10000
	}

	return nil
}

// GetNSQDAddresses 获取 NSQD 地址列表
func (c *Config) GetNSQDAddresses() []string {
	return c.NSQ.NSQDAddresses
}

// GetNSQLookupDAddresses 获取 NSQLookupD 地址列表
func (c *Config) GetNSQLookupDAddresses() []string {
	return c.NSQ.NSQLookupDAddresses
}

// GetDialTimeout 获取连接超时时间
func (c *Config) GetDialTimeout() time.Duration {
	return time.Duration(c.NSQ.Connection.DialTimeout) * time.Second
}

// GetReadTimeout 获取读取超时时间
func (c *Config) GetReadTimeout() time.Duration {
	return time.Duration(c.NSQ.Connection.ReadTimeout) * time.Second
}

// GetWriteTimeout 获取写入超时时间
func (c *Config) GetWriteTimeout() time.Duration {
	return time.Duration(c.NSQ.Connection.WriteTimeout) * time.Second
}

// GetHeartbeatInterval 获取心跳间隔
func (c *Config) GetHeartbeatInterval() time.Duration {
	return time.Duration(c.NSQ.Connection.HeartbeatInterval) * time.Second
}

// GetPublishTimeout 获取发布超时时间
func (c *Config) GetPublishTimeout() time.Duration {
	return time.Duration(c.Producer.PublishTimeout) * time.Millisecond
}

// GetMsgTimeout 获取消息处理超时时间
func (c *Config) GetMsgTimeout() time.Duration {
	return time.Duration(c.Consumer.MsgTimeout) * time.Second
}

// GetRequeueDelay 获取重新排队延迟时间
func (c *Config) GetRequeueDelay() time.Duration {
	return time.Duration(c.Consumer.RequeueDelay) * time.Millisecond
}
