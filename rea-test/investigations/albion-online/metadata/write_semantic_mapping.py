"""Save bounded player field value-flow and UI semantic observations.

Read-only target inputs. Findings are version-specific static analysis, not
runtime or packet observations. Evidence IDs are local AO-META identifiers.
"""
import hashlib
import json
import struct
from protocol_attributes import field_layout
from generic_usages import m, pe, typename, usage
from parse_metadata import OUT, emit


def main():
    schema = json.loads((OUT / 'protocol-parameter-schema.json').read_text(encoding='utf8'))
    symbols = json.loads((OUT / 'all-method-symbols.json').read_text(encoding='utf8'))['symbols']

    def event_field(code, key):
        item = next(t for t in schema['selected_schemas'] if t['kind'] == 'event' and t['code'] == code)
        fld = next(f for f in item['parameters'] if f['parameter_key'] == key)
        return dict(event_code=code, event_names=item['code_names'], event_type=item['type'], **fld)

    refs = {}

    def evidence(*names):
        ids = []
        for name in names:
            filename = name + '.json'
            path = OUT / filename
            data = path.read_bytes()
            obj = json.loads(data)
            eid = obj['evidence_id']
            ids.append(eid)
            refs[eid] = dict(path='results/' + filename, sha256=hashlib.sha256(data).hexdigest())
        return ids

    mappings = []

    def add(code, key, semantic, status, flow, names, unknown):
        mappings.append(dict(**event_field(code, key), semantic=semantic,
                             conclusion_status=status, observed_value_flow=flow,
                             evidence_ids=evidence(*names), unresolved=unknown))

    add(29, 0, '客户端本地实体/对象 ID', '静态值流确认', [
        'cjb.d 0x180aacaf0 按 atd.cvx（0x10）调用 ciy.a(long) 0x18096f090 查询本地实体；不存在时创建 cp0 并加入实体表。'
    ], ['disassembly-180aacaf0-adjacent', 'disassembly-18096f090-adjacent'], [
        '跨地图和重连的 ID 生命周期未观察。', '不能与公开 GameInfo 永久角色 ID 直接等同。'])
    add(29, 1, '角色显示名来源（原始字符串）', '静态值流及 UI 用途确认', [
        'cjb.d 0x180aacaf0 将 atd.cvy（0x18）传给 cp0..ctor 0x180bd6ea0。',
        'cp0..ctor 调用 cp0.af(string) 0x180bd9010；原串存 cp0.ajf（0x358），名称处理结果存 cp0.ajg（0x360）。',
        'cp0.aeo 0x1805b9a90 读取 0x360；CharacterName.UpdateText 0x1814afa00 经 vtable slot24/offset0x2b8 取名并传给 UILabel.set_text 0x1819426a0。'
    ], ['disassembly-180aacaf0-adjacent', 'disassembly-180bd6ea0-adjacent', 'disassembly-180bd9010-adjacent',
        'leaf-1805b9a90', 'disassembly-1814afa00-adjacent'], [
        '显示层可处理本地化或特殊 placeholder；字段值不保证等于最终显示文本。', '实际包中 Photon 字符串的物理编码未逐项验证。'])
    add(29, 7, '角色对象 Guid 值；永久身份映射未知', '静态值流确认，身份范围未知', [
        'cp0..ctor 0x180bd6ea0 将 Guid 传给 cpx..ctor 0x180bc8ae0，存 cpx.ahr（0x1f0）。',
        'csc.v 0x180becdc0 尾部将 atd.cv4（0x40）再次赋值给 cpx.ahr（0x1f0）。'
    ], ['disassembly-180bd6ea0-adjacent', 'disassembly-180bc8ae0-adjacent', 'disassembly-180becdc0-adjacent'], [
        '是否等于 GameInfo/player 永久角色 ID 未追到。', 'Guid 字节序、压缩和序列化物理编码未验证。'])
    guild_shared = ['disassembly-180becdc0-adjacent', 'disassembly-1814a55e0-adjacent', 'disassembly-1814a72f0-adjacent']
    add(29, 8, '公会名来源（原始字符串）', '静态值流及 UI 关联确认', [
        'csc.v 指令0x180bee55a 读 atd.cv5（0x50），经 slot76/offset0x5f8 调 cp0.aer 0x180bd8db0。',
        '原串存 cp0.ai9（0x328）；名称处理后经 slot78/offset0x618 的 cp0.aet 0x1805b8540 存 cp0.ai6（0x308）。',
        'cp0.aes 0x1805b9b70（slot77/offset0x608）读0x308；CharacterName.UpdateText 指令0x1814afde2/0x1814afe23 取其作为公会显示文本。'
    ], ['disassembly-180becdc0-adjacent', 'disassembly-180bd8db0-adjacent', 'leaf-1805b8540',
        'leaf-1805b9b70', 'disassembly-1814afa00-adjacent'], ['名称处理可改变渲染文本。'])
    add(29, 9, '公会 Guid', '强静态语义推导（值流与命名 UI 枚举一致）', [
        'csc.v 指令0x180bee41f 读 atd.cv6（0x58），经 slot74/offset0x5d8 调 cp0.aep 0x180bd8da0 存 cp0.ai8（0x318）。',
        'getter cp0.gy 0x180bda8e0（slot73/offset0x5c8）读取0x318。',
        'CharacterName.CheckGuildAndAllianceStandingState 0x1814a55e0 比较该 Guid 非空且双方相等时返回 standing6。',
        'GetColorSettingForStanding 0x1814a72f0 的跳表把 standing6 指向0x1814a7480，随后 context9；命名枚举 CharacterNameSettingContext.OtherGuild=9。'
    ], guild_shared + ['leaf-180bd8da0', 'leaf-180bda8e0'], ['未以当前服务器实际数据包验证；不能据此取得服务器未发送的公会信息。'])
    add(29, 51, '联盟名来源（原始字符串）', '静态值流及 UI 关联确认', [
        'csc.v 指令0x180bee69c 读 atd.cxc（0x158），经 slot84/offset0x678 调 cp0.aey 0x180bd8ee0。',
        '原串存 cp0.ajd（0x348）；名称处理后经 slot86 的 cp0.ae0 存 cp0.ai7（0x310）。',
        'cp0.aez 0x1805b9b80（slot85/offset0x688）读0x310；CharacterName.UpdateText 指令0x1814afe0a 取其与公会名组合。'
    ], ['disassembly-180becdc0-adjacent', 'disassembly-180bd8ee0-adjacent', 'leaf-1805b9b80',
        'disassembly-1814afa00-adjacent'], ['名称处理可改变渲染文本。'])
    add(29, 52, '联盟 Guid', '强静态语义推导（值流与命名 UI 枚举一致）', [
        'csc.v 指令0x180bee666 读 atd.cxd（0x160），经 slot82/offset0x658 调 cp0.aew 0x180bd8ed0 存 cp0.ajc（0x338）。',
        'getter cp0.gz 0x180bda8f0（slot81/offset0x648）读取0x338。',
        'CheckGuildAndAllianceStandingState 0x1814a55e0 的第二 Guid 比较非空且双方相等时返回 standing5。',
        'GetColorSettingForStanding 跳表把 standing5 指向0x1814a746c，随后 context10；命名枚举 CharacterNameSettingContext.OtherAlliance=10。'
    ], guild_shared + ['leaf-180bd8ed0', 'leaf-180bda8f0'], ['未以当前服务器实际数据包验证；联盟的实时成员范围未观察。'])
    add(29, 10, '未知字符串；不是本轮确认的联盟名', '仅确认值流', [
        'csc.v 指令0x180beddb4 读 atd.cv7（0x68）并存 cpx.aht（0x208）。'
    ], ['disassembly-180becdc0-adjacent'], ['具体业务意义未知。'])
    add(29, 13, '未知字符串', '仅确认值流', [
        'csc.v 指令0x180bedeef 读 atd.cwa（0x78）并存 cpx.ahw（0x218）。'
    ], ['disassembly-180becdc0-adjacent'], ['具体业务意义未知。'])
    equipment_names = ['disassembly-180becdc0-adjacent', 'disassembly-180bd9770-adjacent', 'disassembly-180be90e0-adjacent']
    add(29, 40, '装备物品索引数组', '静态值流与装备变更事件共用处理确认', [
        'csc.v 指令0x180bedf62 读 atd.cw1（0x118）并调用 cp0.af(int[]) 0x180bd9770。',
        'cp0.af 逐元素经 bdh.abo(int) 0x180609120 查询物品表，负值清空对应位置；查到的 bc9 对象存 cp0.agy:bc9[]（0x2c8）。',
        'CharacterEquipmentChanged=90 handler crr.aft 0x180be90e0 从 alv.bw4（key2/0x20）读取 int[]，调用同一 cp0.af。'
    ], equipment_names, ['数组 index 到命名 EquipmentSlot 的完整映射未证明。', 'int 索引如何对应公开物品 ID/客户端 unique name 需要另核对物品表。'])
    add(90, 2, '装备物品索引数组，与 NewCharacter key40 同类', '静态值流确认', [
        'crr.aft 0x180be90e0 使用 ci9.b<alv>，把 alv.bw4（0x20）传给 cp0.af(int[]) 0x180bd9770。'
    ], equipment_names, ['具体装备槽位置和字段的条件省略规则未实测。'])
    add(29, 43, 'bil 对象索引数组；外观/skin 候选', '值流确认，具体语义未知', [
        'csc.v 指令0x180bee038 读 atd.cw4（0x130）并调用 cp0.x8(int[]) 0x180bdab90。',
        'cp0.x8 经另一表 be7.abp(int) 0x18064db20 查询 bil，结果存 cp0.agz:bil[]（0x2d0），负值清空。'
    ], ['disassembly-180becdc0-adjacent', 'disassembly-180bdab90-adjacent'], ['不能仅凭 bil 与 bc9 类型差异确认 skin；数组槽语义未知。'])
    add(29, 19, '未知 float', '仅确认值流', [
        'csc.v 从 atd.cwg（0xa8）经 virtual slot18/offset0x258 调 cpo.ad9(float) 0x180bbf0d0；存实体0x40及simulationObject0x68。'
    ], ['disassembly-180becdc0-adjacent', 'disassembly-180bbf0d0-adjacent'], ['未证实 HP、速度或其它业务名称。'])
    add(29, 20, '未知 float', '仅确认值流', [
        'csc.v 从 atd.cwh（0xac）存 cp0.agx（0x2c4）。'
    ], ['disassembly-180becdc0-adjacent'], ['未证实业务含义。'])
    health_names = ['disassembly-180bc2cb0-adjacent', 'disassembly-1808f16b0-adjacent',
                    'disassembly-180bc5aa0-adjacent', 'disassembly-1814ae840-adjacent']
    for key, fn, offset, dest in [(2, 'ch6', '0x20', '0x10'), (3, 'ch7', '0x24', '0x18')]:
        add(6, key, '生命值更新状态 float；当前HP/最大HP具体语义未确认', '生命值处理链确认，标量业务意义未知', [
            f'cpo.aq7 0x180bc2cb0 使用 ci9.b<ap9>，把 ap9.{fn}（{offset}）传给 a46..ctor 0x1808f16b0，存 a46+{dest}。',
            'a46 状态经 virtual slot46/offset0x418 调 cpo.x8(a46) 0x180bc5aa0；再调用 a44<a46> 状态模型应用函数0x1828f2590，实体字段0x88。',
            'CharacterName.UpdateSliders 0x1814ae840 从实体0x88状态经 a44<a46>.y 0x1828f2e80 取得 float，分母包含 entity+0xa4 和状态对象+0x50；再更新 fillAmount。'
        ], health_names, ['尚未还原 a44<a46> 状态更新、时间插值和最大值变化的完整计算。',
                         '不能直接命名 key2=当前HP、key3=最大HP；也未确认单次伤害或增量含义。'])

    selected_layouts = [field_layout(t['index']) for t in m.types
                        if t['fullname'] in ['cp0', 'cpx', 'cpo', 'a46', 'a44']]
    vtable = []
    for name, slots in [('cp0', [24, 46, 73, 74, 76, 77, 78, 79, 81, 82, 84, 85, 86]), ('cpo', [18, 46])]:
        ty = next(t for t in m.types if t['fullname'] == name)
        for slot in slots:
            raw = m.sections['vtableMethods']['offset'] + (ty['vtableStart'] + slot) * 4
            encoded = struct.unpack_from('<I', m.data, raw)[0]
            kind, idx = encoded >> 29 & 7, (encoded & 0x1fffffff) >> 1
            row = dict(type=name, type_index=ty['index'], slot=slot,
                       virtual_method_offset=hex(0x138 + slot * 16), metadata_file_offset=raw,
                       raw_hex=m.data[raw:raw+4].hex(), encoded=hex(encoded), kind=kind, index=idx)
            if kind == 3:
                me = m.methods[idx]
                row['method_definition'] = dict(index=idx, image=m.types[me['declaringType']]['image'],
                                                type=m.types[me['declaringType']]['fullname'], name=me['name'],
                                                slot=me['slot'], metadata_file_offset=me['metadata_file_offset'])
                row['native_symbols'] = [s for s in symbols if s['method_index'] == idx]
            vtable.append(row)

    table_va = 0x1814a7604
    table_raw = pe.va_to_raw(table_va)
    table_bytes = pe.data[table_raw:table_raw+44]
    jump_entries = [dict(standing=i+1, entry_file_offset=table_raw+i*4, rva=hex(rva),
                         target_va=hex(pe.imagebase+rva)) for i, rva in enumerate(struct.unpack('<11I', table_bytes))]
    const = json.loads((OUT / 'all-field-constants.json').read_text(encoding='utf8'))
    guild_constants = [c for c in const['constants']
                       if c['type'] == 'CharacterNameSettingContext' and c['name'] in ['OtherGuild', 'OtherAlliance']]
    usages = []
    for file in ['disassembly-180bc5aa0-adjacent.json', 'disassembly-1814ae840-adjacent.json']:
        obj = json.loads((OUT/file).read_text(encoding='utf8'))
        import re
        for ins in obj['instructions']:
            mat = re.search(r'\[rip \+ (0x[0-9a-f]+)\]', ins['operands'])
            if not mat or ins['mnemonic'] not in ['mov', 'lea']:
                continue
            addr = int(ins['va'], 16) + len(bytes.fromhex(ins['bytes'])) + int(mat[1], 16)
            try:
                use = usage(addr)
            except (ValueError, IndexError, struct.error):
                continue
            if use and use.get('method_spec', {}).get('type') == 'a44':
                use.update(instruction_va=ins['va'], disassembly_evidence_id=obj['evidence_id'])
                usages.append(use)

    emit('player-semantic-mapping.json', dict(
        evidence_id='AO-META-PLAYER-SEMANTICS-001', binary_sha256=schema['binary_sha256'],
        metadata_sha256=schema['metadata_sha256'], version='1.32.030.345319', user_server='Asia/East',
        parameter_schema_evidence_id='AO-META-PARAMETER-ORDER-001',
        parameter_key_basis='按已恢复父先、每级 MetadataToken 升序、en 过滤的 setter 列表静态推导。尚无当前包样本。',
        mappings=mappings, evidence_files=refs, selected_field_layouts=selected_layouts,
        selected_metadata_vtable_entries=vtable, guild_alliance_context_constants=guild_constants,
        standing_jump_table=dict(va=hex(table_va), file_offset=table_raw, raw_hex=table_bytes.hex(),
                                 sha256=hashlib.sha256(table_bytes).hexdigest(), index_expression='standing - 1',
                                 entries=jump_entries), health_generic_usages=usages,
        limitations=['AO-META Evidence ID 为调查自定义编号，非 REA MCP Evidence ID。',
                     '地址为首选文件 VA；非运行时 ASLR 地址。native_offset 为解码后对象相对偏移，非网络包字节偏移。',
                     '本专题仅只读静态文件及限定反汇编；没有游戏运行、服务器实际样本或覆盖验证。',
                     'folded native pointers 可对应多种 metadata 方法；本文件结合具体类型 vtable、字段布局和 usage 判断。',
                     '没有把角色对象 Guid 等同于公开永久 ID，也没有将未追出的字段语义补猜。']))
    print('player-semantic-mapping.json written:', len(mappings), 'mappings;', len(refs), 'evidence files')


if __name__ == '__main__':
    main()
