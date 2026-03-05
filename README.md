# openwrt monitor api

- 本项目由golang+vue3+TailwindCss编写,目的是给openwrt设备提供一个更好看的网页端仪表盘和更便于调用的无鉴权系统状态HTTP API
- 本项目还使用了`ebpf`技术来实现高性能网络流量统计

> [!WARNING]  
> 本项目仍在开发中,仪表盘页面尚未足够完善,请谨慎在生产环境使用

## 仪器盘截图

![system preview](./images/image-0.png)
![network connection detail](./images/image-1.png)
![monitoring charts](./images/image-2.png)
![settings window](./images/image-3.png)

## 使用方法

1. 从[releases](https://github.com/shuiping233/openwrt-diskio-api/releases)下载最新构建产物
2. 将二进制文件和`./scripts/etc/inid.d/diskio-api`服务文件部署到openwrt设备上,推荐将二进制文件放置在`/usr/bin/`中,服务文件放置在`/etc/init.d/`中
3. 使用文本编辑器打开服务文件,修改必要的"文件路径"或"监控端口等配置
4. 给服务文件和二进制文件`chmod +x`权限,使用`/etc/init.d/diskio-api enable`使其服务开机自启,最后使用`/etc/init.d/diskio-api start`来启动服务

## 工具要求

- linux >= 5.4 (for ebpf)
- go >= 1.18
- node.js >= 20.x
- pnpm >= 8.x
- bpf2go
- ebpf tool chains (clang + llvm + gcc)

> [!WARNING]  
> 项目后端仅支持linux发行版,并只优先适配openwrt,虽然go支持windows交叉编译linux二进制产物,但是不保证能在windows上编译成功

## 编译方法

1. 在任意linux发行版上,clone本项目
2. 后端编译需要[go编译器](https://golang.google.cn/dl/)和[goreleaser](https://goreleaser.com/install/#go-install),下载和安装教程请看对应官网
3. 安装ebpf相关工具链,安装命令如下

    ```bash
    sudo apt update
    # 基础编译工具
    sudo apt install clang llvm gcc-multilib build-essential
    # eBPF 相关开发库
    sudo apt install libbpf-dev libelf-dev
    # 重要：安装内核头文件 (bpf2go 编译时需要引用)
    sudo apt install linux-headers-$(uname -r)
    # 安装代码生成工具
    go install github.com/cilium/ebpf/cmd/bpf2go@latest
    ```

4. 前端编译需要[node.js](https://nodejs.org/zh-cn/download/)和[pnpm](https://pnpm.io/zh/installation),下载和安装教程请看对应官网
5. 确保前置前置所需工具安装完毕后,在项目目录下,使用`goreleaser release --snapshot --clean`命令即可进行编译,编译产物默认在`./dist`目录下`

### 单独手动编译前端

```bash
mkdir ./dist
cd ./frontend
pnpm install
pnpm vite build --outDir ../dist/frontend  --emptyOutDir
```

### 单独手动编译后端

> [!WARNING]  
> 需要先编译前端,因为使用了embed将前端文件打包到二进制产物中,embed找不到`./dist/frontend`文件夹可能会引起编译报错

```bash
go mod tidy
go build -o openwrt-monitor-api ./backend/main.go
```
