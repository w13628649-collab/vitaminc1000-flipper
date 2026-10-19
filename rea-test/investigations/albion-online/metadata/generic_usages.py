"""Decode selected native encoded IL2CPP metadata usages and generic targets."""
import functools
import json
import re
import struct
from parse_metadata import *

m=Metadata()
pe=PE()
reg=pe.find_registration(m)
v=reg['raw_values']
nt=reg['decoded_types']
@functools.lru_cache(None)
def qualified_type_name(index):
    ty=m.types[index]
    di=ty['declaringTypeIndex']
    if di<len(nt):
        da,bi=nt[di]
        if bi>>16&255 in (17,18):return qualified_type_name(da)+'+'+ty['name']
    return ty['fullname']
@functools.lru_cache(None)
def typename_data(data,bits,depth=0):
    k=(bits>>16)&255
    primitive={1:'void',2:'bool',3:'char',4:'sbyte',5:'byte',6:'short',7:'ushort',8:'int',9:'uint',10:'long',11:'ulong',12:'float',13:'double',14:'string',0x18:'IntPtr',0x19:'UIntPtr',0x1c:'object'}
    if k in primitive:return primitive[k]
    if k in (0x11,0x12):return qualified_type_name(data)
    if k==0x1d and depth<5:return typename_data(*pe.unpack_va('<QI',data),depth+1)+'[]'
    if k==0x15 and depth<5:
        tptr,inst=pe.unpack_va('<QQ',data)
        base=typename_data(*pe.unpack_va('<QI',tptr),depth+1)
        if inst:
            argc,args=pe.unpack_va('<QQ',inst)
            return base+'<'+','.join(typename_data(*pe.unpack_va('<QI',a),depth+1) for a in pe.unpack_va('<'+'Q'*argc,args))+'>'
    return f'<kind={k:#x},data={data:#x}>'
def typename(i):return typename_data(*nt[i])
def spec(index):
    raw=pe.va_to_raw(v[9]+index*12)
    mi,ci,gi=struct.unpack_from('<iii',pe.data,raw)
    if not 0<=mi<len(m.methods):raise ValueError()
    me=m.methods[mi];ty=m.types[me['declaringType']]
    generic=[]
    for n in (ci,gi):
        if n>=0:
            p=pe.unpack_va('<Q',v[3]+n*8)[0]
            argc,args=pe.unpack_va('<QQ',p)
            generic.append([dict(data=da,bits=bi,name=typename_data(da,bi)) for da,bi in (pe.unpack_va('<QI',a) for a in pe.unpack_va('<'+'Q'*argc,args))])
        else:generic.append([])
    return dict(index=index,file_offset=raw,method_definition_index=mi,type=ty['fullname'],image=ty['image'],method=me['name'],generic_class_arguments=generic[0],generic_method_arguments=generic[1])
def usage(addr):
    encoded=pe.unpack_va('<Q',addr)[0]
    if encoded>0xffffffff or not encoded&1:return None
    kind=(encoded>>29)&7;index=(encoded&0x1fffffff)>>1
    obj=dict(slot_va=hex(addr),slot_file_offset=pe.va_to_raw(addr),encoded=hex(encoded),kind=kind,index=index)
    if kind==6:obj['method_spec']=spec(index)
    elif kind==3 and index<len(m.methods):
        me=m.methods[index];ty=m.types[me['declaringType']]
        obj['method_definition']=dict(index=index,type=ty['fullname'],type_index=ty['index'],image=ty['image'],method=me['name'],metadata_file_offset=me['metadata_file_offset'])
    elif kind in (1,2) and index<len(nt):obj['type_name']=typename(index)
    else:return None
    return obj
def main():
    results=[]
    for file in OUT.glob('disassembly-*-adjacent.json'):
        obj=json.loads(file.read_text(encoding='utf8'))
        uses=[]
        for ins in obj['instructions']:
            if ins['mnemonic'] not in ['mov','lea']:continue
            match=re.search(r'\[rip \+ (0x[0-9a-f]+)\]',ins['operands'])
            if not match:continue
            addr=int(ins['va'],16)+len(bytes.fromhex(ins['bytes']))+int(match[1],16)
            try:u=usage(addr)
            except (ValueError,struct.error,IndexError):continue
            if u:
                u['instruction_va']=ins['va'];uses.append(u)
        results.append(dict(disassembly_evidence_id=obj['evidence_id'],function_va=obj['function_va'],symbols=obj['symbols'],usages=uses))
    emit('generic-usage-resolution.json',dict(evidence_id='AO-META-GENERIC-USAGES-001',results=results,limitations='File-resident encoded usage to v39 method-spec table. Generic type mapping validated structurally; no runtime methodinfo dereference.'))
    print('constructor deserialize',usage(0x184dca358))
    for result in results:
        for u in result['usages']:
            sp=u.get('method_spec')
            if sp and sp['type']=='ci9' and sp['method']=='b':
                args=sp['generic_method_arguments']
                if args:
                    t=m.types[args[0]['data']] if args[0]['bits']>>16&255 in [17,18] else None
                    if t and t['fieldCount']>20:
                        print(result['function_va'],[(s['type'],s['name']) for s in result['symbols']],t['index'],t['fullname'],t['fieldCount'])

if __name__=='__main__':main()
