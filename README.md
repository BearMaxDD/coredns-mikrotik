# coredns-mikrotik

CoreDNS plugin 实现域名分流 + 可选 RouterOS address-list 写入。

## 功能

- **域名分流** — `domains-file` 中的域名走指定上游，其他域名走默认上游
- **geosite 支持** — 读取 V2Ray geosite.dat，按 category 匹配
- **子域名自动匹配** — `example.com` 自动匹配 `sub.example.com`
- **RouterOS 写入** — 有 `device` 配置时自动写入 address-list
- **dry-run 模式** — 只日志不写入，方便调试
- **mask 支持** — `mask4 24` → IP 自动 CIDR 聚合再写入
- **write cache** — TTL 缓存减少 RouterOS API 调用
- **失败退避** — 1s→1m 指数退避，防止连接风暴
- **timeout 续租** — 每次 DNS 命中 `set` 刷新 RouterOS timeout
- **Prometheus metrics**

## Corefile 示例

```
.:53 {
    mikrotik {
        domains-file /etc/coredns/routes.txt
        device 192.168.88.1:8728 admin mypass
        address-list4 allowed-ipv4
        mask4 24
        forward 119.29.29.29
        timeout 24h
        # dry-run  # 去掉注释即开启日志模式，不写入
    }
    forward . 119.29.29.29
}
```

## routes.txt

```
# 一行一个域名，支持注释
openai.com
chatgpt.com
api.example.com
```

## 编译

编辑 CoreDNS `plugin.cfg`，在 `cache` 前添加：

```
mikrotik:github.com/BearMaxDD/coredns-mikrotik
```

然后 `make`。

## 预编译二进制

| 平台 | 路径 |
|---|---|
| macOS arm64 | `dist/coredns` |
| Linux amd64 | `dist/coredns-linux-amd64` |

SHA256 见 `dist/*.sha256`。

## 指令参考

| 指令 | 说明 |
|---|---|
| `device <addr> <user> <pass>` | RouterOS 设备连接 |
| `domains-file <path>` | 域名文件路径 |
| `geosite <code>` | geosite category |
| `forward <address>` | 匹配域名用的上游 DNS |
| `address-list4 <name>` | IPv4 address-list 名称 |
| `mask4 <n>` | IPv4 CIDR mask |
| `timeout <duration>` | RouterOS timeout（默认 24h） |
| `comment <text>` | 写入 comment 标记 |
| `write-cache-ttl <duration>` | 写入缓存 TTL（默认 min(timeout/2, 1h)） |
| `dry-run` | 只日志不写入 |

## Metrics

| Metric | 说明 |
|---|---|
| `coredns_mikrotik_writes_total{device,list,status}` | `written`/`cache_hit`/`backoff`/`error` |
| `coredns_mikrotik_queue_dropped_total{device}` | 队列满丢弃数 |

## 开发

```bash
git clone https://github.com/BearMaxDD/coredns-mikrotik
cd coredns-mikrotik
go test ./... -race
```
