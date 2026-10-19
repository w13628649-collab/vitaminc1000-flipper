# 静态客户端与通信解密的证据边界

2026-10-14，Asia/Singapore。客户端1.32.030.345319，IL2CPP metadata v39。
本次使用已保存符号与报告查询，没有启动游戏或获得任何实时网络样本。

不能先假定地图消息一定使用某种加密。登录HTTPS/TLS、Photon游戏传输的可选保护、
应用层字段转换及资源文件编码是不同路径；功能存在不表示某次连接启用了它。

当前静态元数据存在44个名称包含Encrypt/Decrypt等词的库方法，来自mscorlib、
System、System.Core及Mono.Security；包含AesManaged.CreateDecryptor、
RijndaelManaged.CreateDecryptor/ManagedTransform.DecryptData等。部分方法已映射
到原生VA，原始source SHA与方法token见
[crypto-static-candidates.json](crypto-static-candidates.json)。这是库实现存在的证据，
不是这些算法用于当前Photon地图数据的证据。

方法表也包含Photon3Unity3D.dll与Albion.PhotonClient.dll。当前检索未直接通过方法名
识别出它们的业务加解密调用链；名字混淆和库接口调用仍需要原生值流与引用解释。
不能由这一名称检索的阴性结果判断客户端没有网络解密实现。

静态追踪应从接收入口定位解密/完整性验证、反序列化、人物/资源实体更新，反向检查
加密启用条件、算法/参数来源和密钥建立路径。若客户端调用外部原生库或系统TLS，
可能只能在此DLL看到调用入口，实际实现属于另一组件。

找到解密函数与算法并不等于取得真实会话密钥。解密一个加密记录通常需要对应
会话的密钥和算法要求的IV/nonce、序列/计数状态或认证数据；具体需要什么取决于
已确认的算法。静态客户端文件通常不包含未来连接协商出的动态秘密。
例如采用前向保密密钥交换的连接，公开握手加客户端程序本身并不能还原会话秘密。

客户端作为合法接收端使用自己的会话状态处理收到的数据，最后形成渲染所需的
位置和对象信息。它未必保留一份完整明文包。现有NewCharacter/Move等REA证据属于
已经解析后的应用结构，尚未覆盖它们与传输解密层之间的完整调用关系。

已解码GameData的DES-CBC/GZip只针对本地静态资源文件。NewCharacter坐标路径中
的e0.a/BitConverter转换目前也不能作整个网络数据包的解密方法。

当前可以继续静态恢复算法、字段和调用链；由于没有真实会话输入与密钥材料，
本次无法演示解密实际地图消息，也不能证明服务器发送了整张地图的玩家列表。
