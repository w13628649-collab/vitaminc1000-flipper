"""Read v39 short protocol attributes, retaining original blob and field layout.

The decoder accepts the encountered one-short, zero-named-member attribute only;
other blobs are retained but not assigned values. Source format is pinned.
"""
import collections
import hashlib
import json
import struct
from generic_usages import m, pe, reg, nt, typename
from parse_metadata import emit, OUT

def cu(buf, p):
    b=buf[p]
    if b<128:return b,p+1
    if b==240:return struct.unpack_from('<I',buf,p+1)[0],p+5
    if b==255:return 0xffffffff,p+1
    if b==254:return 0xfffffffe,p+1
    if b&192==192:return ((b&63)<<24)|(buf[p+1]<<16)|(buf[p+2]<<8)|buf[p+3],p+4
    return ((b&127)<<8)|buf[p+1],p+2

ranges=m.read('attributeDataRange','II','token startOffset')
attributes={}
for im in m.images:
    start=im['customAttributeStart']; count=im['customAttributeCount']
    for ri in range(start,start+count):
        rr=ranges[ri]
        end=ranges[ri+1]['startOffset'] if ri+1<len(ranges) else m.sections['attributeData']['size']
        off=m.sections['attributeData']['offset']+rr['startOffset']
        assert rr['startOffset']<=end<=m.sections['attributeData']['size']
        blob=m.data[off:m.sections['attributeData']['offset']+end]
        n,p=cu(blob,0)
        ctors=list(struct.unpack_from('<'+'I'*n,blob,p)); p+=4*n
        attributes[im['name'],rr['token']]=dict(image=im['name'],token=hex(rr['token']),range_index=ri,range_file_offset=rr['metadata_file_offset'],blob_file_offset=off,blob_size=len(blob),blob_hex=blob.hex(),constructor_method_indices=ctors,argument_start=p)

def field_layout(ti):
    ty=m.types[ti]
    ptr=pe.unpack_va('<Q',reg['raw_values'][11]+ti*8)[0]
    offsets=list(pe.unpack_va('<'+'i'*ty['fieldCount'],ptr)) if ty['fieldCount'] else []
    fields=[]
    for ordinal,fi in enumerate(range(ty['fieldStart'],ty['fieldStart']+ty['fieldCount'])):
        f=m.fields[fi]
        fields.append(dict(own_metadata_ordinal=ordinal,field_index=fi,name=f['name'],type=typename(f['typeIndex']),type_index=f['typeIndex'],metadata_file_offset=f['metadata_file_offset'],token=hex(f['token']),native_offset=hex(offsets[ordinal]),attribute=attributes.get((ty['image'],f['token']))))
    parent=None
    if ty['parentIndex']<len(nt):
        da,bi=nt[ty['parentIndex']]
        if bi>>16&255 in (17,18):parent=dict(type_index=da,name=m.types[da]['fullname'])
    return dict(type_index=ti,name=ty['fullname'],image=ty['image'],metadata_file_offset=ty['metadata_file_offset'],token=hex(ty['token']),parent=parent,fields=fields)

def main():
    constants=json.loads((OUT/'all-field-constants.json').read_text(encoding='utf8'))
    enums={}
    # all-field-constants is a list inside one descriptive envelope.
    for entry in constants['constants']:
        if entry['type'] in ['Albion.Common.Photon.EventCodes','Albion.Common.Photon.OperationCodes']:
            enums.setdefault(entry['type'],{}).setdefault(entry['value'],[]).append(entry['name'])
    types=[]; skipped=[]
    for ti,t in enumerate(m.types):
        a=attributes.get((t['image'],t['token']))
        if not a or not any(c in [76963,76966] for c in a['constructor_method_indices']):continue
        if len(a['constructor_method_indices'])!=1:
            skipped.append(a);continue
        blob=bytes.fromhex(a['blob_hex']);p=a['argument_start']
        na,p=cu(blob,p);nf,p=cu(blob,p);np,p=cu(blob,p)
        if (na,nf,np)!=(1,0,0) or blob[p]!=6 or p+3!=len(blob):
            skipped.append(a);continue
        code=struct.unpack_from('<h',blob,p+1)[0]
        kind='event' if a['constructor_method_indices'][0]==76966 else 'operation'
        en='Albion.Common.Photon.EventCodes' if kind=='event' else 'Albion.Common.Photon.OperationCodes'
        types.append(dict(kind=kind,code=code,code_names=enums.get(en,{}).get(code,[]),protocol_attribute=a,**field_layout(ti)))
    result=dict(evidence_id='AO-META-PROTOCOL-ATTRIBUTES-001',metadata_sha256=hashlib.sha256(m.data).hexdigest(),binary_sha256=hashlib.sha256(pe.data).hexdigest(),constructors=dict(event=76966,operation=76963),types=types,skipped=skipped,limitations='Observed custom-attribute code and metadata/native field order. own_metadata_ordinal is not a proven Photon key. Inherited fields, reflection filtering and ordering must be confirmed from parser before use as a wire schema.')
    emit('protocol-attributes.json',result)
    selected=[x for x in types if (x['kind']=='event' and x['code'] in [1,3,6,7,29,90,103,104,106,238,312]) or (x['kind']=='operation' and x['code'] in [40,41,81,82,95,96,97,141,143,197,546])]
    parents=[];seen=set()
    for t in selected:
        p=t['parent']
        while p and p['type_index'] not in seen:
            seen.add(p['type_index']);item=field_layout(p['type_index']);parents.append(item);p=item['parent']
    emit('player-market-protocol-fields.json',dict(evidence_id='AO-META-PLAYER-MARKET-FIELDS-001',selected_types=selected,parents=parents,extra_types=[field_layout(t['index']) for t in m.types if t['fullname'] in ['dcy','ddc','ddh','dgc','cp0','cpx','eq','ep','er','any','avk','Albion.Common.Auction.AuctionDTO']],limitations=result['limitations']))
    print('Mapped',collections.Counter(x['kind'] for x in types),'skipped',len(skipped))
    for t in selected:print(t['kind'],t['code'],t['code_names'],t['name'],len(t['fields']))

if __name__=='__main__':main()
