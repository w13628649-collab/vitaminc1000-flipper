"""Focused file-resident usage lookup for selected generic parser types."""
import bisect
import collections
import json
import re
import struct
import sys
from generic_usages import m,pe,reg,spec,usage
from parse_metadata import OUT,emit

def main():
    targets=sys.argv[1:] or ['afb','ad1','ad2','atd']
    methods={x['index'] for x in m.methods if x['declaringType']==19182 and x['name'] in ['a','b']}
    specs=[]
    specraw=pe.va_to_raw(reg['raw_values'][9])
    for index,(mi,ci,gi) in enumerate(struct.iter_unpack('<iii',pe.data[specraw:specraw+reg['raw_values'][8]*12])):
        if mi not in methods:continue
        s=spec(index)
        if any(a['name'] in targets for a in s['generic_method_arguments']):specs.append(s)
    slot_lookup={}
    for s in specs:
        enc=((6<<29)|(s['index']<<1)|1)
        for raw in pe.find_all(struct.pack('<Q',enc)):
            if raw%8:continue
            try:va=pe.raw_to_va(raw)
            except ValueError:continue
            slot_lookup[va]=usage(va)
    symbols=json.loads((OUT/'all-method-symbols.json').read_text(encoding='utf8'))['symbols']
    pd=next(s for s in pe.sections if s['name']=='.pdata')
    ranges=[(pe.imagebase+a,pe.imagebase+b,u) for a,b,u in struct.iter_unpack('<III',pe.data[pd['raw']:pd['raw']+pd['rawsize']]) if a and b]
    starts=[r[0] for r in ranges]
    references=[]
    pat=re.compile(rb'[\x48\x4c][\x8b\x8d][\x05\x0d\x15\x1d\x25\x2d\x35\x3d][\s\S]{4}')
    for sec in pe.sections:
        if not sec['flags']&0x20000000:continue
        buf=pe.data[sec['raw']:sec['raw']+sec['rawsize']]
        for mat in pat.finditer(buf):
            off=mat.start();va=pe.imagebase+sec['rva']+off
            target=va+7+struct.unpack_from('<i',buf,off+3)[0]
            if target not in slot_lookup:continue
            i=bisect.bisect_right(starts,va)-1
            r=ranges[i]
            if not r[0]<=va<r[1]:continue
            preceding=[s for s in symbols if s['native_va'] and int(s['native_va'],16)<=va]
            near=max(int(s['native_va'],16) for s in preceding)
            references.append(dict(instruction_va=hex(va),instruction_file_offset=sec['raw']+off,instruction_bytes=mat.group().hex(),usage=slot_lookup[target],pdata_function_begin=hex(r[0]),nearest_named_method_va=hex(near),nearest_named_symbols=[s for s in preceding if int(s['native_va'],16)==near]))
    suffix='-'+'-'.join(targets) if sys.argv[1:] else ''
    emit('focused-generic-parser-callers'+suffix+'.json',dict(evidence_id='AO-META-GENERIC-CALLERS'+suffix+'-001',selected_type_names=targets,specs=specs,references=references,limitations='Byte-pattern RIP instruction candidates; a named method owner is nearest-preceding mapping, not proved CFG containment. Follow bounded disassembly before claiming a caller.'))
    print([(r['instruction_va'],r['nearest_named_method_va'],[(s['type'],s['name']) for s in r['nearest_named_symbols']],r['usage']['method_spec']['generic_method_arguments']) for r in references])

if __name__=='__main__':main()
