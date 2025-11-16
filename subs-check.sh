#!/bin/bash

# subs-check 服务管理脚本
# 用于在 Ubuntu 系统下创建并启动 systemd 服务

set -e

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# 获取脚本所在目录的绝对路径
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BINARY_PATH="${SCRIPT_DIR}/subs-check"
CONFIG_PATH="${SCRIPT_DIR}/config/config.yaml"
SERVICE_NAME="subs-check.service"
SERVICE_PATH="/lib/systemd/system/${SERVICE_NAME}"

echo -e "${GREEN}=== subs-check 服务安装脚本 ===${NC}"
echo "工作目录: ${SCRIPT_DIR}"
echo "二进制文件: ${BINARY_PATH}"
echo "配置文件: ${CONFIG_PATH}"
echo ""

# 检查是否为 root 用户
if [ "$EUID" -ne 0 ]; then 
    echo -e "${RED}错误: 请使用 root 权限运行此脚本${NC}"
    echo "使用方法: sudo bash subs-check.sh"
    exit 1
fi

# 检查 subs-check 二进制文件是否存在
if [ ! -f "${BINARY_PATH}" ]; then
    echo -e "${YELLOW}未检测到 subs-check 二进制文件，正在下载...${NC}"
    
    # 检查是否安装了必要的工具
    if ! command -v curl &> /dev/null && ! command -v wget &> /dev/null; then
        echo -e "${RED}错误: 需要安装 curl 或 wget 工具${NC}"
        echo "Ubuntu/Debian: sudo apt-get install curl"
        echo "CentOS/RHEL: sudo yum install curl"
        exit 1
    fi
    
    # 获取最新的 Pre-release 下载链接
    echo "正在获取最新版本信息..."
    if command -v curl &> /dev/null; then
        LATEST_URL=$(curl -s https://api.github.com/repos/55gY/subs-check/releases | grep "browser_download_url.*tar.gz" | head -n 1 | cut -d '"' -f 4)
    else
        LATEST_URL=$(wget -qO- https://api.github.com/repos/55gY/subs-check/releases | grep "browser_download_url.*tar.gz" | head -n 1 | cut -d '"' -f 4)
    fi
    
    if [ -z "${LATEST_URL}" ]; then
        echo -e "${RED}错误: 无法获取下载链接${NC}"
        echo "请手动从 https://github.com/55gY/subs-check/releases 下载"
        exit 1
    fi
    
    echo "下载地址: ${LATEST_URL}"
    
    # 下载文件
    TMP_FILE="/tmp/subs-check-latest.tar.gz"
    if command -v curl &> /dev/null; then
        curl -L -o "${TMP_FILE}" "${LATEST_URL}"
    else
        wget -O "${TMP_FILE}" "${LATEST_URL}"
    fi
    
    if [ $? -ne 0 ]; then
        echo -e "${RED}错误: 下载失败${NC}"
        exit 1
    fi
    
    # 解压文件
    echo "正在解压..."
    tar -xzf "${TMP_FILE}" -C "${SCRIPT_DIR}"
    
    if [ $? -ne 0 ]; then
        echo -e "${RED}错误: 解压失败${NC}"
        rm -f "${TMP_FILE}"
        exit 1
    fi
    
    # 清理临时文件
    rm -f "${TMP_FILE}"
    
    # 再次检查文件是否存在
    if [ ! -f "${BINARY_PATH}" ]; then
        echo -e "${RED}错误: 解压后仍未找到 subs-check 二进制文件${NC}"
        exit 1
    fi
    
    echo -e "${GREEN}✓ 下载并解压成功${NC}"
fi

# 检查配置文件是否存在
CONFIG_DOWNLOADED=false
if [ ! -f "${CONFIG_PATH}" ]; then
    echo -e "${YELLOW}未检测到配置文件，正在下载示例配置...${NC}"
    
    # 创建 config 目录
    mkdir -p "${SCRIPT_DIR}/config"
    
    # 下载配置文件
    CONFIG_URL="https://raw.githubusercontent.com/55gY/subs-check/master/config/config.example.yaml"
    
    if command -v curl &> /dev/null; then
        curl -L -o "${CONFIG_PATH}" "${CONFIG_URL}"
    else
        wget -O "${CONFIG_PATH}" "${CONFIG_URL}"
    fi
    
    if [ $? -ne 0 ]; then
        echo -e "${RED}错误: 配置文件下载失败${NC}"
        echo "请手动从 ${CONFIG_URL} 下载并保存为 ${CONFIG_PATH}"
        exit 1
    fi
    
    echo -e "${GREEN}✓ 配置文件下载成功${NC}"
    CONFIG_DOWNLOADED=true
fi

# 如果刚下载了配置文件，提示用户但继续执行
if [ "${CONFIG_DOWNLOADED}" = true ]; then
    echo -e "${YELLOW}注意: 已使用默认配置文件，建议稍后编辑: ${CONFIG_PATH}${NC}"
    echo -e "${YELLOW}继续使用默认配置启动服务...${NC}"
    echo ""
    sleep 2
fi

# 检查并设置可执行权限
echo -e "${YELLOW}检查文件权限...${NC}"
if [ ! -x "${BINARY_PATH}" ]; then
    echo "设置可执行权限..."
    chmod +x "${BINARY_PATH}"
    echo -e "${GREEN}✓ 已设置可执行权限${NC}"
else
    echo -e "${GREEN}✓ 文件已具有可执行权限${NC}"
fi

# 检查服务是否已存在
if [ -f "${SERVICE_PATH}" ]; then
    echo -e "${YELLOW}检测到服务已存在${NC}"
    echo "服务文件: ${SERVICE_PATH}"
    
    # 询问是否重新创建
    read -p "是否要重新创建服务? (y/n): " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        echo -e "${GREEN}跳过服务创建，直接启动服务...${NC}"
        
        # 重新加载 systemd 配置
        systemctl daemon-reload
        
        # 启动服务
        systemctl start ${SERVICE_NAME}
        
        # 设置开机自启
        systemctl enable ${SERVICE_NAME}
        
        # 显示服务状态
        echo ""
        echo -e "${GREEN}=== 服务状态 ===${NC}"
        systemctl status ${SERVICE_NAME} --no-pager
        
        exit 0
    fi
    
    # 停止现有服务
    echo "停止现有服务..."
    systemctl stop ${SERVICE_NAME} 2>/dev/null || true
    systemctl disable ${SERVICE_NAME} 2>/dev/null || true
fi

# 创建 systemd 服务文件
echo -e "${YELLOW}创建 systemd 服务文件...${NC}"
cat > "${SERVICE_PATH}" <<EOF
[Unit]
Description=subs-check - Subscription Proxy Checker Service
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

# 停止服务时的处理
KillMode=mixed
KillSignal=SIGTERM
TimeoutStopSec=30

[Install]
WantedBy=multi-user.target
EOF

echo -e "${GREEN}✓ 服务文件创建成功${NC}"
echo "服务文件路径: ${SERVICE_PATH}"
echo ""

# 重新加载 systemd 配置
echo -e "${YELLOW}重新加载 systemd 配置...${NC}"
systemctl daemon-reload
echo -e "${GREEN}✓ 配置已重新加载${NC}"
echo ""

# 启动服务
echo -e "${YELLOW}启动 subs-check 服务...${NC}"
systemctl start ${SERVICE_NAME}
echo -e "${GREEN}✓ 服务已启动${NC}"
echo ""

# 设置开机自启
echo -e "${YELLOW}设置开机自启动...${NC}"
systemctl enable ${SERVICE_NAME}
echo -e "${GREEN}✓ 已设置开机自启动${NC}"
echo ""

# 等待服务启动
sleep 2

# 显示服务状态
echo -e "${GREEN}=== 服务状态 ===${NC}"
systemctl status ${SERVICE_NAME} --no-pager

echo ""
echo -e "${GREEN}=== 安装完成 ===${NC}"
echo "服务已成功安装并启动在后台运行"
echo ""
echo "常用命令："
echo "  查看状态: systemctl status ${SERVICE_NAME}"
echo "  启动服务: systemctl start ${SERVICE_NAME}"
echo "  停止服务: systemctl stop ${SERVICE_NAME}"
echo "  重启服务: systemctl restart ${SERVICE_NAME}"
echo "  查看日志: journalctl -u ${SERVICE_NAME} -f"
echo "  禁用自启: systemctl disable ${SERVICE_NAME}"
echo ""
