"""Derive selected protocol keys from statically recovered reflection ordering.

Not a packet decoder. declared_type is the destination CLR field type, and may
differ from Photon wire representations used by generated ce setters.
"""
import hashlib
import json
import struct
from generic_usages import m,pe,nt,usage
from protocol_attributes import field_layout,attributes
from parse_metadata import OUT,emit

def vtable_slot(type_index,slot):
    t=m.types[type_index];off=m.sections['vtableMethods']['offset']+4*(t['vtableStart']+slot)
    encoded=struct.unpack_from('<I',m.data,off)[0]
    index=(encoded&0x1fffffff)>>1 if encoded>>29==3 else next(x['index'] for x in m.methods[t['methodStart']:t['methodStart']+t['methodCount']] if x['slot']==slot)
    me=m.methods[index]
    return dict(type_index=type_index,type=t['fullname'],slot=slot,metadata_file_offset=off,encoded=hex(encoded),method_definition_index=index,method_name=me['name'],method_slot=me['slot'])

def chain(ti):
    result=[]
    while ti is not None:
        item=field_layout(ti);result.append(item)
        ti=item['parent']['type_index'] if item['parent'] else None
    return result[::-1]

def schema(ti):
    fields=[]
    for level in chain(ti):
        own=[]
        for f in level['fields']:
            flags=nt[f['type_index']][1]&0xffff
            # BindingFlags Instance|Public|DeclaredOnly excludes er static cache.
            if flags&7!=6 or flags&0x10:continue
            # For the selected inputs each attribute list has at most one entry.
            a=f['attribute']
            if a and a['constructor_method_indices'][0]==76961:continue
            own.append(f)
        for f in sorted(own,key=lambda f:int(f['token'],16)):
            fields.append(dict(parameter_key=len(fields),declaring_type=level['name'],declaring_type_index=level['type_index'],field_index=f['field_index'],field_name=f['name'],declared_type=f['type'],native_offset=f['native_offset'],metadata_file_offset=f['metadata_file_offset'],token=f['token']))
    return fields

def main():
    source=json.loads((OUT/'player-market-protocol-fields.json').read_text(encoding='utf8'))
    result=[]
    for t in source['selected_types']:
        ancestry=[x['name'] for x in chain(t['type_index'])]
        if t['kind']=='event':role='event'
        elif 'ah0' in ancestry:role='operation_response'
        elif 'x6' in ancestry or 'aj8' in ancestry:role='operation_request'
        else:role='operation_unknown'
        result.append(dict(kind=t['kind'],code=t['code'],code_names=t['code_names'],type_index=t['type_index'],type=t['name'],role_inferred_from_base_type=role,parameters=schema(t['type_index'])))
    evidence=dict(evidence_id='AO-META-PARAMETER-ORDER-001',binary_sha256=hashlib.sha256(pe.data).hexdigest(),metadata_sha256=hashlib.sha256(m.data).hexdigest(),observed_chain=['er..cctor 0x1808e5070 calls c0.a 0x18084da40','c0.a constructs type ancestry, reverses List<Type>','Type.GetFields BindingFlags 0x16 (DeclaredOnly|Instance|Public)','Fields with first attribute en (methodDef 76961) are omitted','Enumerable.OrderBy<FieldInfo,int>, selector methodDef 76164 VA 0x18084b3a0','Selector calls vtable offset 0x228 = base 0x138 + slot 15*16 = get_MetadataToken','One ce setter constructed per sorted FieldInfo','ci9.b<T> 0x182003eb0 uses byte keys starting at 0, increments once per ce setter; absent keys are skipped','ci9.b<T> operation response 0x182004220 reads subtype key 253 and similarly increments byte parameter keys'],selector_method_usage=usage(0x184db9570),vtable_basis=[vtable_slot(16864,15),vtable_slot(15913,89),vtable_slot(15913,117)],disassembly_evidence_ids=['AO-META-DISASM-1808e5070-adjacent','AO-META-DISASM-18084da40-adjacent','AO-META-DISASM-18084bbe0-adjacent','AO-META-DISASM-18084b3a0-adjacent','AO-META-DISASM-182003eb0-adjacent','AO-META-DISASM-182004220-adjacent'],selected_schemas=result,limitations='Static derived parameter keys only. Types are destination CLR field types, not claims about Photon physical encoding. No packet capture or runtime confirmation. Attribute ordering is simple for selected inputs; generic reflection failures and conditional server omissions not executed. Request/response role inferred from base classes and field shapes, with traced market JSON/parser corroboration.')
    emit('protocol-parameter-schema.json',evidence)
    print([(t['role_inferred_from_base_type'],t['code_names'],t['type'],len(t['parameters'])) for t in result])

if __name__=='__main__':main()
