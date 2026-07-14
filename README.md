# coredns-mikrotik

CoreDNS plugin 将 DNS 解析结果 IP 自动写入 MikroTik RouterOS address-list。

## 编译

编辑 CoreDNS 的 `plugin.cfg`，在 `cache` 前添加：

```
mikrotik:github.com/netctrldns/coredns-mikrotik
```

然后 `make`。

## Corefile 配置

```corefile
*.example.org {
    mikrotik {
        device 192.168.88.1:8728 admin mypass
        address-list4 allowed-ipv4
        address-list6 allowed-ipv6
        timeout 24h
        comment "coredns-mikrotik"
    }
    cache
    forward . 8.8.8.8
}
```

### 指令说明

- `device <address> <username> <password>` — RouterOS 设备连接参数（必需）
- `address-list4 <name>` — IPv4 address-list 名称
- `address-list6 <name>` — IPv6 address-list 名称
- `timeout <duration>` — entry 超时（默认 24h），格式：`24h`、`30m`、`1h30m`
- `comment <text>` — 写入 comment 标记

### 多设备示例

```
mikrotik.internal {
    mikrotik {
        device 10.0.0.1:8728 admin pass
        address-list4 mgmt-servers
        timeout 1h
    }
    forward . 10.0.0.53
}

*.office.example.net {
    mikrotik {
        device 172.16.0.1:8728 admin pass
        address-list4 office-users
        address-list6 office-users-v6
        timeout 8h
        comment "office-coredns"
    }
    forward . 172.16.0.53
}
```

## 行为

- **异步写入** — ServeDNS 通过 ResponseWriter wrapper 捕获下游解析响应，非阻塞 enqueue 到有界 channel（cap=1024）。写 RouterOS 不阻塞 DNS 响应。
- **内存去重** — 相同 `(device,list,address)` 在 30s 窗口内跳过重复写入。过期 key 惰性清理。
- **续租** — 已有 address-list entry 自动 `set` 刷新 timeout（始终写入配置值，不依赖 RouterOS 返回的递减时间）。comment 与配置不同时才更新。
- **IPv4/IPv6 自动分路径** — IPv4 走 `/ip/firewall/address-list/`，IPv6 走 `/ipv6/firewall/address-list/`。
- **连接管理** — 每设备一个 worker goroutine 独占一个 `go-routeros` 连接。懒加载、故障重连、无心跳。
- **写失败不阻塞 DNS** — 失败记日志和 metrics，worker 关闭连接后下一任务自动重连。

## Metrics

| Metric | 说明 |
|---|---|
| `coredns_mikrotik_writes_total{device,list,status}` | 写入结果（`written` / `dedup_hit` / `error`） |
| `coredns_mikrotik_queue_dropped_total{device}` | 队列满丢弃数 |

## 生命周期

- `OnStartup` — 启动 per-device worker goroutine
- `OnShutdown` — 关闭 stop channel，worker 做 1s drain 后关闭连接
- 重启时丢失队列中未处理的 item（设计如此——不做持久化 outbox）

## 开发

```bash
git clone https://github.com/netctrldns/coredns-mikrotik
cd coredns-mikrotik
go test ./...
```

依赖：`github.com/go-routeros/routeros/v3`、CoreDNS plugin API、Prometheus client_golang。
