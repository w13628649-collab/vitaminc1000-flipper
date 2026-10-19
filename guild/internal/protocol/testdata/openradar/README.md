# 真实游戏录包(取自 OpenRadar)

来自 [Nouuu/Albion-Online-OpenRadar](https://github.com/Nouuu/Albion-Online-OpenRadar)
的 `internal/photon/testdata/`,MIT License,Copyright (c) 2025 Nouuu,
许可证全文见仓库根目录 `licenses/openradar-LICENSE`。

作者用 `tools/anonymize-pcap` 洗过:MAC、IP、时间戳、角色名都换掉了(角色名是 `XXXXX`,
时间戳是 1970 年)。码位是 2026-08-31 Dragonfire 之后的,和我们从客户端
1.32.030.345319 抽的码表一致。

| 文件 | 内容 |
|---|---|
| `join-finished.pcap` | 进图完成的 Join 响应(op 2),97 个键 |
| `change-cluster.pcap` | 切区响应(op 41) |
| `move_map_change.pcap` | 584 个包的一段:走动、切区、进图 |

用它们钉住"真实的进图和切区数据能解出位置和角色",见 `../../real_capture_test.go`。
游戏打补丁之后先拿新录包替换这几个文件、跑测试,哪里漂了一目了然。
