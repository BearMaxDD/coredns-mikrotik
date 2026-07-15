# coredns-mikrotik

CoreDNS plugin 实现域名分流 + 可选 RouterOS address-list 写入。

## 功能

- **域名分流** — `domains-file` 中的域名走指定上游，其他域名走默认上游
- **geosite 支持** — 读取 V2Ray geosite.dat，按 category 匹配（`geosite cn address-list4 cn-ipv4`）
- **子域名自动匹配** — `example.com` 自动匹配 `sub.example.com`，不匹配 `badexample.com`
- **RouterOS 写入** — 有 `device` 配置时自动写入 address-list
- **dry-run 模式** — 只日志不写入，方便调试
- **mask 支持** — `mask4 24` → IP 自动 CIDR 聚合再写入
- **timeout 续租** — 每次 DNS 命中 `set` 刷新 RouterOS timeout
- **文件 reload** — 域名文件定时重载（±30% jitter），失败保留旧列表
- **内存去重** — 30s 窗口避免重复写入
- **Prometheus metrics** — `coredns_mikrotik_writes_total`, `queue_dropped_total`

## Corefile 示例

```
. {
    mikrotik {
        device 192.168.88.1:8728 admin mypass
        timeout 24h
        forward 8.8.8.8
        address-list4 default-ipv4
        mask4 24

        # Route 1: domain list
        domains-file /etc/coredns/routes.txt
        reload 5m

        # Route 2: geosite (flat syntax)
        geosite cn address-list4 cn-ipv4 mask4 24

        # Route 3: another geosite
        geosite google address-list4 google-ipv4 mask4 32

        # dry-run 模式：只日志不写入
        # dry-run
    }
    cache
    forward . 1.1.1.1
}
```

## routes.txt

```
# 一行一个域名，支持注释
example.org
google.com
internal.corp.com
```

## 编译

编辑 CoreDNS `plugin.cfg`，在 `cache` 前添加：

```
mikrotik:github.com/netctrldns/coredns-mikrotik
```

然后 `make`。

## 指令参考

| 指令 | 说明 |
|---|---|
| `device <addr> <user> <pass>` | RouterOS 设备连接（可选，不配则不写入） |
| `domains-file <path>` | 域名文件路径 |
| `geosite <code>` | geosite category，后跟 `address-list4`/`mask4`/`address-list6`/`mask6` |
| `forward <address>` | 匹配域名用的上游 DNS |
| `address-list4 <name>` | IPv4 address-list 名称 |
| `address-list6 <name>` | IPv6 address-list 名称 |
| `mask4 <n>` | IPv4 CIDR mask（0-32，0 不 mask） |
| `mask6 <n>` | IPv6 CIDR mask（0-128） |
| `timeout <duration>` | RouterOS timeout（默认 24h） |
| `comment <text>` | 写入 comment 标记 |
| `reload <duration>` | 域名文件重载间隔 |
| `dry-run` | 只日志不写入 |

## geosite 域名类型

| 类型 | 匹配规则 |
|---|---|
| `Full` | 精确匹配，`exact.test.com` 只匹配自身 |
| `RootDomain` | 域名+子域名，`.cn` 匹配 `a.b.cn` |
| `Plain` | 关键词子串，`google` 匹配 `googleapis.com` |
| `Regex` | **不支持**，加载时报错 |

## Metrics

| Metric | 说明 |
|---|---|
| `coredns_mikrotik_writes_total{device,list,status}` | `written`/`cache_hit`/`backoff`/`error` |
| `coredns_mikrotik_queue_dropped_total{device}` | 队列满丢弃数 |

## 生命周期

- `OnStartup` — 启动 per-device worker
- `OnShutdown` — Close 连接 + matcher

## 冒烟测试

```bash
cd ~/Code/coredns-mikrotik
go test ./...  # 52+ tests, all pass
```

编译进 CoreDNS 后可用 `dry-run` 模式验证分流效果。
