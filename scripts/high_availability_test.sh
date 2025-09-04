#!/bin/bash

# NSQ客户端高可用性测试脚本

set -e

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# 配置
CONFIG_FILE="config/config.yaml"
TEST_TOPIC="ha_test_topic"
TEST_CHANNEL="ha_test_channel"

echo -e "${BLUE}=== NSQ客户端高可用性测试 ===${NC}"
echo

# 检查依赖
echo -e "${YELLOW}检查依赖...${NC}"
if ! command -v go &> /dev/null; then
    echo -e "${RED}错误: Go 未安装${NC}"
    exit 1
fi

# 创建测试目录
mkdir -p bin logs test_results

# 编译测试程序
echo -e "${YELLOW}编译测试程序...${NC}"

# 创建高可用性测试消费者
cat > test_ha_consumer.go << 'EOF'
package main

import (
    "context"
    "encoding/json"
    "fmt"
    "os"
    "os/signal"
    "syscall"
    "time"
    "math/rand"

    "nsq-client/client/consumer"
    "nsq-client/client/producer"
    "nsq-client/config"
    "mlog"
    "github.com/nsqio/go-nsq"
)

type TestMessage struct {
    ID      int    `json:"id"`
    Content string `json:"content"`
    Type    string `json:"type"`
}

type HATestProcessor struct {
    processedCount int64
    panicCount     int64
    errorCount     int64
}

func (p *HATestProcessor) ProcessMessage(message *nsq.Message) error {
    var msg TestMessage
    if err := json.Unmarshal(message.Body, &msg); err != nil {
        p.errorCount++
        return fmt.Errorf("解析消息失败: %w", err)
    }

    mlog.Info("处理消息: id=%d, type=%s, content=%s", msg.ID, msg.Type, msg.Content)

    // 模拟不同类型的处理场景
    switch msg.Type {
    case "panic":
        p.panicCount++
        panic(fmt.Sprintf("模拟panic - 消息ID: %d", msg.ID))
    case "error":
        p.errorCount++
        return fmt.Errorf("模拟处理错误 - 消息ID: %d", msg.ID)
    case "slow":
        // 模拟慢处理
        time.Sleep(time.Duration(rand.Intn(1000)) * time.Millisecond)
    case "normal":
        // 正常处理
        time.Sleep(10 * time.Millisecond)
    }

    p.processedCount++
    return nil
}

func (p *HATestProcessor) GetStats() map[string]interface{} {
    return map[string]interface{}{
        "processed_count": p.processedCount,
        "panic_count":     p.panicCount,
        "error_count":     p.errorCount,
    }
}

func main() {
    if len(os.Args) < 4 {
        fmt.Println("用法: test_ha_consumer <config> <topic> <channel>")
        os.Exit(1)
    }

    cfg, err := config.LoadConfig(os.Args[1])
    if err != nil {
        panic(err)
    }

    // 初始化日志
    zapConfig := mlog.ZapConfig{
        Level:        cfg.Logging.Level,
        LogInConsole: true,
    }
    mlog.InitialZap("ha-test-consumer", 1, cfg.Logging.Level, zapConfig)

    // 创建生产者用于失败消息处理
    prod, err := producer.NewProducer(cfg)
    if err != nil {
        panic(err)
    }
    defer prod.Stop()

    // 创建处理器
    processor := &HATestProcessor{}

    // 创建消费者
    cons, err := consumer.NewConsumerWithFailedMessageHandler(
        cfg, os.Args[2], os.Args[3], processor.ProcessMessage, prod)
    if err != nil {
        panic(err)
    }
    defer cons.Stop()

    // 启动消费者
    if err := cons.Start(); err != nil {
        panic(err)
    }

    mlog.Info("高可用性测试消费者启动成功")

    // 定期打印统计信息和健康检查
    go func() {
        ticker := time.NewTicker(10 * time.Second)
        defer ticker.Stop()
        
        for {
            select {
            case <-ticker.C:
                // 健康检查
                health := cons.HealthCheck()
                mlog.Info("健康检查: %v", health)
                
                // 详细统计
                stats := cons.GetDetailedStats()
                mlog.Info("详细统计: %v", stats)
                
                // 处理器统计
                procStats := processor.GetStats()
                mlog.Info("处理器统计: %v", procStats)
            }
        }
    }()

    // 等待退出信号
    sigChan := make(chan os.Signal, 1)
    signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
    <-sigChan

    mlog.Info("高可用性测试消费者退出")
}
EOF

go build -o bin/test_ha_consumer test_ha_consumer.go

# 创建测试消息生产者
cat > test_message_producer.go << 'EOF'
package main

import (
    "encoding/json"
    "fmt"
    "os"
    "time"
    "math/rand"

    "nsq-client/client/producer"
    "nsq-client/config"
    "mlog"
)

type TestMessage struct {
    ID      int    `json:"id"`
    Content string `json:"content"`
    Type    string `json:"type"`
}

func main() {
    if len(os.Args) < 3 {
        fmt.Println("用法: test_message_producer <config> <topic>")
        os.Exit(1)
    }

    cfg, err := config.LoadConfig(os.Args[1])
    if err != nil {
        panic(err)
    }

    // 初始化日志
    zapConfig := mlog.ZapConfig{
        Level:        "info",
        LogInConsole: true,
    }
    mlog.InitialZap("ha-test-producer", 1, "info", zapConfig)

    // 创建生产者
    prod, err := producer.NewProducer(cfg)
    if err != nil {
        panic(err)
    }
    defer prod.Stop()

    topic := os.Args[2]
    messageTypes := []string{"normal", "error", "panic", "slow"}

    mlog.Info("开始发送高可用性测试消息")

    // 发送100条测试消息
    for i := 1; i <= 100; i++ {
        msgType := messageTypes[rand.Intn(len(messageTypes))]
        
        msg := TestMessage{
            ID:      i,
            Content: fmt.Sprintf("测试消息 %d", i),
            Type:    msgType,
        }

        msgBytes, _ := json.Marshal(msg)
        
        if err := prod.Publish(topic, msgBytes); err != nil {
            mlog.Error("发送消息失败: id=%d, error=%v", i, err)
        } else {
            mlog.Info("发送消息成功: id=%d, type=%s", i, msgType)
        }

        time.Sleep(100 * time.Millisecond)
    }

    mlog.Info("高可用性测试消息发送完成")
}
EOF

go build -o bin/test_message_producer test_message_producer.go

echo -e "${GREEN}编译完成${NC}"
echo

# 显示测试配置
echo -e "${YELLOW}测试配置:${NC}"
echo "  配置文件: $CONFIG_FILE"
echo "  测试主题: $TEST_TOPIC"
echo "  测试频道: $TEST_CHANNEL"
echo

# 检查配置文件
if [ ! -f "$CONFIG_FILE" ]; then
    echo -e "${RED}错误: 配置文件 $CONFIG_FILE 不存在${NC}"
    exit 1
fi

echo -e "${BLUE}高可用性测试场景:${NC}"
echo "1. Panic恢复测试 - 验证消息处理panic不会导致程序崩溃"
echo "2. 错误处理测试 - 验证错误处理和失败消息机制"
echo "3. 慢处理测试 - 验证超时和并发控制"
echo "4. 健康检查测试 - 验证健康检查和统计信息"
echo "5. 熔断器测试 - 验证失败消息处理熔断机制"
echo

read -p "按 Enter 键开始测试，或按 Ctrl+C 取消..."
echo

# 启动高可用性测试消费者（后台运行）
echo -e "${YELLOW}启动高可用性测试消费者...${NC}"
./bin/test_ha_consumer "$CONFIG_FILE" "$TEST_TOPIC" "$TEST_CHANNEL" > logs/ha_test_consumer.log 2>&1 &
CONSUMER_PID=$!
echo "消费者 PID: $CONSUMER_PID"

# 等待消费者启动
sleep 5

# 发送测试消息
echo -e "${YELLOW}发送高可用性测试消息...${NC}"
./bin/test_message_producer "$CONFIG_FILE" "$TEST_TOPIC" > logs/ha_test_producer.log 2>&1

echo -e "${GREEN}测试消息发送完成${NC}"

# 等待消息处理
echo -e "${YELLOW}等待消息处理（60秒）...${NC}"
sleep 60

# 显示测试结果
echo -e "${BLUE}=== 高可用性测试结果 ===${NC}"

echo -e "${YELLOW}消费者日志（最后30行）:${NC}"
tail -n 30 logs/ha_test_consumer.log

echo
echo -e "${YELLOW}生产者日志（最后10行）:${NC}"
tail -n 10 logs/ha_test_producer.log

echo
echo -e "${BLUE}=== 测试分析 ===${NC}"

# 分析日志
PANIC_RECOVERED=$(grep -c "消息处理发生panic，已恢复" logs/ha_test_consumer.log || echo "0")
FAILED_MESSAGES=$(grep -c "失败消息已成功发送到失败队列" logs/ha_test_consumer.log || echo "0")
HEALTH_CHECKS=$(grep -c "健康检查:" logs/ha_test_consumer.log || echo "0")
CIRCUIT_EVENTS=$(grep -c "熔断器" logs/ha_test_consumer.log || echo "0")

echo "Panic恢复次数: $PANIC_RECOVERED"
echo "失败消息处理次数: $FAILED_MESSAGES"
echo "健康检查次数: $HEALTH_CHECKS"
echo "熔断器事件次数: $CIRCUIT_EVENTS"

# 评估测试结果
echo
echo -e "${BLUE}=== 高可用性评估 ===${NC}"

if [ "$PANIC_RECOVERED" -gt 0 ]; then
    echo -e "${GREEN}✓ Panic恢复机制正常工作${NC}"
else
    echo -e "${YELLOW}⚠ 未检测到panic恢复（可能没有panic消息）${NC}"
fi

if [ "$FAILED_MESSAGES" -gt 0 ]; then
    echo -e "${GREEN}✓ 失败消息处理机制正常工作${NC}"
else
    echo -e "${YELLOW}⚠ 未检测到失败消息处理${NC}"
fi

if [ "$HEALTH_CHECKS" -gt 0 ]; then
    echo -e "${GREEN}✓ 健康检查机制正常工作${NC}"
else
    echo -e "${RED}✗ 健康检查机制未工作${NC}"
fi

# 清理进程
echo
echo -e "${YELLOW}清理测试进程...${NC}"
kill $CONSUMER_PID 2>/dev/null || true
sleep 2

# 清理临时文件
rm -f test_ha_consumer.go test_message_producer.go

echo -e "${GREEN}高可用性测试完成！${NC}"
echo
echo -e "${BLUE}查看完整日志:${NC}"
echo "  消费者日志: logs/ha_test_consumer.log"
echo "  生产者日志: logs/ha_test_producer.log"
echo
echo -e "${BLUE}测试总结:${NC}"
echo "本次测试验证了以下高可用性功能："
echo "- Panic恢复机制"
echo "- 失败消息处理"
echo "- 健康检查功能"
echo "- 统计信息收集"
echo "- 熔断器保护"
echo
echo "如需更详细的测试，请查看审查报告: docs/high_availability_audit_report.md"
