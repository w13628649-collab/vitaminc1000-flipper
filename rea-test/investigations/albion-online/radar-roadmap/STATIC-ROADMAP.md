# 静态客户端可以确认什么，以及地图数据功能的逆向路线

日期：2026-10-14，Asia/Singapore。用户服务器 Asia / East。
客户端 `1.32.030.345319`，IL2CPP metadata v39。本次复用已保存资料做有限静态检索，
没有启动游戏、抓取流量或创建新的 Ghidra 导入。

静态分析可以恢复数据结构、消息解析算法、对象状态更新和显示规则，也能为一个后续
数据观察工具确定接口。静态文件没有当前服务器世界中的实时实例值。客户端声明可
处理某消息，不能证明服务器本次发送了它，或发送了同地图全部对象。

## 按优先级继续追踪

| 路线 | 已确认的静态起点 | 需要追到的结果 |
| --- | --- | --- |
| 资源类别、Tier与附魔 | `harvestables.xml` 的 WOOD/ORE/FIBER/HIDE、Tier、item、RareState；`rareresourcedistribution.xml` | 数字资源类型如何关联定义；Tier与稀有状态怎样生成图标、文字和采集产物 |
| 资源创建和初始状态 | 事件 `NewSimpleHarvestableObject=38`、List=39、`NewHarvestableObject=40` | 注册实体、初始化位置、资源类型、Tier、稀有状态、采集状态的值流；不能凭字段类型直接命名 |
| 资源后续更新 | 事件 `HarvestableChangeState=46`；HarvestStart=59、Cancel=60、Finished=61 | 哪些字段改变资源余量、显示状态或耗尽状态；通知与对象ID如何关联 |
| 兽皮动物与尸体 | `mobs.xml` 的 Mob/Harvestable(type=HIDE)；事件 `NewMob=123`、MobChangeState=47 | 活动物种/位置/Tier、死亡和可剥皮对象之间的关联；活动物与静态皮资源不能合并成一类 |
| 资源字段的含义 | `HarvestableBarGui.UpdateInfo` VA `0x18142e310`；OnHarvestableChanged `0x18142dda0` | 从资源界面读取的值倒查资源模型getter；命名UI是定位线索，尚未新增反编译证据 |
| 玩家出现、移动和离开 | 既有 NewCharacter=29、Move=3、Leave=1；人物入口与实体表的REA证据 | 按区域实例与本地entity ID维护状态，识别位置、姓名、公会/联盟；保留ID与公开player ID不等同的限制 |
| 敌对关系判定 | 已恢复的 guild/alliance、FlaggingStatus和关系/颜色显示路径 | 怎样由阵营、队伍、公会/联盟和所在区规则得出当前关系；不能仅按“不是自己”判敌对 |
| 坐标与区域生命周期 | 模板/tile/compound变换、minimap bounds、MapLabel.SetPosition；区域加入/切换路径 | 各坐标空间的转换、进入区域初始化与离开清理；防止旧区域记录被显示为当前位置 |

表内编号是当前客户端静态属性确认的 **Albion业务事件号**，不是直接可套用的Photon
外层字节编号。操作与事件即使编号相同也属于不同命名空间；例如事件40不是操作
GetClusterData=40。当前资源消息的混淆类型及字段、字段token和native offset见
[resource-protocol-static-index.json](resource-protocol-static-index.json)，它是带source SHA的
派生静态索引，不是新REA `ev_*`。

## 哪些能在不运行游戏时完成

- 恢复本地资源和怪物字典、候选放置点、等级与稀有状态规则。
- 找到事件类型、解析器、注册/更新/删除处理器和内部实体模型。
- 沿资源条、提示框、模型选择与小地图位置setter确认字段含义。
- 恢复消息序列化/转换算法和坐标转换代码；需结合泛型usage、字段排序和实际原字节，
  不能把内存offset当作包offset。
- 设计读取已解析消息或离线记录的状态维护逻辑。

## 哪些还需要运行证据

- 当前黑区实际有哪些玩家、活动资源、剩余采集量与位置。
- 服务器对该客户端发送对象的半径、分区、订阅条件以及是否覆盖整张地图。
- 具体消息本次是否出现、条件字段如何省略、同步频率和丢失/重连行为。
- 实际连接的传输保护、会话选择及静态坐标与屏幕最终显示的一致性。

静态路线可以完成观察工具的数据结构与处理逻辑设计。显示实时地图仍需要一个真实
运行状态来源；只有静态输入时应展示候选布局或离线记录，不能生成当前在线世界的值。

## 当前已有与本次新增证据

人物创建、装备、姓名、生命、小地图等23个函数的成功REA Evidence已经保存，本次
直接复用 [native/REPORT.md](../native/REPORT.md)、[人物复核](../native/PLAYER-REVIEW.md)。
资源候选消息与UI入口来自当前元数据属性及方法指针表，本次没有将它们标为已经
追完的原生值流。资源定义和模板已有 [静态报告](../static-data/REPORT.md)。

下一次原生分析优先选少量资源创建/状态handler、`HarvestableBarGui.UpdateInfo`和其
资源getter，将字段关联到类型/Tier/RareState/位置；确认这一条链后再扩大。原有
Ghidra会话已关闭，旧driver队列是历史记录；复用Evidence与索引可避免重复全局分析。
