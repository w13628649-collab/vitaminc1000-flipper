import collections
import json
import re
import struct
from parse_metadata import *

m=Metadata()
pe=PE()
reg=pe.find_registration(m)
nt=reg['decoded_types']
mods={n:pe.find_module(n) for n in ['Albion.PhotonClient.dll','Albion.Common.dll','Assembly-CSharp.dll','Photon3Unity3D.dll']}
primitive={1:'void',2:'bool',3:'char',4:'sbyte',5:'byte',6:'short',7:'ushort',8:'int',9:'uint',10:'long',11:'ulong',12:'float',13:'double',14:'string',0x18:'IntPtr',0x19:'UIntPtr',0x1c:'object'}
def typename(i):
    data,bits=nt[i]
    k=(bits>>16)&255
    if k in primitive:return primitive[k]
    if k in (0x11,0x12):return m.types[data]['fullname']
    if k==0x15:
        tptr,inst=pe.unpack_va('<QQ',data)
        td,tb=pe.unpack_va('<QI',tptr)
        if (tb>>16)&255 in (0x11,0x12):return m.types[td]['fullname']+'<?>'
    if k==0x1d:
        td,tb=pe.unpack_va('<QI',data)
        if (tb>>16)&255 in primitive:return primitive[(tb>>16)&255]+'[]'
    return 'kind:'+hex(k)
def merow(me):
    me=dict(me)
    ty=m.types[me['declaringType']]
    pars=m.params[me['parameterStart']:me['parameterStart']+me['parameterCount']] if me['parameterCount'] else []
    me['parameters']=[dict(p,type=typename(p['typeIndex'])) for p in pars]
    me['returnTypeName']=typename(me['returnType'])
    mod=mods[ty['image']]
    ix=(me['token']&0xffffff)-1
    pointer=mod['pointers'][ix]
    me.update(type=ty['fullname'],image=ty['image'],native_va=hex(pointer) if pointer else None,native_file_offset=pe.va_to_raw(pointer) if pointer else None,pointer_slot_file_offset=mod['methodPointersFileOffset']+ix*8)
    return me
interfaces=struct.unpack_from('<'+'I'*m.sections['interfaces']['count'],m.data,m.sections['interfaces']['offset'])
target_interfaces={t['byvalTypeIndex'] for t in m.types if t['fullname'] in ['ci3','ExitGames.Client.Photon.IPhotonPeerListener']}
target_types={t['index'] for t in m.types if t['fullname'] in ['ci7','ctp','c3d','dgc','WorldMap.WorldmapGeneration','WorldMap.WorldmapGenerationMinimap','WorldMap.WorldmapResources','WorldMap.Worldmap','LocalPlayerCharacterView','g7s','g7q','c1l','cpo']}
implementers=[]
for t in m.types:
    if t['image'] not in mods:continue
    ints=interfaces[t['interfacesStart']:t['interfacesStart']+t['interfacesCount']] if t['interfacesCount'] else []
    if set(ints)&target_interfaces:
        target_types.add(t['index']);implementers.append(dict(t,interface_types=[typename(i) for i in ints]))
typed_candidates=[]
for me in m.methods:
    ty=m.types[me['declaringType']]
    if ty['image'] not in mods:continue
    pars=m.params[me['parameterStart']:me['parameterStart']+me['parameterCount']] if me['parameterCount'] else []
    types=[typename(p['typeIndex']) for p in pars]
    if (any(re.search(r'Photon.(EventData|OperationResponse)|EventCodes|OperationCodes|AuctionDTO|^g7[sq]$',t) for t in types) and ty['image'] in ['Albion.PhotonClient.dll','Albion.Common.dll','Photon3Unity3D.dll']) or (ty['index'] in target_types):
        typed_candidates.append(merow(me))
emit('focus-candidates.json',dict(evidence_id='AO-META-CANDIDATES-001',implementers=implementers,methods=typed_candidates,limitations='Signatures and tables only. Obfuscated method labels do not prove behavior.'))
print('implementers',[(t['index'],t['fullname'],t['methodCount']) for t in implementers])
for x in typed_candidates:
    if x['type'] not in ['ci7','ctp','c3d','cpo','WorldMap.Worldmap','LocalPlayerCharacterView','LoginGui'] or any(re.search(r'Photon.(EventData|OperationResponse)|EventCodes|OperationCodes|AuctionDTO|^g7[sq]$',p['type']) for p in x['parameters']):
        print(x['index'],x['type'],x['name'],x['native_va'],x['returnTypeName'],[(p['name'],p['type']) for p in x['parameters']])
