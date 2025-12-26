#!/bin/bash
echo "======= SSH 故障深度排查修复工具 ======="

# 1. 检查磁盘空间
echo "[1] 检查磁盘空间..."
if [ $(df / --output=pcent | tail -1 | tr -dc '0-9') -ge 95 ]; then
    echo "!!! 警告: 根目录已满，请清理日志或临时文件。"
fi

# 2. 检查 Shell 路径有效性
echo "[2] 检查用户 Shell..."
USER_SHELL=$(grep "^$USER:" /etc/passwd | cut -d: -f7)
if [ ! -f "$USER_SHELL" ]; then
    echo "!!! 错误: Shell $USER_SHELL 不存在，正在重置为 /bin/bash"
    sudo usermod -s /bin/bash $USER
fi

# 3. 检查 SSH 配置关键项
echo "[3] 检查 sshd_config 配置..."
# 确保没有禁用 TTY，确保会话数正常
sudo sed -i 's/^PermitTTY no/PermitTTY yes/g' /etc/ssh/sshd_config
sudo sed -i 's/^MaxSessions.*/MaxSessions 10/g' /etc/ssh/sshd_config

# 4. 检查系统伪终端 (PTY) 挂载
echo "[4] 检查 devpts 挂载情况..."
if ! mount | grep -q devpts; then
    echo "!!! 错误: devpts 未挂载，这会导致无法开启 Shell 通道。"
    sudo mount -t devpts devpts /dev/pts
fi

# 5. 检查文件权限
echo "[5] 修复关键设备权限..."
sudo chmod 666 /dev/ptmx
sudo chmod 666 /dev/tty
sudo chmod 666 /dev/null

# 6. 重启 SSH 服务
echo "[6] 重启 SSH 服务..."
sudo systemctl restart ssh || sudo service ssh restart

echo "======= 排查完成，请再次尝试 SSH 连接 ======="
