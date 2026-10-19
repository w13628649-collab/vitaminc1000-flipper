"""Read-only bounded x64 PE disassembly to locate candidate call paths.

Capstone is installed only in metadata/dependencies; target is never loaded.
Ranges come from PE .pdata, retaining exact file offsets and byte hashes.
"""
import bisect
import hashlib
import json
import pathlib
import struct
import sys
from parse_metadata import PE, OUT, emit
sys.path.insert(0,str(pathlib.Path(__file__).parent/'dependencies'))
import capstone

pe=PE()
pd=next(s for s in pe.sections if s['name']=='.pdata')
ranges=[(pe.imagebase+a,pe.imagebase+b,u) for a,b,u in struct.iter_unpack('<III',pe.data[pd['raw']:pd['raw']+pd['rawsize']]) if a and b]
starts=[r[0] for r in ranges]
symbols=json.loads((OUT/'all-method-symbols.json').read_text(encoding='utf-8'))['symbols']
byaddr={}
for s in symbols:
    if s['native_va']:
        byaddr.setdefault(int(s['native_va'],16),[]).append(s)
def disassemble(va, extend=False):
    i=bisect.bisect_right(starts,va)-1
    if i<0 or not ranges[i][0]<=va<ranges[i][1]:
        raise ValueError(f'{va:#x} outside PE .pdata functions')
    begin,end,unwind=ranges[i]
    selected=[ranges[i]]
    if extend:
        following=[a for a in byaddr if a>begin]
        limit=min(following)
        j=i+1
        while j<len(ranges) and ranges[j][0]==end and ranges[j][0]<limit and ranges[j][1]-begin<65536:
            selected.append(ranges[j])
            end=ranges[j][1]
            j+=1
    raw=pe.va_to_raw(begin)
    buf=pe.data[raw:raw+end-begin]
    decoder=capstone.Cs(capstone.CS_ARCH_X86,capstone.CS_MODE_64)
    decoder.detail=True
    rows=[]
    for ins in decoder.disasm(buf,begin):
        row=dict(va=hex(ins.address),file_offset=raw+ins.address-begin,bytes=ins.bytes.hex(),mnemonic=ins.mnemonic,operands=ins.op_str)
        if ins.mnemonic in ['call','jmp'] and ins.operands and ins.operands[0].type==capstone.x86.X86_OP_IMM:
            target=ins.operands[0].imm
            row['target_va']=hex(target)
            row['target_symbols']=byaddr.get(target,[])
        rows.append(row)
    suffix='-adjacent' if extend else ''
    obj=dict(evidence_id='AO-META-DISASM-'+hex(begin)[2:]+suffix,target_sha256=hashlib.sha256(pe.data).hexdigest(),requested_va=hex(va),function_va=hex(begin),end_exclusive_va=hex(end),file_offset=raw,byte_length=len(buf),function_sha256=hashlib.sha256(buf).hexdigest(),capstone_version=capstone.__version__,symbols=byaddr.get(begin,[]),instructions=rows,pdata_segments=[dict(begin_va=hex(a),end_exclusive_va=hex(b),unwind_rva=hex(u)) for a,b,u in selected],limitations='PE unwind function range and x64 instruction decode. With adjacent flag, contiguous unwind fragments before next named method are included as a candidate interval; fragment membership remains inference. Direct call labels from metadata pointer slots; overloaded/folded pointers may have multiple symbols. No control-flow reconstruction or runtime evidence.')
    emit('disassembly-'+hex(begin)[2:]+suffix+'.json',obj)
    text='\n'.join(f"{r['va']} {r['mnemonic']:8} {r['operands']}"+((' ; '+' | '.join(s['image']+':'+s['type']+'.'+s['name'] for s in r.get('target_symbols',[]))[:600]) if r.get('target_symbols') else '') for r in rows)
    (OUT/('disassembly-'+hex(begin)[2:]+suffix+'.txt')).write_text(text,encoding='utf-8')
    return obj

if __name__=='__main__':
    extend='--adjacent' in sys.argv
    for arg in (a for a in sys.argv[1:] if a!='--adjacent'):
        obj=disassemble(int(arg,16),extend)
        print(obj['function_va'],obj['byte_length'],'bytes',len(obj['instructions']),'instructions')
