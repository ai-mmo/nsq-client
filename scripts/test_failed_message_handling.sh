#!/bin/bash

# NSQ 失败消息处理功能测试脚本

set -e

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# 配置
CONFIG_FILE="config/config.yaml"
TEST_TOPIC="test_failed_handling"
TEST_CHANNEL="test_channel"
FAILED_TOPIC="${TEST_TOPIC}_failed_channel"

echo -e "${BLUE}=== NSQ 失败消息处理功能测试 ===${NC}"
echo

# 检查依赖
echo -e "${YELLOW}检查依赖...${NC}"
if ! command -v go &> /dev/null; then
    echo -e "${RED}错误: Go 未安装${NC}"
    exit 1
fi

# 编译示例程序
echo -e "${YELLOW}编译示例程序...${NC}"
go build -o bin/consumer_with_failed_handler examples/failed_message_handling/consumer/main.go
go build -o bin/failed_message_viewer examples/failed_message_handling/viewer/main.go
go build -o bin/producer_example examples/producer_example.go

echo -e "${GREEN}编译完成${NC}"
echo

# 创建 bin 目录
mkdir -p bin
mkdir -p logs

# 显示配置信息
echo -e "${YELLOW}测试配置:${NC}"
echo "  配置文件: $CONFIG_FILE"
echo "  测试主题: $TEST_TOPIC"
echo "  测试频道: $TEST_CHANNEL"
echo "  失败队列主题: $FAILED_TOPIC"
echo

# 检查配置文件
if [ ! -f "$CONFIG_FILE" ]; then
    echo -e "${RED}错误: 配置文件 $CONFIG_FILE 不存在${NC}"
    exit 1
fi

echo -e "${YELLOW}当前失败消息处理配置:${NC}"
grep -A 10 "failed_message:" "$CONFIG_FILE" || echo "  未找到失败消息配置"
echo

# 提示用户
echo -e "${BLUE}测试步骤:${NC}"
echo "1. 启动带失败消息处理的消费者"
echo "2. 启动失败消息查看器"
echo "3. 发送测试消息（包含会失败的消息）"
echo "4. 观察失败消息处理过程"
echo

read -p "按 Enter 键开始测试，或按 Ctrl+C 取消..."
echo

# 启动带失败消息处理的消费者（后台运行）
echo -e "${YELLOW}启动带失败消息处理的消费者...${NC}"
./bin/consumer_with_failed_handler "$CONFIG_FILE" "$TEST_TOPIC" "$TEST_CHANNEL" > logs/consumer_failed_handler.log 2>&1 &
CONSUMER_PID=$!
echo "消费者 PID: $CONSUMER_PID"

# 等待消费者启动
sleep 3

# 启动失败消息查看器（后台运行）
echo -e "${YELLOW}启动失败消息查看器...${NC}"
./bin/failed_message_viewer "$CONFIG_FILE" "$TEST_TOPIC" > logs/failed_message_viewer.log 2>&1 &
VIEWER_PID=$!
echo "失败消息查看器 PID: $VIEWER_PID"

# 等待查看器启动
sleep 3

# 发送测试消息
echo -e "${YELLOW}发送测试消息...${NC}"
echo "发送 20 条测试消息（其中一些会失败）..."

for i in {1..20}; do
    echo "发送消息 $i/20"
    ./bin/producer_example "$CONFIG_FILE" "$TEST_TOPIC" 1 > /dev/null 2>&1
    sleep 0.5
done

echo -e "${GREEN}测试消息发送完成${NC}"
echo

# 等待消息处理
echo -e "${YELLOW}等待消息处理（30秒）...${NC}"
sleep 30

# 显示日志
echo -e "${BLUE}=== 消费者日志（最后20行）===${NC}"
tail -n 20 logs/consumer_failed_handler.log
echo

echo -e "${BLUE}=== 失败消息查看器日志（最后20行）===${NC}"
tail -n 20 logs/failed_message_viewer.log
echo

# 清理进程
echo -e "${YELLOW}清理测试进程...${NC}"
kill $CONSUMER_PID 2>/dev/null || true
kill $VIEWER_PID 2>/dev/null || true

# 等待进程结束
sleep 2

echo -e "${GREEN}测试完成！${NC}"
echo
echo -e "${BLUE}查看完整日志:${NC}"
echo "  消费者日志: logs/consumer_failed_handler.log"
echo "  失败消息查看器日志: logs/failed_message_viewer.log"
echo
echo -e "${BLUE}手动测试命令:${NC}"
echo "  启动消费者: ./bin/consumer_with_failed_handler $CONFIG_FILE $TEST_TOPIC $TEST_CHANNEL"
echo "  启动查看器: ./bin/failed_message_viewer $CONFIG_FILE $TEST_TOPIC"
echo "  发送消息: ./bin/producer_example $CONFIG_FILE $TEST_TOPIC 10"
echo

# 检查是否有失败消息
if grep -q "失败消息已成功发送到失败队列" logs/consumer_failed_handler.log; then
    echo -e "${GREEN}✓ 检测到失败消息处理${NC}"
else
    echo -e "${YELLOW}⚠ 未检测到失败消息处理，可能所有消息都处理成功了${NC}"
fi

if grep -q "=== 失败消息详情 ===" logs/failed_message_viewer.log; then
    echo -e "${GREEN}✓ 检测到失败消息查看${NC}"
else
    echo -e "${YELLOW}⚠ 未检测到失败消息查看，可能失败队列中没有消息${NC}"
fi

echo
echo -e "${BLUE}测试说明:${NC}"
echo "- 消费者会模拟处理失败（约30%的消息会失败）"
echo "- 失败的消息会被发送到失败队列: $FAILED_TOPIC"
echo "- 失败消息查看器会显示失败消息的详细信息"
echo "- 可以通过日志文件查看完整的处理过程"
