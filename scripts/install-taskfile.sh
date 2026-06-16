#!/usr/bin/env bash
command -v task || { [[ $(uname -m) == "x86_64" ]] && ARCH="amd64" || ARCH="arm64"; curl -L "https://gh-proxy.org/https://github.com/go-task/task/releases/download/$(curl -sL https://api.github.com/repos/go-task/task/releases/latest | grep -o '"tag_name": "[^"]*' | cut -d'"' -f4)/task_linux_${ARCH}.tar.gz" | tar -xz -C /usr/local/bin task; }
