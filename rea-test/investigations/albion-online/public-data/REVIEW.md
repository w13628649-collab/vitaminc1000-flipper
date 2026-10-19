# 最终阅读核对记录

日期：2026-10-13，Asia/Singapore。范围为已保存报告、JSON与脚本的只读核对；没有运行测试、查询助手、游戏、抓包或NATS。

## 人物章节

[主报告](../REPORT.md)第5节已与[元数据专题](../metadata/REPORT.md)新增语义附录同步。

- NewCharacter为业务29、类型atd、66项参数；Move为3/as2、9项；Leave为1/arp、1项；装备为90/alv、10项；HealthUpdate为6/ap9、9项；HealthUpdates为7/aqa、8项。直接核对[参数schema](../metadata/results/protocol-parameter-schema.json)一致。
- Move、装备、单次/批量生命的key0来自父类avk.ddn；NewCharacter key14/15为Vector2，key16/17为byte[]。报告没有把native对象offset当成网络包字节偏移，也没有套用旧示例的key12坐标。
- key1是角色显示名来源；key8/9为公会名来源/Guid，key51/52为联盟名来源/Guid，Guid语义保留强静态推导标记。key10/13未知，key7未等同公开永久ID。名称处理可改变最终显示文本。
- NewCharacter key40与装备变更key2共用物品索引数组处理；EquipmentSlot位置和数字索引到唯一物品名仍未知。Health key2/3没有被直接命名成当前HP/最大HP。
- [player-semantic-mapping.json](../metadata/results/player-semantic-mapping.json)保存16项映射及27个引用文件SHA；初次核对时引用文件均存在，742项metadata manifest已收录该产物。主报告把这些标为AO-META静态证据，与REA函数Evidence分别标明来源。随后新增的REA语义收紧见[PLAYER-REVIEW.md](../native/PLAYER-REVIEW.md)及metadata专题；742是初次核对的历史manifest数量。
- GameInfo样例、客户端对象状态建议和实际实时采集状态有区分。没有宣称公开档案给出当前位置、全地图隐藏人物或当前全服在线名单。

## 计数、链接与状态

实际读取的manifest/目录计数与主报告一致：3278个GameData载荷（3259 XML、19词表）；1427 cluster、1336 enabled、3223 exits、106 markets；1910模板文件、427重名模板；8497语义tile；12237物品JSONL行；GetClusterMapInfo接收schema92项；公开请求70次、HTTP200为67次。enabled、放置和模板数均限定为静态定义。

主报告、四个专题及Evidence索引中的Markdown本地链接目标均存在。公开直接证据ID与sources-manifest中的ID/路径相符。

初次核对时第四次Ghidra导入仍等待、bundle records为0。随后第四次默认分析加四项禁用成功：概览1914.01秒及23条函数Evidence，bundle已导出24 records/28,998,233 bytes；关闭成功，持有进程与临时目录已清理。最终状态见[生命周期](../native/lifecycle-final.json)和[环境记录](../../../REA-ENVIRONMENT.md)。概览自身profile为composed workflow86bb5b15…，上游Ghidra与函数profile96664c5e…；seeded-noanalysis没有运行。全局170工具/skill37与隔离191工具/skill41有明确区分。

初次核对发现`SCOPE.md`的“三个专题已完成”与四个专题链接需要同步；该观察属于历史检查，最终主报告与SCOPE由调查持有者统一维护。本文件不替代其最终状态。

## 查询助手阅读修复

[fetch_public_data.py](fetch_public_data.py)存在可明确推导的失败配对问题：使用`--overwrite`后请求失败会保留旧raw、覆盖其metadata。本轮已修复为唯一的`.failure-<id>.metadata.json`，失败保留此前raw与此前配对metadata。

另已保存解析失败前收到的HTTP状态/响应头，捕获HTTPException（含IncompleteRead）与zlib.error；429要求等候超过300秒时停止自动重试并记录期限，避免把服务器期限截短后提前请求。成功配对仍需读者核对metadata SHA，文件权限/写入错误可阻止保存。行为说明已更新在[公开报告](REPORT.md)。这些是源码阅读修复，未运行验证。
