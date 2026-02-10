#!/bin/bash

# subs-check 一键管理脚本
# 支持：安装、升级、卸载、启动、停止、重启、状态查看、日志查看等

set -e

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
MAGENTA='\033[0;35m'
NC='\033[0m' # No Color

# 配置变量
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BINARY_PATH="${SCRIPT_DIR}/subs-check"
CONFIG_PATH="${SCRIPT_DIR}/config/config.yaml"
SERVICE_NAME="subs-check.service"
SERVICE_PATH="/lib/systemd/system/${SERVICE_NAME}"
GITHUB_REPO="55gY/subs-check"

# GitHub 加速镜像列表（按优先级排序）
GITHUB_MIRRORS=(
    "https://xuc.xi-xu.me/"
    "https://ghfast.top/"
    "https://github.akams.cn/"
    "https://gh-proxy.com/"
    "https://kkgithub.com/"
    "https://xiake.pro/"
    ""  # 直连
    
)
 
# 显示 Logo
show_logo() {
    clear
    echo -e "${CYAN}"
    cat << "EOF"
 ____        _                ____ _               _    
/ ___| _   _| |__  ___       / ___| |__   ___  ___| | __
\___ \| | | | '_ \/ __|_____| |   | '_ \ / _ \/ __| |/ /
 ___) | |_| | |_) \__ \_____| |___| | | |  __/ (__|   < 
|____/ \__,_|_.__/|___/      \____|_| |_|\___|\___|_|\_\
                                                          
EOF
    echo -e "${NC}"
    echo -e "${GREEN}Subscription Proxy Checker - 订阅节点检测工具${NC}"
    echo -e "${BLUE}https://github.com/${GITHUB_REPO}${NC}"
    echo ""
}

# 显示主菜单
show_menu() {
    show_logo
    
    # 显示当前状态
    echo -e "${CYAN}=== 当前状态 ===${NC}"
    if [ -f "$BINARY_PATH" ]; then
        echo -e "二进制文件: ${GREEN}已安装${NC}"
    else
        echo -e "二进制文件: ${RED}未安装${NC}"
    fi
    
    if [ -f "$SERVICE_PATH" ]; then
        if systemctl is-active --quiet "$SERVICE_NAME"; then
            echo -e "服务状态:   ${GREEN}运行中 ✓${NC}"
        else
            echo -e "服务状态:   ${YELLOW}已停止${NC}"
        fi
        
        if systemctl is-enabled --quiet "$SERVICE_NAME" 2>/dev/null; then
            echo -e "开机自启:   ${GREEN}已启用${NC}"
        else
            echo -e "开机自启:   ${YELLOW}未启用${NC}"
        fi
    else
        echo -e "服务状态:   ${RED}未安装${NC}"
    fi
    
    echo ""
    echo -e "${CYAN}=== 主菜单 ===${NC}"
    echo -e "${YELLOW}1.${NC}  安装 subs-check"
    echo -e "${YELLOW}2.${NC}  升级到最新版本"
    echo -e "${YELLOW}3.${NC}  卸载 subs-check"
    echo ""
    echo -e "${YELLOW}4.${NC}  启动服务"
    echo -e "${YELLOW}5.${NC}  停止服务"
    echo -e "${YELLOW}6.${NC}  重启服务"
    echo -e "${YELLOW}7.${NC}  查看服务状态"
    echo ""
    echo -e "${YELLOW}8.${NC}  启用开机自启"
    echo -e "${YELLOW}9.${NC}  禁用开机自启"
    echo ""
    echo -e "${YELLOW}10.${NC} 查看实时日志"
    echo -e "${YELLOW}11.${NC} 查看完整日志"
    echo ""
    echo -e "${YELLOW}12.${NC} 编辑配置文件"
    echo -e "${YELLOW}13.${NC} 测试配置语法"
    echo ""
    echo -e "${YELLOW}14.${NC} 实时监控服务"
    echo -e "${YELLOW}15.${NC} 显示安装信息"
    echo ""
    echo -e "${YELLOW}16.${NC} 仅安装 systemd 服务"
    echo -e "${YELLOW}17.${NC} 仅移除 systemd 服务"
    echo ""
    echo -e "${RED}0.${NC}  退出"
    echo ""
}

# 等待用户按键
press_any_key() {
    echo ""
    echo -e "${CYAN}按任意键返回主菜单...${NC}"
    read -n 1 -s
}

# 检查 root 权限（某些操作需要）
check_root() {
    if [ "$EUID" -ne 0 ]; then 
        echo -e "${RED}错误: 此操作需要 root 权限${NC}"
        echo -e "请使用: ${YELLOW}sudo $(basename $0) $1${NC}"
        exit 1
    fi
}

# 检查并安装必要工具
check_dependencies() {
    echo -e "${CYAN}正在检查依赖...${NC}"
    
    local missing_deps=()
    local need_install=0
    
    # 检查下载工具
    if ! command -v curl &> /dev/null && ! command -v wget &> /dev/null; then
        missing_deps+=("curl")
        need_install=1
    fi
    
    # 检查解压工具
    if ! command -v tar &> /dev/null; then
        missing_deps+=("tar")
        need_install=1
    fi
    
    if ! command -v gzip &> /dev/null; then
        missing_deps+=("gzip")
        need_install=1
    fi
    
    # 检查 file 命令（用于验证文件类型）
    if ! command -v file &> /dev/null; then
        missing_deps+=("file")
        need_install=1
    fi
    
    # 如果有缺失依赖，尝试自动安装
    if [ $need_install -eq 1 ]; then
        echo -e "${YELLOW}缺少必要工具: ${missing_deps[*]}${NC}"
        echo -e "${BLUE}正在尝试自动安装...${NC}"
        
        # 检测系统包管理器并安装
        if command -v apt-get &> /dev/null; then
            echo -e "${GREEN}检测到 apt-get 包管理器${NC}"
            apt-get update -qq
            apt-get install -y curl tar gzip file
        elif command -v yum &> /dev/null; then
            echo -e "${GREEN}检测到 yum 包管理器${NC}"
            yum install -y curl tar gzip file
        elif command -v dnf &> /dev/null; then
            echo -e "${GREEN}检测到 dnf 包管理器${NC}"
            dnf install -y curl tar gzip file
        elif command -v apk &> /dev/null; then
            echo -e "${GREEN}检测到 apk 包管理器${NC}"
            apk add --no-cache curl tar gzip file
        elif command -v pacman &> /dev/null; then
            echo -e "${GREEN}检测到 pacman 包管理器${NC}"
            pacman -Sy --noconfirm curl tar gzip file
        else
            echo -e "${RED}错误: 无法识别系统包管理器${NC}"
            echo ""
            echo "请手动安装必要工具:"
            echo "  Ubuntu/Debian: sudo apt-get install curl tar gzip file"
            echo "  CentOS/RHEL:   sudo yum install curl tar gzip file"
            echo "  Fedora:        sudo dnf install curl tar gzip file"
            echo "  Alpine:        sudo apk add curl tar gzip file"
            echo "  Arch:          sudo pacman -S curl tar gzip file"
            exit 1
        fi
        
        # 再次检查是否安装成功
        if ! command -v curl &> /dev/null && ! command -v wget &> /dev/null; then
            echo -e "${RED}错误: 下载工具安装失败${NC}"
            exit 1
        fi
        
        if ! command -v tar &> /dev/null; then
            echo -e "${RED}错误: tar 安装失败${NC}"
            exit 1
        fi
        
        if ! command -v gzip &> /dev/null; then
            echo -e "${RED}错误: gzip 安装失败${NC}"
            exit 1
        fi
        
        if ! command -v file &> /dev/null; then
            echo -e "${YELLOW}警告: file 命令安装失败，将跳过文件类型验证${NC}"
        fi
        
        echo -e "${GREEN}✓ 依赖安装完成${NC}"
    else
        echo -e "${GREEN}✓ 所有依赖已满足${NC}"
    fi
}

# 获取最新版本下载链接（包括 Pre-release）
get_latest_release_url() {
    echo -e "${BLUE}正在获取最新版本信息...${NC}" >&2
    
    local api_url="https://api.github.com/repos/${GITHUB_REPO}/releases"
    local download_url
    
    # 检测系统架构
    local arch=$(uname -m)
    local arch_pattern
    case "$arch" in
        x86_64|amd64)
            arch_pattern="x86_64"
            ;;
        aarch64|arm64)
            arch_pattern="arm64"
            ;;
        armv7l)
            arch_pattern="armv7"
            ;;
        *)
            echo -e "${YELLOW}警告: 未知架构 $arch，尝试使用 x86_64${NC}" >&2
            arch_pattern="x86_64"
            ;;
    esac
    
    # 检测操作系统
    local os=$(uname -s)
    local os_pattern
    case "$os" in
        Linux)
            os_pattern="Linux"
            ;;
        Darwin)
            os_pattern="Darwin"
            ;;
        *)
            echo -e "${YELLOW}警告: 未知操作系统 $os，尝试使用 Linux${NC}" >&2
            os_pattern="Linux"
            ;;
    esac
    
    echo -e "${CYAN}检测到系统: ${os_pattern} ${arch_pattern}${NC}" >&2
    
    # 获取所有 releases（包括 pre-release），查找匹配当前系统的下载链接
    if command -v curl &> /dev/null; then
        # 首先尝试精确匹配系统和架构
        download_url=$(curl -s "$api_url" | grep -o '"browser_download_url": *"[^"]*'"${os_pattern}"'[^"]*'"${arch_pattern}"'[^"]*\.tar\.gz"' | head -n 1 | sed 's/"browser_download_url": *"\([^"]*\)"/\1/' | tr -d '\r\n')
        
        # 如果没找到，尝试只匹配 tar.gz（兼容旧版本）
        if [ -z "$download_url" ]; then
            download_url=$(curl -s "$api_url" | grep -o '"browser_download_url": *"[^"]*\.tar\.gz"' | head -n 1 | sed 's/"browser_download_url": *"\([^"]*\)"/\1/' | tr -d '\r\n')
        fi
    else
        # 使用 wget 获取
        download_url=$(wget -qO- "$api_url" | grep -o '"browser_download_url": *"[^"]*'"${os_pattern}"'[^"]*'"${arch_pattern}"'[^"]*\.tar\.gz"' | head -n 1 | sed 's/"browser_download_url": *"\([^"]*\)"/\1/' | tr -d '\r\n')
        
        if [ -z "$download_url" ]; then
            download_url=$(wget -qO- "$api_url" | grep -o '"browser_download_url": *"[^"]*\.tar\.gz"' | head -n 1 | sed 's/"browser_download_url": *"\([^"]*\)"/\1/' | tr -d '\r\n')
        fi
    fi
    
    if [ -z "$download_url" ]; then
        echo -e "${RED}错误: 无法获取下载链接${NC}" >&2
        echo "请手动访问: https://github.com/${GITHUB_REPO}/releases" >&2
        exit 1
    fi
    
    # 显示即将下载的版本信息
    local version=$(echo "$download_url" | grep -o 'v[0-9]\+\.[0-9]\+\.[0-9]\+-[0-9]\+' || echo "未知版本")
    local filename=$(basename "$download_url")
    echo -e "${GREEN}找到最新版本: ${version}${NC}" >&2
    echo -e "${CYAN}文件名: ${filename}${NC}" >&2
    
    echo "$download_url"
}

# 下载并安装二进制文件
download_binary() {
    local download_url="$1"
    local tmp_file="/tmp/subs-check-latest.tar.gz"
    
    echo -e "${YELLOW}正在尝试下载二进制文件...${NC}"
    
    # 尝试使用不同的镜像下载
    local success=0
    for mirror in "${GITHUB_MIRRORS[@]}"; do
        local url="${mirror}${download_url}"
        
        if [ -z "$mirror" ]; then
            echo -e "${BLUE}尝试直连 GitHub...${NC}"
        else
            echo -e "${BLUE}尝试镜像: ${mirror}${NC}"
        fi
        
        # 使用超时和重试
        if command -v curl &> /dev/null; then
            curl -L --connect-timeout 10 --max-time 300 --retry 3 --retry-delay 2 \
                 --progress-bar -o "$tmp_file" "$url"
        else
            wget --timeout=10 --tries=3 --waitretry=2 \
                 --show-progress -O "$tmp_file" "$url"
        fi
        
        if [ $? -eq 0 ] && [ -f "$tmp_file" ] && [ -s "$tmp_file" ]; then
            # 验证文件是否为正确的 gzip 格式
            if file "$tmp_file" 2>/dev/null | grep -q "gzip compressed data"; then
                success=1
                echo -e "${GREEN}✓ 下载成功${NC}"
                break
            else
                echo -e "${YELLOW}下载的文件格式不正确，尝试下一个镜像...${NC}"
                rm -f "$tmp_file"
            fi
        else
            echo -e "${YELLOW}下载失败，尝试下一个镜像...${NC}"
            rm -f "$tmp_file"
        fi
    done
    
    if [ $success -eq 0 ]; then
        echo -e "${RED}错误: 所有镜像均下载失败${NC}"
        echo -e "${YELLOW}请检查网络连接或稍后重试${NC}"
        echo -e "${YELLOW}也可以手动下载: ${download_url}${NC}"
        exit 1
    fi
    
    echo -e "${YELLOW}正在解压...${NC}"
    
    # 创建临时目录用于解压
    local tmp_extract_dir="/tmp/subs-check-extract-$$"
    mkdir -p "$tmp_extract_dir"
    
    # 解压到临时目录
    tar -xzf "$tmp_file" -C "$tmp_extract_dir"
    
    if [ $? -ne 0 ]; then
        echo -e "${RED}错误: 解压失败${NC}"
        rm -rf "$tmp_file" "$tmp_extract_dir"
        exit 1
    fi
    
    # 查找可执行文件（可能是 subs-check 或其他名称）
    local binary_file=$(find "$tmp_extract_dir" -type f -executable 2>/dev/null | head -n 1)
    
    # 如果没找到可执行文件，尝试查找名称包含 subs-check 的文件
    if [ -z "$binary_file" ]; then
        binary_file=$(find "$tmp_extract_dir" -type f -name "*subs-check*" | head -n 1)
    fi
    
    # 如果还是没找到，尝试查找任何文件（排除目录）
    if [ -z "$binary_file" ]; then
        binary_file=$(find "$tmp_extract_dir" -type f ! -name "*.md" ! -name "*.txt" ! -name "LICENSE" | head -n 1)
    fi
    
    if [ -z "$binary_file" ] || [ ! -f "$binary_file" ]; then
        echo -e "${RED}错误: 无法找到二进制文件${NC}"
        echo -e "${YELLOW}压缩包内容:${NC}"
        ls -la "$tmp_extract_dir"
        rm -rf "$tmp_file" "$tmp_extract_dir"
        exit 1
    fi
    
    # 移动二进制文件到目标位置
    mv "$binary_file" "$BINARY_PATH"
    chmod +x "$BINARY_PATH"
    
    # 清理临时文件
    rm -rf "$tmp_file" "$tmp_extract_dir"
    
    echo -e "${GREEN}✓ 二进制文件安装成功${NC}"
}

# 获取仓库默认分支
get_default_branch() {
    local api_url="https://api.github.com/repos/${GITHUB_REPO}"
    local default_branch
    
    if command -v curl &> /dev/null; then
        default_branch=$(curl -s "$api_url" | grep -o '"default_branch": *"[^"]*"' | sed 's/"default_branch": *"\([^"]*\)"/\1/' | tr -d '\r\n')
    else
        default_branch=$(wget -qO- "$api_url" | grep -o '"default_branch": *"[^"]*"' | sed 's/"default_branch": *"\([^"]*\)"/\1/' | tr -d '\r\n')
    fi
    
    # 如果获取失败，回退到 master
    if [ -z "$default_branch" ]; then
        default_branch="master"
    fi
    
    echo "$default_branch"
}

# 下载配置文件
download_config() {
    local default_branch=$(get_default_branch)
    local config_url="https://raw.githubusercontent.com/${GITHUB_REPO}/${default_branch}/config/config.example.yaml"
    
    echo -e "${YELLOW}正在下载示例配置文件...${NC}"
    echo -e "${CYAN}使用分支: ${default_branch}${NC}"
    
    mkdir -p "$(dirname "$CONFIG_PATH")"
    
    # 尝试使用不同的镜像下载
    local success=0
    for mirror in "${GITHUB_MIRRORS[@]}"; do
        local url="${mirror}${config_url}"
        
        if [ -z "$mirror" ]; then
            echo -e "${BLUE}尝试直连 GitHub...${NC}"
        else
            echo -e "${BLUE}尝试镜像: ${mirror}${NC}"
        fi
        
        if command -v curl &> /dev/null; then
            curl -L --connect-timeout 10 --max-time 60 --retry 3 --retry-delay 2 \
                 -o "$CONFIG_PATH" "$url"
        else
            wget --timeout=10 --tries=3 --waitretry=2 \
                 -O "$CONFIG_PATH" "$url"
        fi
        
        if [ $? -eq 0 ] && [ -f "$CONFIG_PATH" ] && [ -s "$CONFIG_PATH" ]; then
            success=1
            echo -e "${GREEN}✓ 配置文件下载成功${NC}"
            break
        else
            echo -e "${YELLOW}下载失败，尝试下一个镜像...${NC}"
            rm -f "$CONFIG_PATH"
        fi
    done
    
    if [ $success -eq 0 ]; then
        echo -e "${RED}错误: 配置文件下载失败${NC}"
        echo -e "${YELLOW}请手动从以下地址下载: ${config_url}${NC}"
        exit 1
    fi
    
    echo -e "${YELLOW}配置文件位置: ${CONFIG_PATH}${NC}"
    echo -e "${YELLOW}请根据需要编辑配置文件${NC}"
}

# 创建 systemd 服务
create_systemd_service() {
    echo -e "${YELLOW}创建 systemd 服务...${NC}"
    
    cat > "$SERVICE_PATH" <<EOF
[Unit]
Description=subs-check - Subscription Proxy Checker Service
Documentation=https://github.com/${GITHUB_REPO}
After=network.target

[Service]
Type=simple
User=root
WorkingDirectory=${SCRIPT_DIR}
ExecStart=${BINARY_PATH} -f ${CONFIG_PATH}
Restart=always
RestartSec=10
StandardOutput=journal
StandardError=journal

# 内存管理（可选，根据需要取消注释）
# 软限制：限制最大内存使用，不会杀死进程
# Environment="SUB_CHECK_MEM_SOFT_LIMIT=2GB"
# 硬限制：超过限制后自动重启进程
# Environment="SUB_CHECK_MEM_LIMIT=4GB"
# 内存监控：输出详细内存使用信息
# Environment="SUB_CHECK_MEM_MONITOR=1"

# 停止服务时的处理
KillMode=mixed
KillSignal=SIGTERM
TimeoutStopSec=30

[Install]
WantedBy=multi-user.target
EOF
    
    systemctl daemon-reload
    echo -e "${GREEN}✓ systemd 服务创建成功${NC}"
}

# 移除 systemd 服务
remove_systemd_service() {
    if [ -f "$SERVICE_PATH" ]; then
        echo -e "${YELLOW}停止并移除 systemd 服务...${NC}"
        systemctl stop "$SERVICE_NAME" 2>/dev/null || true
        systemctl disable "$SERVICE_NAME" 2>/dev/null || true
        rm -f "$SERVICE_PATH"
        systemctl daemon-reload
        echo -e "${GREEN}✓ systemd 服务已移除${NC}"
    else
        echo -e "${YELLOW}systemd 服务不存在，跳过${NC}"
    fi
}

# 安装
cmd_install() {
    check_root
    check_dependencies
    
    show_logo
    echo -e "${GREEN}=== 开始安装 subs-check ===${NC}"
    echo ""
    
    # 检查是否已安装
    if [ -f "$BINARY_PATH" ] || [ -f "$SERVICE_PATH" ]; then
        echo -e "${YELLOW}检测到已安装的版本${NC}"
        read -p "是否覆盖安装？配置文件会保留 (y/n): " -n 1 -r
        echo
        if [[ ! $REPLY =~ ^[Yy]$ ]]; then
            echo -e "${BLUE}安装已取消${NC}"
            press_any_key
            return
        fi
    fi
    
    # 下载二进制文件
    if [ ! -f "$BINARY_PATH" ]; then
        download_url=$(get_latest_release_url)
        download_binary "$download_url"
    else
        echo -e "${GREEN}✓ 二进制文件已存在${NC}"
    fi
    
    # 下载配置文件
    if [ ! -f "$CONFIG_PATH" ]; then
        download_config
    else
        echo -e "${GREEN}✓ 配置文件已存在，保留现有配置${NC}"
    fi
    
    # 创建 systemd 服务
    create_systemd_service
    
    # 启动服务
    echo -e "${YELLOW}启动服务...${NC}"
    systemctl enable "$SERVICE_NAME"
    systemctl start "$SERVICE_NAME"
    
    sleep 2
    
    echo ""
    echo -e "${GREEN}=== 安装完成 ===${NC}"
    press_any_key
}

# 升级
cmd_upgrade() {
    check_root
    check_dependencies
    
    show_logo
    echo -e "${GREEN}=== 升级 subs-check ===${NC}"
    echo ""
    
    if [ ! -f "$BINARY_PATH" ]; then
        echo -e "${RED}错误: 未检测到已安装的版本${NC}"
        echo -e "请先使用选项 1 进行安装${NC}"
        press_any_key
        return
    fi
    
    # 获取当前版本
    if [ -x "$BINARY_PATH" ]; then
        echo -e "${BLUE}当前版本信息:${NC}"
        "$BINARY_PATH" --version 2>/dev/null || echo "无法获取版本信息"
        echo ""
    fi
    
    # 停止服务
    if systemctl is-active --quiet "$SERVICE_NAME"; then
        echo -e "${YELLOW}停止服务...${NC}"
        systemctl stop "$SERVICE_NAME"
    fi
    
    # 备份旧版本
    if [ -f "$BINARY_PATH" ]; then
        backup_file="${BINARY_PATH}.backup.$(date +%Y%m%d%H%M%S)"
        echo -e "${YELLOW}备份旧版本到: ${backup_file}${NC}"
        cp "$BINARY_PATH" "$backup_file"
    fi
    
    # 下载新版本
    download_url=$(get_latest_release_url)
    download_binary "$download_url"
    
    # 重新创建服务（可能有更新）
    create_systemd_service
    
    # 启动服务
    echo -e "${YELLOW}启动服务...${NC}"
    systemctl start "$SERVICE_NAME"
    
    sleep 2
    
    echo ""
    echo -e "${GREEN}=== 升级完成 ===${NC}"
    
    # 显示新版本
    if [ -x "$BINARY_PATH" ]; then
        echo -e "${BLUE}新版本信息:${NC}"
        "$BINARY_PATH" --version 2>/dev/null || echo "无法获取版本信息"
    fi
    
    press_any_key
}

# 卸载
cmd_uninstall() {
    check_root
    
    show_logo
    echo -e "${RED}=== 卸载 subs-check ===${NC}"
    echo ""
    
    read -p "确定要完全卸载吗？这将删除服务、二进制文件和配置文件 (y/n): " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        echo -e "${BLUE}卸载已取消${NC}"
        press_any_key
        return
    fi
    
    # 移除 systemd 服务
    remove_systemd_service
    
    # 删除二进制文件
    if [ -f "$BINARY_PATH" ]; then
        echo -e "${YELLOW}删除二进制文件...${NC}"
        rm -f "$BINARY_PATH"
        echo -e "${GREEN}✓ 二进制文件已删除${NC}"
    fi
    
    # 询问是否删除配置文件
    if [ -d "$(dirname "$CONFIG_PATH")" ]; then
        read -p "是否删除配置文件和数据？(y/n): " -n 1 -r
        echo
        if [[ $REPLY =~ ^[Yy]$ ]]; then
            rm -rf "$(dirname "$CONFIG_PATH")"
            echo -e "${GREEN}✓ 配置文件和数据已删除${NC}"
        else
            echo -e "${YELLOW}保留配置文件: ${CONFIG_PATH}${NC}"
        fi
    fi
    
    echo ""
    echo -e "${GREEN}=== 卸载完成 ===${NC}"
    press_any_key
}

# 启动服务
cmd_start() {
    check_root
    
    if ! systemctl is-active --quiet "$SERVICE_NAME"; then
        echo -e "${YELLOW}正在启动服务...${NC}"
        systemctl start "$SERVICE_NAME"
        sleep 1
        cmd_status
    else
        echo -e "${GREEN}服务已在运行中${NC}"
        cmd_status
    fi
    press_any_key
}

# 停止服务
cmd_stop() {
    check_root
    
    if systemctl is-active --quiet "$SERVICE_NAME"; then
        echo -e "${YELLOW}正在停止服务...${NC}"
        systemctl stop "$SERVICE_NAME"
        sleep 1
        echo -e "${GREEN}✓ 服务已停止${NC}"
    else
        echo -e "${YELLOW}服务未运行${NC}"
    fi
    press_any_key
}

# 重启服务
cmd_restart() {
    check_root
    
    echo -e "${YELLOW}正在重启服务...${NC}"
    systemctl restart "$SERVICE_NAME"
    sleep 2
    cmd_status
    press_any_key
}

# 查看状态
cmd_status() {
    if systemctl is-active --quiet "$SERVICE_NAME"; then
        status_color="${GREEN}"
        status_text="运行中"
    else
        status_color="${RED}"
        status_text="已停止"
    fi
    
    echo -e "${CYAN}=== 服务状态 ===${NC}"
    echo -e "状态: ${status_color}${status_text}${NC}"
    echo ""
    systemctl status "$SERVICE_NAME" --no-pager --lines=10
}

# 启用开机自启
cmd_enable() {
    check_root
    
    echo -e "${YELLOW}启用开机自启...${NC}"
    systemctl enable "$SERVICE_NAME"
    echo -e "${GREEN}✓ 已设置开机自启${NC}"
    press_any_key
}

# 禁用开机自启
cmd_disable() {
    check_root
    
    echo -e "${YELLOW}禁用开机自启...${NC}"
    systemctl disable "$SERVICE_NAME"
    echo -e "${GREEN}✓ 已禁用开机自启${NC}"
    press_any_key
}

# 查看日志
cmd_logs() {
    echo -e "${CYAN}=== 实时日志（Ctrl+C 退出）===${NC}"
    journalctl -u "$SERVICE_NAME" -f --no-pager
}

# 查看完整日志
cmd_logs_all() {
    echo -e "${CYAN}=== 完整日志 ===${NC}"
    journalctl -u "$SERVICE_NAME" --no-pager
}

# 编辑配置
cmd_config() {
    if [ ! -f "$CONFIG_PATH" ]; then
        echo -e "${RED}错误: 配置文件不存在${NC}"
        press_any_key
        return
    fi
    
    # 检测可用的编辑器
    if command -v nano &> /dev/null; then
        nano "$CONFIG_PATH"
    elif command -v vi &> /dev/null; then
        vi "$CONFIG_PATH"
    elif command -v vim &> /dev/null; then
        vim "$CONFIG_PATH"
    else
        echo -e "${YELLOW}未找到文本编辑器${NC}"
        echo -e "配置文件位置: ${CONFIG_PATH}"
        echo -e "请手动编辑该文件"
        press_any_key
    fi
}

# 测试配置
cmd_test() {
    if [ ! -f "$CONFIG_PATH" ]; then
        echo -e "${RED}错误: 配置文件不存在${NC}"
        press_any_key
        return
    fi
    
    echo -e "${YELLOW}测试配置文件语法...${NC}"
    
    # 使用 Python 或其他工具验证 YAML 语法
    if command -v python3 &> /dev/null; then
        python3 -c "import yaml; yaml.safe_load(open('$CONFIG_PATH'))" 2>/dev/null
        if [ $? -eq 0 ]; then
            echo -e "${GREEN}✓ 配置文件语法正确${NC}"
        else
            echo -e "${RED}✗ 配置文件语法错误${NC}"
        fi
    else
        echo -e "${YELLOW}无法验证语法（需要 python3）${NC}"
        echo -e "显示配置文件内容:"
        cat "$CONFIG_PATH"
    fi
    press_any_key
}

# 监控服务状态
cmd_monitor() {
    echo -e "${CYAN}=== 实时监控（Ctrl+C 退出）===${NC}"
    echo ""
    
    while true; do
        clear
        echo -e "${CYAN}=== Subs-Check 实时监控 ===${NC}"
        echo -e "更新时间: $(date '+%Y-%m-%d %H:%M:%S')"
        echo ""
        
        # 服务状态
        if systemctl is-active --quiet "$SERVICE_NAME"; then
            echo -e "服务状态: ${GREEN}运行中 ✓${NC}"
        else
            echo -e "服务状态: ${RED}已停止 ✗${NC}"
        fi
        
        # 内存使用
        if systemctl is-active --quiet "$SERVICE_NAME"; then
            pid=$(systemctl show -p MainPID "$SERVICE_NAME" | cut -d= -f2)
            if [ "$pid" != "0" ]; then
                mem_usage=$(ps -p "$pid" -o rss= 2>/dev/null || echo "0")
                mem_mb=$((mem_usage / 1024))
                echo -e "内存使用: ${YELLOW}${mem_mb} MB${NC}"
            fi
        fi
        
        # 开机自启状态
        if systemctl is-enabled --quiet "$SERVICE_NAME" 2>/dev/null; then
            echo -e "开机自启: ${GREEN}已启用${NC}"
        else
            echo -e "开机自启: ${YELLOW}未启用${NC}"
        fi
        
        echo ""
        echo -e "${CYAN}--- 最近日志 ---${NC}"
        journalctl -u "$SERVICE_NAME" -n 15 --no-pager --output=short
        
        sleep 5
    done
}

# 显示安装信息
cmd_info() {
    echo -e "${CYAN}=== 安装信息 ===${NC}"
    echo -e "工作目录: ${YELLOW}${SCRIPT_DIR}${NC}"
    echo -e "二进制文件: ${YELLOW}${BINARY_PATH}${NC}"
    echo -e "配置文件: ${YELLOW}${CONFIG_PATH}${NC}"
    echo -e "服务文件: ${YELLOW}${SERVICE_PATH}${NC}"
    echo ""
    
    if [ -f "$BINARY_PATH" ]; then
        echo -e "${GREEN}✓${NC} 二进制文件存在"
        if [ -x "$BINARY_PATH" ]; then
            version=$("$BINARY_PATH" --version 2>/dev/null || echo "未知版本")
            echo -e "  版本: ${version}"
        fi
    else
        echo -e "${RED}✗${NC} 二进制文件不存在"
    fi
    
    if [ -f "$CONFIG_PATH" ]; then
        echo -e "${GREEN}✓${NC} 配置文件存在"
    else
        echo -e "${RED}✗${NC} 配置文件不存在"
    fi
    
    if [ -f "$SERVICE_PATH" ]; then
        echo -e "${GREEN}✓${NC} systemd 服务已安装"
        if systemctl is-enabled --quiet "$SERVICE_NAME" 2>/dev/null; then
            echo -e "  开机自启: ${GREEN}已启用${NC}"
        else
            echo -e "  开机自启: ${YELLOW}未启用${NC}"
        fi
    else
        echo -e "${RED}✗${NC} systemd 服务未安装"
    fi
    
    echo ""
    echo -e "${CYAN}--- 访问地址 ---${NC}"
    echo -e "Web 管理: ${BLUE}http://localhost:8199/admin${NC}"
    echo -e "订阅链接: ${BLUE}http://localhost:8199/sub/all.yaml${NC}"
    press_any_key
}

# 仅安装 systemd 服务
cmd_systemd_install() {
    check_root
    
    if [ ! -f "$BINARY_PATH" ]; then
        echo -e "${RED}错误: 二进制文件不存在${NC}"
        echo -e "请先使用选项 1 进行安装${NC}"
        press_any_key
        return
    fi
    
    create_systemd_service
    echo -e "${GREEN}✓ systemd 服务安装完成${NC}"
    echo -e "使用选项 4 启动服务${NC}"
    press_any_key
}

# 仅移除 systemd 服务
cmd_systemd_remove() {
    check_root
    remove_systemd_service
    press_any_key
}

# 主函数
main() {
    # 如果有命令行参数，使用命令行模式（兼容性）
    if [ $# -gt 0 ]; then
        case "$1" in
            install) cmd_install ;;
            upgrade) cmd_upgrade ;;
            uninstall) cmd_uninstall ;;
            start) cmd_start ;;
            stop) cmd_stop ;;
            restart) cmd_restart ;;
            status) cmd_status ;;
            enable) cmd_enable ;;
            disable) cmd_disable ;;
            logs) cmd_logs ;;
            logs-all) cmd_logs_all ;;
            config) cmd_config ;;
            test) cmd_test ;;
            monitor) cmd_monitor ;;
            info) cmd_info ;;
            systemd-install) cmd_systemd_install ;;
            systemd-remove) cmd_systemd_remove ;;
            *)
                echo -e "${RED}错误: 未知命令 '$1'${NC}"
                exit 1
                ;;
        esac
        exit 0
    fi
    
    # 交互式菜单模式
    while true; do
        show_menu
        
        echo -ne "${GREEN}请选择操作 [0-17]: ${NC}"
        read choice
        
        case $choice in
            1)
                cmd_install
                ;;
            2)
                cmd_upgrade
                ;;
            3)
                cmd_uninstall
                ;;
            4)
                cmd_start
                ;;
            5)
                cmd_stop
                ;;
            6)
                cmd_restart
                ;;
            7)
                cmd_status
                press_any_key
                ;;
            8)
                cmd_enable
                ;;
            9)
                cmd_disable
                ;;
            10)
                cmd_logs
                ;;
            11)
                cmd_logs_all
                ;;
            12)
                cmd_config
                ;;
            13)
                cmd_test
                ;;
            14)
                cmd_monitor
                ;;
            15)
                cmd_info
                ;;
            16)
                cmd_systemd_install
                ;;
            17)
                cmd_systemd_remove
                ;;
            0)
                show_logo
                echo -e "${GREEN}感谢使用 subs-check 管理脚本！${NC}"
                echo ""
                exit 0
                ;;
            *)
                echo -e "${RED}无效的选择，请输入 0-17${NC}"
                sleep 2
                ;;
        esac
    done
}

# 运行主函数
main "$@"
