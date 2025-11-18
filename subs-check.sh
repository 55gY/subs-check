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
NC='\033[0m' # No Color

# 配置变量
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BINARY_PATH="${SCRIPT_DIR}/subs-check"
CONFIG_PATH="${SCRIPT_DIR}/config/config.yaml"
SERVICE_NAME="subs-check.service"
SERVICE_PATH="/lib/systemd/system/${SERVICE_NAME}"
GITHUB_REPO="55gY/subs-check"
 
# 显示 Logo
show_logo() {
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

# 显示帮助信息
show_help() {
    cat << EOF
${GREEN}使用方法:${NC}
    $(basename $0) [命令]

${GREEN}可用命令:${NC}
    ${YELLOW}install${NC}         安装 subs-check（下载二进制、配置文件、创建服务）
    ${YELLOW}upgrade${NC}         升级到最新版本（保留配置文件）
    ${YELLOW}uninstall${NC}       完全卸载（删除服务、二进制和配置文件）
    
    ${YELLOW}start${NC}           启动服务
    ${YELLOW}stop${NC}            停止服务
    ${YELLOW}restart${NC}         重启服务
    ${YELLOW}status${NC}          查看服务状态
    
    ${YELLOW}enable${NC}          启用开机自启
    ${YELLOW}disable${NC}         禁用开机自启
    
    ${YELLOW}logs${NC}            查看实时日志
    ${YELLOW}logs-all${NC}        查看完整日志
    
    ${YELLOW}config${NC}          编辑配置文件
    ${YELLOW}test${NC}            测试配置文件语法
    
    ${YELLOW}monitor${NC}         监控服务状态（实时刷新）
    ${YELLOW}info${NC}            显示安装信息
    
    ${YELLOW}systemd-install${NC} 仅安装 systemd 服务
    ${YELLOW}systemd-remove${NC}  仅移除 systemd 服务
    
    ${YELLOW}help${NC}            显示此帮助信息

${GREEN}示例:${NC}
    sudo $(basename $0) install      # 全新安装
    sudo $(basename $0) upgrade      # 升级到最新版
    sudo $(basename $0) status       # 查看服务状态
    $(basename $0) logs              # 查看日志（无需 root）
    sudo $(basename $0) uninstall    # 完全卸载

EOF
}

# 检查 root 权限（某些操作需要）
check_root() {
    if [ "$EUID" -ne 0 ]; then 
        echo -e "${RED}错误: 此操作需要 root 权限${NC}"
        echo -e "请使用: ${YELLOW}sudo $(basename $0) $1${NC}"
        exit 1
    fi
}

# 检查必要工具
check_dependencies() {
    local missing_deps=()
    
    if ! command -v curl &> /dev/null && ! command -v wget &> /dev/null; then
        missing_deps+=("curl 或 wget")
    fi
    
    if ! command -v tar &> /dev/null; then
        missing_deps+=("tar")
    fi
    
    if [ ${#missing_deps[@]} -gt 0 ]; then
        echo -e "${RED}错误: 缺少必要工具: ${missing_deps[*]}${NC}"
        echo ""
        echo "安装方法:"
        echo "  Ubuntu/Debian: sudo apt-get install curl tar"
        echo "  CentOS/RHEL:   sudo yum install curl tar"
        exit 1
    fi
}

# 获取最新版本下载链接
get_latest_release_url() {
    echo -e "${BLUE}正在获取最新版本信息...${NC}"
    
    local api_url="https://api.github.com/repos/${GITHUB_REPO}/releases"
    local download_url
    
    if command -v curl &> /dev/null; then
        download_url=$(curl -s "$api_url" | grep "browser_download_url.*tar.gz" | head -n 1 | cut -d '"' -f 4)
    else
        download_url=$(wget -qO- "$api_url" | grep "browser_download_url.*tar.gz" | head -n 1 | cut -d '"' -f 4)
    fi
    
    if [ -z "$download_url" ]; then
        echo -e "${RED}错误: 无法获取下载链接${NC}"
        echo "请手动访问: https://github.com/${GITHUB_REPO}/releases"
        exit 1
    fi
    
    echo "$download_url"
}

# 下载并安装二进制文件
download_binary() {
    local download_url="$1"
    local tmp_file="/tmp/subs-check-latest.tar.gz"
    
    echo -e "${BLUE}下载地址: ${download_url}${NC}"
    echo -e "${YELLOW}正在下载...${NC}"
    
    if command -v curl &> /dev/null; then
        curl -L --progress-bar -o "$tmp_file" "$download_url"
    else
        wget --show-progress -O "$tmp_file" "$download_url"
    fi
    
    if [ $? -ne 0 ]; then
        echo -e "${RED}错误: 下载失败${NC}"
        rm -f "$tmp_file"
        exit 1
    fi
    
    echo -e "${YELLOW}正在解压...${NC}"
    tar -xzf "$tmp_file" -C "$SCRIPT_DIR"
    
    if [ $? -ne 0 ]; then
        echo -e "${RED}错误: 解压失败${NC}"
        rm -f "$tmp_file"
        exit 1
    fi
    
    rm -f "$tmp_file"
    chmod +x "$BINARY_PATH"
    
    echo -e "${GREEN}✓ 二进制文件安装成功${NC}"
}

# 下载配置文件
download_config() {
    local config_url="https://raw.githubusercontent.com/${GITHUB_REPO}/master/config/config.example.yaml"
    
    echo -e "${YELLOW}正在下载示例配置文件...${NC}"
    
    mkdir -p "$(dirname "$CONFIG_PATH")"
    
    if command -v curl &> /dev/null; then
        curl -L -o "$CONFIG_PATH" "$config_url"
    else
        wget -O "$CONFIG_PATH" "$config_url"
    fi
    
    if [ $? -ne 0 ]; then
        echo -e "${RED}错误: 配置文件下载失败${NC}"
        echo "请手动从以下地址下载: $config_url"
        exit 1
    fi
    
    echo -e "${GREEN}✓ 配置文件下载成功${NC}"
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
            exit 0
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
    echo ""
    cmd_info
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
        echo -e "请先运行: ${YELLOW}sudo $(basename $0) install${NC}"
        exit 1
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
    echo ""
    
    # 显示新版本
    if [ -x "$BINARY_PATH" ]; then
        echo -e "${BLUE}新版本信息:${NC}"
        "$BINARY_PATH" --version 2>/dev/null || echo "无法获取版本信息"
    fi
    
    echo ""
    cmd_status
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
        exit 0
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
}

# 重启服务
cmd_restart() {
    check_root
    
    echo -e "${YELLOW}正在重启服务...${NC}"
    systemctl restart "$SERVICE_NAME"
    sleep 2
    cmd_status
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
}

# 禁用开机自启
cmd_disable() {
    check_root
    
    echo -e "${YELLOW}禁用开机自启...${NC}"
    systemctl disable "$SERVICE_NAME"
    echo -e "${GREEN}✓ 已禁用开机自启${NC}"
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
        exit 1
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
    fi
}

# 测试配置
cmd_test() {
    if [ ! -f "$CONFIG_PATH" ]; then
        echo -e "${RED}错误: 配置文件不存在${NC}"
        exit 1
    fi
    
    echo -e "${YELLOW}测试配置文件语法...${NC}"
    
    # 使用 Python 或其他工具验证 YAML 语法
    if command -v python3 &> /dev/null; then
        python3 -c "import yaml; yaml.safe_load(open('$CONFIG_PATH'))" 2>/dev/null
        if [ $? -eq 0 ]; then
            echo -e "${GREEN}✓ 配置文件语法正确${NC}"
        else
            echo -e "${RED}✗ 配置文件语法错误${NC}"
            exit 1
        fi
    else
        echo -e "${YELLOW}无法验证语法（需要 python3）${NC}"
        echo -e "显示配置文件内容:"
        cat "$CONFIG_PATH"
    fi
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
    echo ""
    
    echo -e "${CYAN}--- 常用命令 ---${NC}"
    echo -e "  查看状态: ${YELLOW}sudo $(basename $0) status${NC}"
    echo -e "  查看日志: ${YELLOW}$(basename $0) logs${NC}"
    echo -e "  重启服务: ${YELLOW}sudo $(basename $0) restart${NC}"
    echo -e "  编辑配置: ${YELLOW}$(basename $0) config${NC}"
}

# 仅安装 systemd 服务
cmd_systemd_install() {
    check_root
    
    if [ ! -f "$BINARY_PATH" ]; then
        echo -e "${RED}错误: 二进制文件不存在${NC}"
        echo -e "请先运行: ${YELLOW}sudo $(basename $0) install${NC}"
        exit 1
    fi
    
    create_systemd_service
    echo -e "${GREEN}✓ systemd 服务安装完成${NC}"
    echo -e "启动服务: ${YELLOW}sudo $(basename $0) start${NC}"
}

# 仅移除 systemd 服务
cmd_systemd_remove() {
    check_root
    remove_systemd_service
}

# 主函数
main() {
    # 如果没有参数，显示帮助
    if [ $# -eq 0 ]; then
        show_logo
        show_help
        exit 0
    fi
    
    # 解析命令
    case "$1" in
        install)
            cmd_install
            ;;
        upgrade)
            cmd_upgrade
            ;;
        uninstall)
            cmd_uninstall
            ;;
        start)
            cmd_start
            ;;
        stop)
            cmd_stop
            ;;
        restart)
            cmd_restart
            ;;
        status)
            cmd_status
            ;;
        enable)
            cmd_enable
            ;;
        disable)
            cmd_disable
            ;;
        logs)
            cmd_logs
            ;;
        logs-all)
            cmd_logs_all
            ;;
        config)
            cmd_config
            ;;
        test)
            cmd_test
            ;;
        monitor)
            cmd_monitor
            ;;
        info)
            cmd_info
            ;;
        systemd-install)
            cmd_systemd_install
            ;;
        systemd-remove)
            cmd_systemd_remove
            ;;
        help|--help|-h)
            show_logo
            show_help
            ;;
        *)
            echo -e "${RED}错误: 未知命令 '$1'${NC}"
            echo ""
            show_help
            exit 1
            ;;
    esac
}

# 运行主函数
main "$@"
