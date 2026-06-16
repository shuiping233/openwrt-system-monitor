#!/usr/bin/env bash

set -euo pipefail   # 遇到未定义变量、命令失败、管道失败立即退出

RED='\033[0;31m'; GREEN='\033[0;32m'; NC='\033[0m'

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
INIT_SRC="${SCRIPT_DIR}/etc/init.d/diskio-api"
INIT_DST="/etc/init.d/diskio-api"

echo "正在安装 diskio-ip 服务文件到 \"$INIT_DST\""


log_info()  { printf "${GREEN}[INFO]${NC} %s\n" "$*"; }
log_error() { printf "${RED}[ERROR]${NC} %s\n" "$*" >&2; }


rollback(){
    log_error "$1"
    [[ -L $INIT_DST || -f $INIT_DST ]] && rm -f "$INIT_DST"
    exit 1
}


[[ -f $INIT_SRC ]] || rollback "源文件不存在：${INIT_SRC}"


if [[ -e $INIT_DST ]]; then
    BACKUP="${INIT_DST}.bak.$(date +%s)"
    log_info "发现旧文件，已备份到 ${BACKUP}"
    mv "$INIT_DST" "$BACKUP"
fi


ln -s "$INIT_SRC" "$INIT_DST" || rollback "创建软链接失败"


chmod +x "$INIT_SRC"
chmod +x "$INIT_DST"


log_info "正在启用服务 ..."
/etc/init.d/diskio-api enable  || rollback "enable 失败"

log_info "diskio-api 安装完成！ 请执行 /etc/init.d/diskio-api start 启动服务"
log_info "若要修改默认监听的端口和host ， 请修改 \"/etc/init.d/diskio-api\" 文件"
exit 0

# TODO 拉取github release还没做