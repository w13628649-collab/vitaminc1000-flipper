# 跨报告事实复核

复核日期：2026-10-13，Asia/Singapore。只读检查 `static-data/REPORT.md`、`public-data/REPORT.md`、`SCOPE.md`、`EVIDENCE-INDEX.md`、`query_catalog.py`、`fetch_public_data.py` 和既有 summary。没有执行测试、重新提取大数据、发起新接口请求或修改主体报告。

## 主体报告必须保留的解释

| 主题 | 可作的结论 | 必须同时保留的边界 |
| --- | --- | --- |
| 静态文件全量 | 3,278 个本地 GameData 容器全部解码成功；3,259 XML、19 词表，输入/输出 hash 已保存 | 这不表示 `.assets/.resS` 全部图片/几何已经解包，也不表示服务器实时数据全部取得 |
| Asia 世界 | 本地共用 world 1,427 定义、1,336 enabled，当前 `worldsettings_asia` 已保存 | 包含测试/岛屿/动态候选；不是当前 Asia 开放实例清单；2023 `world_asia/patch` 不适用于2026安装，未应用 |
| 资源点/人物坐标 | 静态布局、模板、tile、出口坐标和动态字段处理线索可定位 | 模板有427名字存在变体，`dyn`/active layer/compound变换未完全确定；1,888,023是原始模板放置计数而非当前实体；30点示例只是候选 |
| 图像与路径 | 2,930地图 Texture2D流式范围有效，图层名/bounds已定位；高度碰撞Base64载荷已保留 | 没有PNG导出/Mesh还原/地图渲染；5,109 `paths`是prefab资源路径，不能称导航图；`roads`道路图也不等于通行网格 |
| 物品和市场 | 12,237字符串物品ID及静态市场对象/cluster可以枚举 | `dumper_index`网络数值映射未验证；附魔与品质独立；城市ID/市场ID/市场UUID不可混用；106市场定义不等于公开API全支持 |
| REST价格 | East的公开报价/历史样例HTTP200，具体字段已见 | `0`+year0001表示缺测，占位不证明免费/市场无订单；min/max是项目聚合报价范围，不是全深度、成交记录或保证的实时快照 |
| 历史 | 保存 item_count/avg_price/timestamp 聚合 | 请求日期与返回日期边界不同，按服务聚合语义保留原值；事件/成交统计非当前盘口 |
| 订单深度 | 源码有orderbook候选，公开NATS/备份路线有文档 | East orderbook实测404；NATS未连接、备份未下载/恢复；未来仅覆盖贡献者提交/项目保存的数据，不能承诺权威全订单 |
| 价格单位 | Wire/ingest原始放大值与deduped/REST已换算值可由源码区别 | 不对所有层再统一除10000；部署与输入样本仍需核对，保留raw/unit/topic/server |
| 公开人物 | Asia GameInfo可返回档案、公会/联盟、击杀死亡事件 | 样例无当前XY/zone/online；装备null不是裸装，事件装备仅事件时刻；搜索/事件分页非全玩家枚举，limit支持不一致 |
| 客户端人物事件 | 当前元数据/REA可以证明客户端定义的状态和处理路径 | 没有当前运行/记录样例，未证明实际兴趣半径或每帧同步；实体局部ID不等于永久player ID；未发送的角色/全球坐标没有获取证据 |

三个“全量”应明确：**本地携带的静态定义全量、社区保存/观察的数据全量、服务器权威实时全量**。目前完成的是第一类的 GameData 部分，并为图形资源建立库存；后二者提供实际可行路线、样例和缺口。不能把“完整报告”写成“所有目标数据已经取得”。

## 一致性检查结果

1. static/public 都引用本地版本 `1.32.030.345319`，world/market/物品/template计数一致。
2. static/public 都保留2023 Asia文件陈旧问题；没有将旧Asia1262/863推广为当前值。
3. EVIDENCE-INDEX 正确区分 `ev_*` REA、`AO-META-*`脚本、公开 `PUB-*`与本地容器/资源证据。主体需要沿用这些标签，避免把整个报告的所有发现统一称REA反编译。
4. SCOPE中的“正在首次Ghidra分析/正在保存样例”是进度状态，结束前应由主体按实际最终结果更新；不要让它与最终报告不一致。
5. public查询助手只支持prices/history/player/search；没有实现订单、NATS订阅、批量爬取、GUI或当前人物图层。
6. `query_catalog.py item` 的实现对整条 JSON 文本做不区分大小写匹配。它不仅搜索item ID/名称，还可能因description/属性内容命中；主体应称“目录文字搜索”，不能当严格精确 ID查询。`cluster`是保留前导零的精确字符串查询。
7. `fetch_public_data.py`未运行测试，也未负责已有样例采集；HTTP200证据来自已保存研究样例，不能说这份新助手已验证在线成功。

## 可直接使用的命令

从 `C:/Users/ludy/Documents/code/rea-test` 运行。以下是已存在脚本的入口说明，复核期间没有执行它们。

```powershell
# 已保存静态区域记录（精确ID）
python .\investigations\albion-online\query_catalog.py cluster 0217

# 物品目录文字匹配；附魔仍是item_id中的@N
python .\investigations\albion-online\query_catalog.py item T4_BAG --limit 5

# 已保存城市/市场映射
python .\investigations\albion-online\query_catalog.py markets

# 单次East报价；文件已存在时选新的输出名
python .\investigations\albion-online\public-data\fetch_public_data.py prices --server east --items 'T4_BAG,T4_BAG@1' --locations '3005,2004' --qualities '1,2,3,4,5' --output '.\investigations\albion-online\public-data\manual\review-bag-prices.json'

# 单次East历史；保留响应日期边界
python .\investigations\albion-online\public-data\fetch_public_data.py history --server east --items 'T4_BAG' --locations '3005' --qualities '1' --date '2026-10-01' --end-date '2026-10-12' --time-scale 24 --output '.\investigations\albion-online\public-data\manual\review-bag-history.json'

# Asia公开人物档案（永久ID来自公开search）
python .\investigations\albion-online\public-data\fetch_public_data.py player --server east --player-id 'wpDh5rpcTXyKkmDpnm7PNg' --output '.\investigations\albion-online\public-data\manual\review-player.json'
```

价格助手保存 raw JSON 和 `.metadata.json`，默认拒绝覆盖；每次尝试两秒间隔、429退避、最多4次，整体批次必须串行。`0007`是本地Thetford市场cluster；公开AODP清单把其规范为7。Caerleon普通市场用3005、Black Market用3003，不可颠倒。

数据重做命令见同目录 REPORT.md；已有 `decoded/`时不带`--overwrite`将正常拒绝覆盖。优先使用现成catalog helper，没必要为每次目录查询重新解码391MB XML或重建1.5GB调查产物。
