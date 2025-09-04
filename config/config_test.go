package config

import (
	"os"
	"testing"
	"time"
)

func TestLoadConfig(t *testing.T) {
	// 创建临时配置文件
	configContent := `
nsq:
  nsqd_addresses:
    - "localhost:4150"
  nsqlookupd_addresses:
    - "localhost:4161"
  connection:
    dial_timeout: 30
    read_timeout: 60
    write_timeout: 30
    heartbeat_interval: 30
    max_attempts: 5
    reconnect_delay: 15

producer:
  default_topic: "test_topic"
  publish_timeout: 5000
  max_in_flight: 200

consumer:
  default_topic: "test_topic"
  default_channel: "test_channel"
  max_in_flight: 200
  msg_timeout: 60
  max_attempts: 5
  requeue_delay: 90000

logging:
  level: "info"
  format: "text"
  output: "stdout"
`

	// 创建临时文件
	tmpFile, err := os.CreateTemp("", "config_test_*.yaml")
	if err != nil {
		t.Fatalf("创建临时文件失败: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	// 写入配置内容
	if _, err := tmpFile.WriteString(configContent); err != nil {
		t.Fatalf("写入配置文件失败: %v", err)
	}
	tmpFile.Close()

	// 测试加载配置
	cfg, err := LoadConfig(tmpFile.Name())
	if err != nil {
		t.Fatalf("加载配置失败: %v", err)
	}

	// 验证配置内容
	if len(cfg.NSQ.NSQDAddresses) != 1 || cfg.NSQ.NSQDAddresses[0] != "localhost:4150" {
		t.Errorf("NSQD 地址配置错误: %v", cfg.NSQ.NSQDAddresses)
	}

	if len(cfg.NSQ.NSQLookupDAddresses) != 1 || cfg.NSQ.NSQLookupDAddresses[0] != "localhost:4161" {
		t.Errorf("NSQLookupD 地址配置错误: %v", cfg.NSQ.NSQLookupDAddresses)
	}

	if cfg.Producer.DefaultTopic != "test_topic" {
		t.Errorf("生产者默认主题配置错误: %s", cfg.Producer.DefaultTopic)
	}

	if cfg.Consumer.DefaultChannel != "test_channel" {
		t.Errorf("消费者默认频道配置错误: %s", cfg.Consumer.DefaultChannel)
	}
}

func TestConfigWithEnvOverride(t *testing.T) {
	// 设置环境变量
	os.Setenv("NSQ_NSQD_ADDRESSES", "env-host:4150")
	os.Setenv("NSQ_PRODUCER_TOPIC", "env_topic")
	os.Setenv("LOG_LEVEL", "debug")
	defer func() {
		os.Unsetenv("NSQ_NSQD_ADDRESSES")
		os.Unsetenv("NSQ_PRODUCER_TOPIC")
		os.Unsetenv("LOG_LEVEL")
	}()

	// 创建基础配置文件
	configContent := `
nsq:
  nsqd_addresses:
    - "localhost:4150"
  nsqlookupd_addresses:
    - "localhost:4161"

producer:
  default_topic: "test_topic"

logging:
  level: "info"
`

	tmpFile, err := os.CreateTemp("", "config_env_test_*.yaml")
	if err != nil {
		t.Fatalf("创建临时文件失败: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(configContent); err != nil {
		t.Fatalf("写入配置文件失败: %v", err)
	}
	tmpFile.Close()

	// 加载配置
	cfg, err := LoadConfig(tmpFile.Name())
	if err != nil {
		t.Fatalf("加载配置失败: %v", err)
	}

	// 验证环境变量覆盖
	if len(cfg.NSQ.NSQDAddresses) != 1 || cfg.NSQ.NSQDAddresses[0] != "env-host:4150" {
		t.Errorf("环境变量覆盖 NSQD 地址失败: %v", cfg.NSQ.NSQDAddresses)
	}

	if cfg.Producer.DefaultTopic != "env_topic" {
		t.Errorf("环境变量覆盖生产者主题失败: %s", cfg.Producer.DefaultTopic)
	}

	if cfg.Logging.Level != "debug" {
		t.Errorf("环境变量覆盖日志级别失败: %s", cfg.Logging.Level)
	}
}

func TestConfigTimeoutMethods(t *testing.T) {
	cfg := &Config{
		NSQ: NSQConfig{
			Connection: ConnectionConfig{
				DialTimeout:       30,
				ReadTimeout:       60,
				WriteTimeout:      30,
				HeartbeatInterval: 30,
			},
		},
		Producer: ProducerConfig{
			PublishTimeout: 5000,
		},
		Consumer: ConsumerConfig{
			MsgTimeout:   60,
			RequeueDelay: 90000,
		},
	}

	// 测试超时方法
	if cfg.GetDialTimeout() != 30*time.Second {
		t.Errorf("GetDialTimeout() 返回值错误: %v", cfg.GetDialTimeout())
	}

	if cfg.GetReadTimeout() != 60*time.Second {
		t.Errorf("GetReadTimeout() 返回值错误: %v", cfg.GetReadTimeout())
	}

	if cfg.GetWriteTimeout() != 30*time.Second {
		t.Errorf("GetWriteTimeout() 返回值错误: %v", cfg.GetWriteTimeout())
	}

	if cfg.GetHeartbeatInterval() != 30*time.Second {
		t.Errorf("GetHeartbeatInterval() 返回值错误: %v", cfg.GetHeartbeatInterval())
	}

	if cfg.GetPublishTimeout() != 5000*time.Millisecond {
		t.Errorf("GetPublishTimeout() 返回值错误: %v", cfg.GetPublishTimeout())
	}

	if cfg.GetMsgTimeout() != 60*time.Second {
		t.Errorf("GetMsgTimeout() 返回值错误: %v", cfg.GetMsgTimeout())
	}

	if cfg.GetRequeueDelay() != 90000*time.Millisecond {
		t.Errorf("GetRequeueDelay() 返回值错误: %v", cfg.GetRequeueDelay())
	}
}

func TestValidateConfig(t *testing.T) {
	// 测试空配置验证
	cfg := &Config{}
	err := validateConfig(cfg)
	if err == nil {
		t.Error("空配置应该返回验证错误")
	}

	// 测试有效配置
	cfg = &Config{
		NSQ: NSQConfig{
			NSQDAddresses: []string{"localhost:4150"},
		},
	}
	err = validateConfig(cfg)
	if err != nil {
		t.Errorf("有效配置验证失败: %v", err)
	}

	// 验证默认值设置
	if cfg.NSQ.Connection.DialTimeout != 30 {
		t.Errorf("默认连接超时设置错误: %d", cfg.NSQ.Connection.DialTimeout)
	}

	if cfg.Producer.DefaultTopic != "default_topic" {
		t.Errorf("默认生产者主题设置错误: %s", cfg.Producer.DefaultTopic)
	}

	if cfg.Consumer.DefaultChannel != "default_channel" {
		t.Errorf("默认消费者频道设置错误: %s", cfg.Consumer.DefaultChannel)
	}
}
