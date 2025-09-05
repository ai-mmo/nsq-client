#!/bin/bash

# 版本发布脚本
# 用法: ./release.sh [版本号] [发布说明]
# 示例: ./release.sh v1.0.1 "修复日志级别问题"

set -e  # 遇到错误立即退出

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# 日志函数
log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

log_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# 检查是否在 git 仓库中
check_git_repo() {
    if ! git rev-parse --git-dir > /dev/null 2>&1; then
        log_error "当前目录不是 git 仓库"
        exit 1
    fi
}

# 检查工作目录是否干净
check_clean_working_dir() {
    if ! git diff-index --quiet HEAD --; then
        log_error "工作目录有未提交的更改，请先提交或暂存"
        git status --porcelain
        exit 1
    fi
}

# 检查是否在主分支
check_main_branch() {
    current_branch=$(git branch --show-current)
    if [[ "$current_branch" != "main" && "$current_branch" != "master" ]]; then
        log_warning "当前不在主分支 ($current_branch)，是否继续? (y/N)"
        read -r response
        if [[ ! "$response" =~ ^[Yy]$ ]]; then
            log_info "已取消发布"
            exit 0
        fi
    fi
}

# 验证版本号格式
validate_version() {
    local version=$1
    if [[ ! $version =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[a-zA-Z0-9]+)?$ ]]; then
        log_error "版本号格式错误，应为 vX.Y.Z 或 vX.Y.Z-suffix 格式"
        log_info "示例: v1.0.0, v1.2.3, v2.0.0-beta"
        exit 1
    fi
}

# 检查版本号是否已存在
check_version_exists() {
    local version=$1
    if git tag -l | grep -q "^$version$"; then
        log_error "版本 $version 已存在"
        git tag -l | grep "^v" | sort -V | tail -5
        exit 1
    fi
}

# 获取最新版本
get_latest_version() {
    git tag -l | grep "^v" | sort -V | tail -1
}

# 运行测试
run_tests() {
    log_info "运行测试..."

    # 运行测试并捕获输出
    if ! go test -v ./... 2>&1; then
        log_error "测试失败，请修复后重试"
        log_info "提示: 可以运行 'go test -v ./...' 查看详细错误信息"
        exit 1
    fi
    log_success "所有测试通过"
}

# 运行代码检查
run_lint() {
    log_info "运行代码检查..."
    
    # 检查 go fmt
    if ! gofmt -l . | grep -q '^$'; then
        log_warning "代码格式不规范，正在自动格式化..."
        go fmt ./...
        log_success "代码格式化完成"
    fi
    
    # 检查 go vet
    if ! go vet ./...; then
        log_error "代码检查失败，请修复后重试"
        exit 1
    fi
    
    log_success "代码检查通过"
}

# 更新版本信息
update_version_info() {
    local version=$1
    local version_without_v=${version#v}
    
    # 如果存在 version.go 文件，更新版本信息
    if [[ -f "version.go" ]]; then
        log_info "更新 version.go 文件..."
        sed -i.bak "s/const Version = \".*\"/const Version = \"$version_without_v\"/" version.go
        rm -f version.go.bak
        git add version.go
    fi
    
    # 更新 README.md 中的版本信息（如果存在）
    if grep -q "go get" README.md; then
        log_info "更新 README.md 中的版本信息..."
        # 这里可以根据实际需要更新 README 中的版本引用
    fi
}

# 生成变更日志
generate_changelog() {
    local version=$1
    local message=$2
    local latest_version=$(get_latest_version)
    
    log_info "生成变更日志..."
    
    if [[ -n "$latest_version" ]]; then
        echo "## $version ($(date +%Y-%m-%d))" > /tmp/changelog.md
        echo "" >> /tmp/changelog.md
        echo "$message" >> /tmp/changelog.md
        echo "" >> /tmp/changelog.md
        echo "### 提交记录:" >> /tmp/changelog.md
        git log --oneline "$latest_version"..HEAD >> /tmp/changelog.md
    else
        echo "## $version ($(date +%Y-%m-%d))" > /tmp/changelog.md
        echo "" >> /tmp/changelog.md
        echo "$message" >> /tmp/changelog.md
        echo "" >> /tmp/changelog.md
        echo "### 提交记录:" >> /tmp/changelog.md
        git log --oneline >> /tmp/changelog.md
    fi
}

# 创建标签和发布
create_release() {
    local version=$1
    local message=$2
    
    log_info "创建 Git 标签 $version..."
    
    # 创建带注释的标签
    git tag -a "$version" -m "$message"
    
    log_success "标签 $version 创建成功"
    
    # 推送标签到远程仓库
    log_info "推送标签到远程仓库..."
    git push origin "$version"
    
    log_success "标签已推送到远程仓库"
}

# 显示发布信息
show_release_info() {
    local version=$1
    
    echo ""
    log_success "🎉 版本 $version 发布成功!"
    echo ""
    echo "发布信息:"
    echo "- 版本: $version"
    echo "- 时间: $(date)"
    echo "- 分支: $(git branch --show-current)"
    echo "- 提交: $(git rev-parse --short HEAD)"
    echo ""
    echo "下一步操作:"
    echo "1. 检查 GitHub/GitLab 上的发布页面"
    echo "2. 更新文档和示例"
    echo "3. 通知团队成员"
    echo ""
}

# 主函数
main() {
    local version=$1
    local message=$2
    
    log_info "开始版本发布流程..."
    
    # 检查参数
    if [[ -z "$version" ]]; then
        log_error "请提供版本号"
        echo "用法: $0 <版本号> [发布说明]"
        echo "示例: $0 v1.0.1 '修复日志级别问题'"
        exit 1
    fi
    
    if [[ -z "$message" ]]; then
        message="Release $version"
    fi
    
    # 执行检查
    check_git_repo
    check_clean_working_dir
    check_main_branch
    validate_version "$version"
    check_version_exists "$version"
    
    # 运行测试和检查
    run_tests
    run_lint
    
    # 更新版本信息
    update_version_info "$version"
    
    # 如果有版本文件更新，提交更改
    if ! git diff-index --quiet HEAD --; then
        log_info "提交版本信息更新..."
        git commit -m "chore: bump version to $version"
    fi
    
    # 生成变更日志
    generate_changelog "$version" "$message"
    
    # 显示变更日志预览
    echo ""
    log_info "变更日志预览:"
    echo "----------------------------------------"
    cat /tmp/changelog.md
    echo "----------------------------------------"
    echo ""
    
    # 确认发布
    log_warning "确认发布版本 $version? (y/N)"
    read -r response
    if [[ ! "$response" =~ ^[Yy]$ ]]; then
        log_info "已取消发布"
        exit 0
    fi
    
    # 创建发布
    create_release "$version" "$message"
    
    # 显示发布信息
    show_release_info "$version"
    
    # 清理临时文件
    rm -f /tmp/changelog.md
}

# 脚本入口
main "$@"
