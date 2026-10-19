"""Bounded decode of metadata-named leaf methods omitted from PE unwind tables."""
import hashlib
import sys
from disassemble import pe,byaddr,capstone,emit,OUT

def main():
    decoder=capstone.Cs(capstone.CS_ARCH_X86,capstone.CS_MODE_64)
    for arg in sys.argv[1:]:
        va=int(arg,16);assert va in byaddr
        nextaddr=min(a for a in byaddr if a>va)
        raw=pe.va_to_raw(va);size=min(nextaddr-va,64)
        buf=pe.data[raw:raw+size];rows=[]
        for ins in decoder.disasm(buf,va):
            rows.append(dict(va=hex(ins.address),file_offset=raw+ins.address-va,bytes=ins.bytes.hex(),mnemonic=ins.mnemonic,operands=ins.op_str))
            if ins.mnemonic in ['ret','jmp']:break
        used=sum(len(bytes.fromhex(r['bytes'])) for r in rows)
        obj=dict(evidence_id='AO-META-LEAF-'+hex(va)[2:],function_va=hex(va),file_offset=raw,byte_length=used,bytes_sha256=hashlib.sha256(buf[:used]).hexdigest(),binary_sha256=hashlib.sha256(pe.data).hexdigest(),symbols=byaddr[va],instructions=rows,limitations='Metadata-named code pointer with no PE unwind record. Maximum64 bytes bounded by next named method, stop at first RET/JMP; leaf function membership remains inference. No runtime observation.')
        emit('leaf-'+hex(va)[2:]+'.json',obj)
        text='\n'.join(f"{r['va']} {r['mnemonic']} {r['operands']}" for r in rows)
        (OUT/('leaf-'+hex(va)[2:]+'.txt')).write_text(text,encoding='utf8');print(text)

if __name__=='__main__':main()
