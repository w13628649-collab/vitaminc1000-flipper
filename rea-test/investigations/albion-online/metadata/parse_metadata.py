"""Bounded, read-only IL2CPP v39 metadata and PE registration decoder.

Format provenance is pinned in sources/source-manifest.json. No target execution.
Supports the encountered v39 layout only; assertions reject unexpected layouts.
"""
import collections
import functools
import hashlib
import json
import pathlib
import re
import struct

ROOT = pathlib.Path(__file__).parent
GAME = pathlib.Path('C:/Users/ludy/AppData/Local/AlbionOnline/game')
META = GAME / 'Albion-Online_Data/il2cpp_data/Metadata/global-metadata.dat'
BINARY = GAME / 'GameAssembly.dll'
OUT = ROOT / 'results'
NAMES = 'stringLiteral stringLiteralData string events properties methods parameterDefaultValues fieldDefaultValues fieldAndParameterDefaultValueData fieldMarshaledSizes parameters fields genericParameters genericParameterConstraints genericContainers nestedTypes interfaces vtableMethods interfaceOffsets typeDefinitions images assemblies fieldRefs referencedAssemblies attributeData attributeDataRange unresolvedVirtualCallParameterTypes unresolvedVirtualCallParameterRanges windowsRuntimeTypeNames windowsRuntimeStrings exportedTypeDefinitions'.split()

def emit(name, obj):
    (OUT / name).write_text(json.dumps(obj, ensure_ascii=False, indent=2), encoding='utf-8')

def width(count):
    return 1 if count <= 255 else 2 if count <= 65535 else 4

def idxfmt(w):
    return {1: 'B', 2: 'H', 4: 'I'}[w]

class Metadata:
    def __init__(self):
        self.data = META.read_bytes()
        assert struct.unpack_from('<II', self.data) == (0xFAB11BAF, 39)
        self.sections = {n: dict(zip(('offset','size','count'), struct.unpack_from('<iii', self.data, 8+i*12))) for i,n in enumerate(NAMES)}
        for n,s in self.sections.items():
            assert s['offset'] >= 380 and s['offset'] + s['size'] <= len(self.data), (n,s)
        tw = self.sections['interfaceOffsets']['size'] // self.sections['interfaceOffsets']['count'] - 4
        dw = width(self.sections['typeDefinitions']['count'])
        gw = width(self.sections['genericContainers']['count'])
        pw = width(self.sections['parameters']['count'])
        self.widths = dict(type=tw,typeDefinition=dw,genericContainer=gw,parameter=pw)
        t,d,g,p = map(idxfmt, (tw,dw,gw,pw))
        self.types = self.read('typeDefinitions', 'ii'+t*3+g+'I'+'i'*8+'H'*8+'II',
            'nameIndex namespaceIndex byvalTypeIndex declaringTypeIndex parentIndex genericContainerIndex flags fieldStart methodStart eventStart propertyStart nestedTypesStart interfacesStart vtableStart interfaceOffsetsStart methodCount propertyCount fieldCount eventCount nestedTypeCount vtableCount interfacesCount interfaceOffsetsCount bitfield token')
        self.methods = self.read('methods', 'i'+d+t+'I'+p+g+'IHHHH',
            'nameIndex declaringType returnType returnParameterToken parameterStart genericContainerIndex token flags iflags slot parameterCount')
        self.fields = self.read('fields', 'i'+t+'I', 'nameIndex typeIndex token')
        self.params = self.read('parameters', 'iI'+t, 'nameIndex token typeIndex')
        self.defaults = self.read('fieldDefaultValues', 'i'+t+'i', 'fieldIndex typeIndex dataIndex')
        self.images = self.read('images', 'ii'+d+'I'+d+'IiIiI', 'nameIndex assemblyIndex typeStart typeCount exportedTypeStart exportedTypeCount entryPointIndex token customAttributeStart customAttributeCount')
        self.field_owner = {}
        self.type_image = {}
        self.default_lookup = {r['fieldIndex']: r for r in self.defaults}
        for image in self.images:
            image['name'] = self.string(image['nameIndex'])
            for ti in range(image['typeStart'], image['typeStart']+image['typeCount']):
                self.type_image[ti] = image['name']
        for ti,ty in enumerate(self.types):
            ty['name'] = self.string(ty['nameIndex'])
            ty['namespace'] = self.string(ty['namespaceIndex'])
            ty['fullname'] = (ty['namespace']+'.' if ty['namespace'] else '') + ty['name']
            ty['image'] = self.type_image[ti]
            assert ty['fieldStart'] + ty['fieldCount'] <= len(self.fields)
            assert ty['methodStart'] + ty['methodCount'] <= len(self.methods)
            for fi in range(ty['fieldStart'], ty['fieldStart'] + ty['fieldCount']):
                self.field_owner[fi] = ti
            for mi in range(ty['methodStart'], ty['methodStart'] + ty['methodCount']):
                assert self.methods[mi]['declaringType'] == ti, (ti,mi)
        for m in self.methods:
            m['name'] = self.string(m['nameIndex'])
            assert m['token'] >> 24 == 6
            if m['parameterCount']:
                assert m['parameterStart'] + m['parameterCount'] <= len(self.params)
        for f in self.fields:
            f['name'] = self.string(f['nameIndex'])
            assert f['token'] >> 24 == 4
        for p in self.params:
            p['name'] = self.string(p['nameIndex'])
        assert len(self.field_owner) == len(self.fields)
        assert len(self.type_image) == len(self.types)

    @functools.lru_cache(maxsize=None)
    def string(self, i):
        s = self.sections['string']
        assert 0 <= i < s['size'], i
        start=s['offset']+i
        end=self.data.index(b'\x00', start, s['offset']+s['size'])
        return self.data[start:end].decode('utf-8',errors='strict')

    def read(self, name, fmt, fields):
        s = self.sections[name]
        st=struct.Struct('<'+fmt)
        assert s['size'] == st.size*s['count'], (name, st.size, s)
        self.sections[name]['decoded_stride']=st.size
        names=fields.split()
        result=[]
        for i in range(s['count']):
            vals=st.unpack_from(self.data, s['offset']+i*st.size)
            assert len(vals)==len(names), name
            r=dict(zip(names, vals))
            r.update(index=i, metadata_file_offset=s['offset']+i*st.size)
            result.append(r)
        return result

class PE:
    def __init__(self):
        self.data = BINARY.read_bytes()
        b=self.data
        assert b[:2]==b'MZ'
        peoff=struct.unpack_from('<I',b,0x3c)[0]
        assert b[peoff:peoff+4]==b'PE\0\0'
        machine,count,_,_,_,optlen,_=struct.unpack_from('<HHIIIHH',b,peoff+4)
        assert machine==0x8664
        op=peoff+24
        assert struct.unpack_from('<H',b,op)[0]==0x20b
        self.imagebase=struct.unpack_from('<Q',b,op+24)[0]
        self.sections=[]
        for i in range(count):
            x=op+optlen+40*i
            name=b[x:x+8].rstrip(b'\0').decode()
            vsize,rva,rawsize,raw=struct.unpack_from('<IIII',b,x+8)
            flags=struct.unpack_from('<I',b,x+36)[0]
            self.sections.append(dict(name=name,rva=rva,vsize=vsize,rawsize=rawsize,raw=raw,flags=flags))

    def va_to_raw(self, va):
        rva=va-self.imagebase
        for s in self.sections:
            if s['rva'] <= rva < s['rva']+s['rawsize']:
                return s['raw']+rva-s['rva']
        raise ValueError(f'VA {va:#x} outside raw image')

    def raw_to_va(self, raw):
        for s in self.sections:
            if s['raw'] <= raw < s['raw']+s['rawsize']:
                return self.imagebase+s['rva']+raw-s['raw']
        raise ValueError(f'raw {raw:#x} outside sections')

    def executable(self, va):
        return any(s['rva'] <= va-self.imagebase < s['rva']+s['vsize'] and s['flags'] & 0x20000000 for s in self.sections)

    def unpack_va(self, fmt, va):
        return struct.unpack_from(fmt, self.data, self.va_to_raw(va))

    def find_all(self, pattern):
        pos=-1
        while True:
            pos=self.data.find(pattern,pos+1)
            if pos<0: break
            yield pos

    def find_module(self,name):
        candidates=[]
        for sraw in self.find_all(name.encode()+b'\0'):
            stringva=self.raw_to_va(sraw)
            for raw in self.find_all(struct.pack('<Q',stringva)):
                if raw%8: continue
                vals=struct.unpack_from('<QQQ',self.data,raw)
                if not 0<=vals[1]<1000000: continue
                try:
                    ptrraw=self.va_to_raw(vals[2]) if vals[1] else 0
                    ptrs=list(struct.unpack_from('<'+'Q'*vals[1],self.data,ptrraw))
                except (ValueError,struct.error):
                    continue
                if any(p and not self.executable(p) for p in ptrs): continue
                candidates.append(dict(name=name,file_offset=raw,va=self.raw_to_va(raw),name_string_file_offset=sraw,methodPointerCount=vals[1],methodPointersVA=vals[2],methodPointersFileOffset=ptrraw,pointers=ptrs))
        assert len(candidates)==1,(name,len(candidates))
        return candidates[0]

    def find_registration(self,m):
        count=m.sections['typeDefinitions']['count']
        result=[]
        for raw in self.find_all(struct.pack('<Q',count)):
            if raw%8 or raw<80: continue
            start=raw-80
            vals=struct.unpack_from('<'+'Q'*16,self.data,start)
            if vals[10]!=count or vals[12]!=count: continue
            if not max(f['typeIndex'] for f in m.fields)<vals[6]<1000000: continue
            try:
                ptrraw=self.va_to_raw(vals[7])
                ptrs=struct.unpack_from('<'+'Q'*vals[6],self.data,ptrraw)
                rawtypes=[self.va_to_raw(p) for p in ptrs]
                decoded=[struct.unpack_from('<QI',self.data,r) for r in rawtypes]
            except (ValueError,struct.error): continue
            if any((bits>>16)&255>0x55 for _,bits in decoded): continue
            result.append(dict(file_offset=start,va=self.raw_to_va(start),raw_values=vals,type_count=vals[6],type_pointers_va=vals[7],type_pointers_file_offset=ptrraw,decoded_types=decoded,type_raw_offsets=rawtypes))
        assert len(result)==1,[(x['file_offset'],x['type_count']) for x in result]
        return result[0]

def main():
    OUT.mkdir(parents=True,exist_ok=True)
    m=Metadata()
    emit('metadata-header.json', dict(evidence_id='AO-META-HEADER-001',target=str(META),sha256=hashlib.sha256(m.data).hexdigest(),metadata_version=39,widths=m.widths,sections=m.sections,validation='All selected table byte strides, string bounds, field/type/image coverage, method ownership and token table tags validated.'))
    print('parsed',len(m.types),'types',len(m.methods),'methods',len(m.fields),'fields',m.widths)
    pe=PE()
    modules={im['name']:pe.find_module(im['name']) for im in m.images}
    emit('native-modules.json',dict(evidence_id='AO-META-MODULES-001',target=str(BINARY),sha256=hashlib.sha256(pe.data).hexdigest(),imagebase=hex(pe.imagebase),sections=pe.sections,modules=[{k:(hex(v) if k.endswith('VA') or k=='va' else v) for k,v in mod.items() if k!='pointers'} for mod in modules.values()]))
    print('mapped',len(modules),'codegen modules')
    reg=pe.find_registration(m)
    rawtypes=reg.pop('decoded_types')
    tyraw=reg.pop('type_raw_offsets')
    emit('native-metadata-registration.json',dict(evidence_id='AO-META-REGISTRATION-001',**reg))
    print('registration',hex(reg['va']),'types',len(rawtypes))
    primitive={1:'void',2:'bool',3:'char',4:'sbyte',5:'byte',6:'short',7:'ushort',8:'int',9:'uint',10:'long',11:'ulong',12:'float',13:'double',14:'string',0x18:'IntPtr',0x19:'UIntPtr',0x1c:'object'}
    @functools.lru_cache(maxsize=None)
    def type_name_at(data,bits,depth=0):
        kind=(bits>>16)&255
        name=primitive.get(kind)
        if name is not None: return ('ref ' if bits&(1<<29) else '')+name
        if kind in (0x11,0x12):
            return ('ref ' if bits&(1<<29) else '')+m.types[data]['fullname']
        if depth<3 and kind in (0x0f,0x1d):
            da,bi=pe.unpack_va('<QI',data)
            return type_name_at(da,bi,depth+1)+('[]' if kind==0x1d else '*')
        if kind==0x15 and depth<3:
            typeptr,instptr=pe.unpack_va('<QQ',data)
            da,bi=pe.unpack_va('<QI',typeptr)
            base=type_name_at(da,bi,depth+1)
            if instptr:
                argc,args=pe.unpack_va('<QQ',instptr)
                if argc<32:
                    ap=pe.unpack_va('<'+'Q'*argc,args)
                    return base+'<'+','.join(type_name_at(*pe.unpack_va('<QI',p),depth+1) for p in ap)+'>'
            return base+'<?>'
        return f'<IL2CPP kind={kind:#x} data={data:#x}>'
    def type_name(i):
        return type_name_at(*rawtypes[i])
    def compressed_uint(offset):
        first=m.data[offset]
        if first==0xff: return 0xffffffff,1
        if first==0xfe: return 0xfffffffe,1
        if first==0xf0: return struct.unpack_from('<I',m.data,offset+1)[0],5
        if first&0xc0==0xc0:
            return ((first&0x3f)<<24)|(m.data[offset+1]<<16)|(m.data[offset+2]<<8)|m.data[offset+3],4
        if first&0x80:
            return ((first&0x7f)<<8)|m.data[offset+1],2
        return first,1
    def compressed_int(offset):
        u,n=compressed_uint(offset)
        if u==0xffffffff: return -2147483648,n
        return -(u>>1)-1 if u&1 else u>>1,n
    def default(fi):
        r=m.default_lookup.get(fi)
        if not r:return None
        out=dict(r)
        if r['dataIndex']<0: out['value']=None; return out
        offset=m.sections['fieldAndParameterDefaultValueData']['offset']+r['dataIndex']
        kind=(rawtypes[r['typeIndex']][1]>>16)&255
        out['default_data_file_offset']=offset
        out['il2cpp_kind']=kind
        fmt={2:'?',3:'H',4:'b',5:'B',6:'h',7:'H',10:'q',11:'Q',12:'f',13:'d'}
        if kind in fmt:
            out['value']=struct.unpack_from('<'+fmt[kind],m.data,offset)[0]
            used=struct.calcsize(fmt[kind])
        elif kind==8:out['value'],used=compressed_int(offset)
        elif kind==9:out['value'],used=compressed_uint(offset)
        elif kind==14:
            size,used=compressed_int(offset)
            if size<0: out['value']=None
            else:
                out['value']=m.data[offset+used:offset+used+size].decode('utf-8',errors='replace')
                used+=size
        else: out['value']=None; used=0; out['unsupported']=True
        out['encoded_hex']=m.data[offset:offset+used].hex()
        return out
    topic=re.compile(r'market|auction|worldmap|world.map|cluster|resource|harvest|photon|parametercode|operationcode|eventcode|minimap|overlay|expedition|territory|gateway|character|player|guild|alliance|party|health|hitpoints|interest|equipment|movement',re.I)
    selected=[]
    symbols=[]
    for ti,ty in enumerate(m.types):
        methods=[]
        fields=[]
        match=bool(topic.search(ty['fullname']))
        for fi in range(ty['fieldStart'],ty['fieldStart']+ty['fieldCount']):
            f=dict(m.fields[fi]);f['type']=type_name(f['typeIndex']);f['default']=default(fi)
            fields.append(f)
            match=match or bool(topic.search(f['name']))
        for mi in range(ty['methodStart'],ty['methodStart']+ty['methodCount']):
            me=dict(m.methods[mi]);me['returnTypeName']=type_name(me['returnType'])
            pars=m.params[me['parameterStart']:me['parameterStart']+me['parameterCount']] if me['parameterCount'] else []
            me['parameters']=[dict(p,type=type_name(p['typeIndex'])) for p in pars]
            mod=modules[ty['image']]
            ix=(me['token']&0xffffff)-1
            assert ix<len(mod['pointers']),(ty['image'],ix,len(mod['pointers']))
            pointer=mod['pointers'][ix]
            me.update(native_va=hex(pointer) if pointer else None,native_rva=hex(pointer-pe.imagebase) if pointer else None,native_file_offset=pe.va_to_raw(pointer) if pointer else None,pointer_slot_file_offset=mod['methodPointersFileOffset']+ix*8)
            methods.append(me)
            match=match or bool(topic.search(me['name']))
            symbols.append(dict(type_index=ti,type=ty['fullname'],image=ty['image'],method_index=mi,name=me['name'],token=hex(me['token']),native_va=me['native_va'],native_rva=me['native_rva'],native_file_offset=me['native_file_offset'],pointer_slot_file_offset=me['pointer_slot_file_offset']))
        if match:
            selected.append(dict(ty,methods=methods,fields=fields))
    emit('selected-types.json',dict(evidence_id='AO-META-TOPICS-001',search_regex=topic.pattern,coverage='Names and signatures in metadata only; bodies and runtime semantics not observed.',types=selected))
    emit('all-method-symbols.json',dict(evidence_id='AO-META-SYMBOLS-001',symbols=symbols))
    emit('all-type-index.json',dict(evidence_id='AO-META-TYPES-001',types=m.types))
    emit('all-field-constants.json',dict(evidence_id='AO-META-CONSTANTS-001',constants=[dict(type=m.types[m.field_owner[r['fieldIndex']]]['fullname'],image=m.types[m.field_owner[r['fieldIndex']]]['image'],name=m.fields[r['fieldIndex']]['name'],**default(r['fieldIndex'])) for r in m.defaults]))
    print('topic types',len(selected),'constants',len(m.defaults),'mapped nonzero methods',sum(bool(s['native_va']) for s in symbols))
    print('\n'.join(t['fullname']+' ['+t['image']+']' for t in selected if topic.search(t['fullname'])))

if __name__=='__main__':
    main()
